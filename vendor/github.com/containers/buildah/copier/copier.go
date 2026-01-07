package copier

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/reexec"
)

const (
	copierCommand    = "buildah-copier"
	maxLoopsFollowed = 64
	// See http://pubs.opengroup.org/onlinepubs/9699919799/utilities/pax.html#tag_20_92_13_06, from archive/tar
	cISUID = 0o4000 // Set uid, from archive/tar
	cISGID = 0o2000 // Set gid, from archive/tar
	cISVTX = 0o1000 // Save text (sticky bit), from archive/tar
	// xattrs in the PAXRecords map are namespaced with this prefix
	xattrPAXRecordNamespace = "SCHILY.xattr."
)

func init() {
	reexec.Register(copierCommand, copierMain)
}

// extendedGlob calls filepath.Glob() on the passed-in patterns.  If there is a
// "**" component in the pattern, filepath.Glob() will be called with the "**"
// replaced with all of the subdirectories under that point, and the results
// will be concatenated.
// The matched paths are returned in lexical order, which makes the output deterministic.
func extendedGlob(pattern string) (matches []string, err error) {
	subdirs := func(dir string) []string {
		var subdirectories []string
		if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if rel, err := filepath.Rel(dir, path); err == nil {
					subdirectories = append(subdirectories, rel)
				}
			}
			return nil
		}); err != nil {
			subdirectories = []string{"."}
		}
		return subdirectories
	}
	expandPatterns := func(pattern string) []string {
		components := []string{}
		dir := pattern
		file := ""
		for dir != filepath.VolumeName(dir) && dir != string(os.PathSeparator) {
			dir, file = filepath.Split(dir)
			if file != "" {
				components = append([]string{file}, components...)
			}
			dir = strings.TrimSuffix(dir, string(os.PathSeparator))
		}
		patterns := []string{filepath.VolumeName(dir) + string(os.PathSeparator)}
		for i := range components {
			var nextPatterns []string
			if components[i] == "**" {
				for _, parent := range patterns {
					nextSubdirs := subdirs(parent)
					for _, nextSubdir := range nextSubdirs {
						nextPatterns = append(nextPatterns, filepath.Join(parent, nextSubdir))
					}
				}
			} else {
				for _, parent := range patterns {
					nextPattern := filepath.Join(parent, components[i])
					nextPatterns = append(nextPatterns, nextPattern)
				}
			}
			patterns = []string{}
			seen := map[string]struct{}{}
			for _, nextPattern := range nextPatterns {
				if _, seen := seen[nextPattern]; seen {
					continue
				}
				patterns = append(patterns, nextPattern)
				seen[nextPattern] = struct{}{}
			}
		}
		return patterns
	}
	patterns := expandPatterns(pattern)
	for _, pattern := range patterns {
		theseMatches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		matches = append(matches, theseMatches...)
	}
	sort.Strings(matches)
	return matches, nil
}

// isArchivePath returns true if the specified path can be read like a (possibly
// compressed) tarball.
func isArchivePath(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	rc, _, err := compression.AutoDecompress(f)
	if err != nil {
		return false
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	_, err = tr.Next()
	return err == nil
}

// requestType encodes exactly what kind of request this is.
type requestType string

const (
	requestEval              requestType = "EVAL"
	requestStat              requestType = "STAT"
	requestGet               requestType = "GET"
	requestPut               requestType = "PUT"
	requestMkdir             requestType = "MKDIR"
	requestRemove            requestType = "REMOVE"
	requestQuit              requestType = "QUIT"
	requestEnsure            requestType = "ENSURE"
	requestConditionalRemove requestType = "CONDRM"
)

// Request encodes a single request.
type request struct {
	Request                  requestType
	Root                     string // used by all requests
	preservedRoot            string
	rootPrefix               string // used to reconstruct paths being handed back to the caller
	Directory                string // used by all requests
	preservedDirectory       string
	Globs                    []string `json:",omitempty"` // used by stat, get
	preservedGlobs           []string
	StatOptions              StatOptions
	GetOptions               GetOptions
	PutOptions               PutOptions
	MkdirOptions             MkdirOptions
	RemoveOptions            RemoveOptions
	EnsureOptions            EnsureOptions
	ConditionalRemoveOptions ConditionalRemoveOptions
}

func (req *request) Excludes() []string {
	switch req.Request {
	case requestEval:
		return nil
	case requestStat:
		return req.StatOptions.Excludes
	case requestGet:
		return req.GetOptions.Excludes
	case requestPut:
		return nil
	case requestMkdir:
		return nil
	case requestRemove:
		return nil
	case requestQuit:
		return nil
	case requestEnsure:
		return nil
	case requestConditionalRemove:
		return nil
	default:
		panic(fmt.Sprintf("not an implemented request type: %q", req.Request))
	}
}

func (req *request) UIDMap() []idtools.IDMap {
	switch req.Request {
	case requestEval:
		return nil
	case requestStat:
		return req.StatOptions.UIDMap
	case requestGet:
		return req.GetOptions.UIDMap
	case requestPut:
		return req.PutOptions.UIDMap
	case requestMkdir:
		return req.MkdirOptions.UIDMap
	case requestRemove:
		return nil
	case requestQuit:
		return nil
	case requestEnsure:
		return req.EnsureOptions.UIDMap
	case requestConditionalRemove:
		return req.ConditionalRemoveOptions.UIDMap
	default:
		panic(fmt.Sprintf("not an implemented request type: %q", req.Request))
	}
}

func (req *request) GIDMap() []idtools.IDMap {
	switch req.Request {
	case requestEval:
		return nil
	case requestStat:
		return req.StatOptions.GIDMap
	case requestGet:
		return req.GetOptions.GIDMap
	case requestPut:
		return req.PutOptions.GIDMap
	case requestMkdir:
		return req.MkdirOptions.GIDMap
	case requestRemove:
		return nil
	case requestQuit:
		return nil
	case requestEnsure:
		return req.EnsureOptions.GIDMap
	case requestConditionalRemove:
		return req.ConditionalRemoveOptions.GIDMap
	default:
		panic(fmt.Sprintf("not an implemented request type: %q", req.Request))
	}
}

// Response encodes a single response.
type response struct {
	Error             string `json:",omitempty"`
	Stat              statResponse
	Eval              evalResponse
	Get               getResponse
	Put               putResponse
	Mkdir             mkdirResponse
	Remove            removeResponse
	Ensure            ensureResponse
	ConditionalRemove conditionalRemoveResponse
}

// statResponse encodes a response for a single Stat request.
type statResponse struct {
	Globs []*StatsForGlob
}

// evalResponse encodes a response for a single Eval request.
type evalResponse struct {
	Evaluated string
}

// StatsForGlob encode results for a single glob pattern passed to Stat().
type StatsForGlob struct {
	Error   string                  `json:",omitempty"` // error if the Glob pattern was malformed
	Glob    string                  // input pattern to which this result corresponds
	Globbed []string                // a slice of zero or more names that match the glob
	Results map[string]*StatForItem // one for each Globbed value if there are any, or for Glob
}

// StatForItem encode results for a single filesystem item, as returned by Stat().
type StatForItem struct {
	Error           string `json:",omitempty"`
	Name            string
	Size            int64       // dereferenced value for symlinks
	Mode            os.FileMode // dereferenced value for symlinks
	ModTime         time.Time   // dereferenced value for symlinks
	UID, GID        int64       // usually in the uint32 range, set to -1 if unknown
	IsSymlink       bool
	IsDir           bool   // dereferenced value for symlinks
	IsRegular       bool   // dereferenced value for symlinks
	IsArchive       bool   // dereferenced value for symlinks
	ImmediateTarget string `json:",omitempty"` // raw link content
}

// getResponse encodes a response for a single Get request.
type getResponse struct{}

// putResponse encodes a response for a single Put request.
type putResponse struct{}

// mkdirResponse encodes a response for a single Mkdir request.
type mkdirResponse struct{}

// removeResponse encodes a response for a single Remove request.
type removeResponse struct{}

// ensureResponse encodes a response to an Ensure request.
type ensureResponse struct {
	Created []string           // paths that were created because they weren't already present
	Noted   []EnsureParentPath // preexisting paths that are parents of created items
}

// conditionalRemoveResponse encodes a response to a conditionalRemove request.
type conditionalRemoveResponse struct {
	Removed []string // paths that were removed
}

// EvalOptions controls parts of Eval()'s behavior.
type EvalOptions struct{}

// Eval evaluates the directory's path, including any intermediate symbolic
// links.
// If root is specified and the current OS supports it, and the calling process
// has the necessary privileges, evaluation is performed in a chrooted context.
// If the directory is specified as an absolute path, it should either be the
// root directory or a subdirectory of the root directory.  Otherwise, the
// directory is treated as a path relative to the root directory.
func Eval(root string, directory string, _ EvalOptions) (string, error) {
	req := request{
		Request:   requestEval,
		Root:      root,
		Directory: directory,
	}
	resp, err := copier(nil, nil, req)
	if err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", errors.New(resp.Error)
	}
	return resp.Eval.Evaluated, nil
}

// StatOptions controls parts of Stat()'s behavior.
type StatOptions struct {
	UIDMap, GIDMap   []idtools.IDMap // map from hostIDs to containerIDs when returning results
	CheckForArchives bool            // check for and populate the IsArchive bit in returned values
	Excludes         []string        // contents to pretend don't exist, using the OS-specific path separator
}

// Stat globs the specified pattern in the specified directory and returns its
// results.
// If root and directory are both not specified, the current root directory is
// used, and relative names in the globs list are treated as being relative to
// the current working directory.
// If root is specified and the current OS supports it, and the calling process
// has the necessary privileges, the stat() is performed in a chrooted context.
// If the directory is specified as an absolute path, it should either be the
// root directory or a subdirectory of the root directory.  Otherwise, the
// directory is treated as a path relative to the root directory.
// Relative names in the glob list are treated as being relative to the
// directory.
func Stat(root string, directory string, options StatOptions, globs []string) ([]*StatsForGlob, error) {
	req := request{
		Request:     requestStat,
		Root:        root,
		Directory:   directory,
		Globs:       slices.Clone(globs),
		StatOptions: options,
	}
	resp, err := copier(nil, nil, req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Stat.Globs, nil
}

// GetOptions controls parts of Get()'s behavior.
type GetOptions struct {
	UIDMap, GIDMap     []idtools.IDMap   // map from hostIDs to containerIDs in the output archive
	Excludes           []string          // contents to pretend don't exist, using the OS-specific path separator
	ExpandArchives     bool              // extract the contents of named items that are archives
	ChownDirs          *idtools.IDPair   // set ownership on directories. no effect on archives being extracted
	ChmodDirs          *os.FileMode      // set permissions on directories. no effect on archives being extracted
	ChownFiles         *idtools.IDPair   // set ownership of files. no effect on archives being extracted
	ChmodFiles         *os.FileMode      // set permissions on files. no effect on archives being extracted
	Parents            bool              // maintain the sources parent directory in the destination
	StripSetuidBit     bool              // strip the setuid bit off of items being copied. no effect on archives being extracted
	StripSetgidBit     bool              // strip the setgid bit off of items being copied. no effect on archives being extracted
	StripStickyBit     bool              // strip the sticky bit off of items being copied. no effect on archives being extracted
	StripXattrs        bool              // don't record extended attributes of items being copied. no effect on archives being extracted
	KeepDirectoryNames bool              // don't strip the top directory's basename from the paths of items in subdirectories
	Rename             map[string]string // rename items with the specified names, or under the specified names
	NoDerefSymlinks    bool              // don't follow symlinks when globs match them
	IgnoreUnreadable   bool              // ignore errors reading items, instead of returning an error
	NoCrossDevice      bool              // if a subdirectory is a mountpoint with a different device number, include it but skip its contents
	Timestamp          *time.Time        // timestamp to force on all contents
}

// Get produces an archive containing items that match the specified glob
// patterns and writes it to bulkWriter.
// If root and directory are both not specified, the current root directory is
// used, and relative names in the globs list are treated as being relative to
// the current working directory.
// If root is specified and the current OS supports it, and the calling process
// has the necessary privileges, the contents are read in a chrooted context.
// If the directory is specified as an absolute path, it should either be the
// root directory or a subdirectory of the root directory.  Otherwise, the
// directory is treated as a path relative to the root directory.
// Relative names in the glob list are treated as being relative to the
// directory.
func Get(root string, directory string, options GetOptions, globs []string, bulkWriter io.Writer) error {
	req := request{
		Request:   requestGet,
		Root:      root,
		Directory: directory,
		Globs:     slices.Clone(globs),
		StatOptions: StatOptions{
			CheckForArchives: options.ExpandArchives,
		},
		GetOptions: options,
	}
	resp, err := copier(nil, bulkWriter, req)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// PutOptions controls parts of Put()'s behavior.
type PutOptions struct {
	UIDMap, GIDMap       []idtools.IDMap   // map from containerIDs to hostIDs when writing contents to disk
	DefaultDirOwner      *idtools.IDPair   // set ownership of implicitly-created directories, default is ChownDirs, or 0:0 if ChownDirs not set
	DefaultDirMode       *os.FileMode      // set permissions on implicitly-created directories, default is ChmodDirs, or 0755 if ChmodDirs not set
	ChownDirs            *idtools.IDPair   // set ownership of newly-created directories
	ChmodDirs            *os.FileMode      // set permissions on newly-created directories
	ChownFiles           *idtools.IDPair   // set ownership of newly-created files
	ChmodFiles           *os.FileMode      // set permissions on newly-created files
	StripSetuidBit       bool              // strip the setuid bit off of items being written
	StripSetgidBit       bool              // strip the setgid bit off of items being written
	StripStickyBit       bool              // strip the sticky bit off of items being written
	StripXattrs          bool              // don't bother trying to set extended attributes of items being copied
	IgnoreXattrErrors    bool              // ignore any errors encountered when attempting to set extended attributes
	IgnoreDevices        bool              // ignore items which are character or block devices
	NoOverwriteDirNonDir bool              // instead of quietly overwriting directories with non-directories, return an error
	NoOverwriteNonDirDir bool              // instead of quietly overwriting non-directories with directories, return an error
	Rename               map[string]string // rename items with the specified names, or under the specified names
}

// Put extracts an archive from the bulkReader at the specified directory.
// If root and directory are both not specified, the current root directory is
// used.
// If root is specified and the current OS supports it, and the calling process
// has the necessary privileges, the contents are written in a chrooted
// context.  If the directory is specified as an absolute path, it should
// either be the root directory or a subdirectory of the root directory.
// Otherwise, the directory is treated as a path relative to the root
// directory.
func Put(root string, directory string, options PutOptions, bulkReader io.Reader) error {
	req := request{
		Request:    requestPut,
		Root:       root,
		Directory:  directory,
		PutOptions: options,
	}
	resp, err := copier(bulkReader, nil, req)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// MkdirOptions controls parts of Mkdir()'s behavior.
type MkdirOptions struct {
	UIDMap, GIDMap []idtools.IDMap // map from containerIDs to hostIDs when creating directories
	ModTimeNew     *time.Time      // set mtime and atime of newly-created directories
	ChownNew       *idtools.IDPair // set ownership of newly-created directories
	ChmodNew       *os.FileMode    // set permissions on newly-created directories
}

// Mkdir ensures that the specified directory exists.  Any directories which
// need to be created will be given the specified ownership and permissions.
// If root and directory are both not specified, the current root directory is
// used.
// If root is specified and the current OS supports it, and the calling process
// has the necessary privileges, the directory is created in a chrooted
// context.  If the directory is specified as an absolute path, it should
// either be the root directory or a subdirectory of the root directory.
// Otherwise, the directory is treated as a path relative to the root
// directory.
func Mkdir(root string, directory string, options MkdirOptions) error {
	req := request{
		Request:      requestMkdir,
		Root:         root,
		Directory:    directory,
		MkdirOptions: options,
	}
	resp, err := copier(nil, nil, req)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// RemoveOptions controls parts of Remove()'s behavior.
type RemoveOptions struct {
	All bool // if Directory is a directory, remove its contents as well
}

// Remove removes the specified directory or item, traversing any intermediate
// symbolic links.
// If the root directory is not specified, the current root directory is used.
// If root is specified and the current OS supports it, and the calling process
// has the necessary privileges, the remove() is performed in a chrooted context.
// If the item to remove is specified as an absolute path, it should either be
// in the root directory or in a subdirectory of the root directory.  Otherwise,
// the directory is treated as a path relative to the root directory.
func Remove(root string, item string, options RemoveOptions) error {
	req := request{
		Request:       requestRemove,
		Root:          root,
		Directory:     item,
		RemoveOptions: options,
	}
	resp, err := copier(nil, nil, req)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

// cleanerReldirectory resolves relative path candidate lexically, attempting
// to ensure that when joined as a subdirectory of another directory, it does
// not reference anything outside of that other directory.
func cleanerReldirectory(candidate string) string {
	cleaned := strings.TrimPrefix(filepath.Clean(string(os.PathSeparator)+candidate), string(os.PathSeparator))
	if cleaned == "" {
		return "."
	}
	return cleaned
}

// convertToRelSubdirectory returns the path of directory, bound and relative to
// root, as a relative path, or an error if that path can't be computed or if
// the two directories are on different volumes
func convertToRelSubdirectory(root, directory string) (relative string, err error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("expected root directory to be an absolute path, got %q", root)
	}
	if directory == "" || !filepath.IsAbs(directory) {
		return "", fmt.Errorf("expected directory to be an absolute path, got %q", root)
	}
	if filepath.VolumeName(root) != filepath.VolumeName(directory) {
		return "", fmt.Errorf("%q and %q are on different volumes", root, directory)
	}
	rel, err := filepath.Rel(root, directory)
	if err != nil {
		return "", fmt.Errorf("computing path of %q relative to %q: %w", directory, root, err)
	}
	return cleanerReldirectory(rel), nil
}

func currentVolumeRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current working directory: %w", err)
	}
	return filepath.VolumeName(cwd) + string(os.PathSeparator), nil
}

func isVolumeRoot(candidate string) (bool, error) {
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return false, fmt.Errorf("converting %q to an absolute path: %w", candidate, err)
	}
	return abs == filepath.VolumeName(abs)+string(os.PathSeparator), nil
}

func looksLikeAbs(candidate string) bool {
	return candidate[0] == os.PathSeparator && (len(candidate) == 1 || candidate[1] != os.PathSeparator)
}

func copier(bulkReader io.Reader, bulkWriter io.Writer, req request) (*response, error) {
	if req.Directory == "" {
		if req.Root == "" {
			wd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("getting current working directory: %w", err)
			}
			req.Directory = wd
		} else {
			req.Directory = req.Root
		}
	}
	if req.Root == "" {
		root, err := currentVolumeRoot()
		if err != nil {
			return nil, fmt.Errorf("determining root of current volume: %w", err)
		}
		req.Root = root
	}
	if filepath.IsAbs(req.Directory) {
		_, err := convertToRelSubdirectory(req.Root, req.Directory)
		if err != nil {
			return nil, fmt.Errorf("rewriting %q to be relative to %q: %w", req.Directory, req.Root, err)
		}
	}
	isAlreadyRoot, err := isVolumeRoot(req.Root)
	if err != nil {
		return nil, fmt.Errorf("checking if %q is a root directory: %w", req.Root, err)
	}
	if !isAlreadyRoot && canChroot {
		return copierWithSubprocess(bulkReader, bulkWriter, req)
	}
	return copierWithoutSubprocess(bulkReader, bulkWriter, req)
}

func copierWithoutSubprocess(bulkReader io.Reader, bulkWriter io.Writer, req request) (*response, error) {
	req.preservedRoot = req.Root
	req.rootPrefix = string(os.PathSeparator)
	req.preservedDirectory = req.Directory
	req.preservedGlobs = slices.Clone(req.Globs)
	if !filepath.IsAbs(req.Directory) {
		req.Directory = filepath.Join(req.Root, cleanerReldirectory(req.Directory))
	}
	absoluteGlobs := make([]string, 0, len(req.Globs))
	for _, glob := range req.preservedGlobs {
		if filepath.IsAbs(glob) {
			relativeGlob, err := convertToRelSubdirectory(req.preservedRoot, glob)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error rewriting %q to be relative to %q: %v", glob, req.preservedRoot, err)
				os.Exit(1)
			}
			absoluteGlobs = append(absoluteGlobs, filepath.Join(req.Root, string(os.PathSeparator)+relativeGlob))
		} else {
			absoluteGlobs = append(absoluteGlobs, filepath.Join(req.Directory, cleanerReldirectory(glob)))
		}
	}
	req.Globs = absoluteGlobs
	resp, cb, err := copierHandler(bulkReader, bulkWriter, req)
	if err != nil {
		return nil, err
	}
	if cb != nil {
		if err = cb(); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func closeIfNotNilYet(f **os.File, what string) {
	if f != nil && *f != nil {
		err := (*f).Close()
		*f = nil
		if err != nil {
			logrus.Debugf("error closing %s: %v", what, err)
		}
	}
}

func copierWithSubprocess(bulkReader io.Reader, bulkWriter io.Writer, req request) (resp *response, err error) {
	if bulkReader == nil {
		bulkReader = bytes.NewReader([]byte{})
	}
	if bulkWriter == nil {
		bulkWriter = io.Discard
	}
	cmd := reexec.Command(copierCommand)
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}
	defer closeIfNotNilYet(&stdinRead, "stdin pipe reader")
	defer closeIfNotNilYet(&stdinWrite, "stdin pipe writer")
	encoder := json.NewEncoder(stdinWrite)
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}
	defer closeIfNotNilYet(&stdoutRead, "stdout pipe reader")
	defer closeIfNotNilYet(&stdoutWrite, "stdout pipe writer")
	decoder := json.NewDecoder(stdoutRead)
	bulkReaderRead, bulkReaderWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}
	defer closeIfNotNilYet(&bulkReaderRead, "child bulk content reader pipe, read end")
	defer closeIfNotNilYet(&bulkReaderWrite, "child bulk content reader pipe, write end")
	bulkWriterRead, bulkWriterWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}
	defer closeIfNotNilYet(&bulkWriterRead, "child bulk content writer pipe, read end")
	defer closeIfNotNilYet(&bulkWriterWrite, "child bulk content writer pipe, write end")
	cmd.Dir = "/"
	cmd.Env = append([]string{fmt.Sprintf("LOGLEVEL=%d", logrus.GetLevel())}, os.Environ()...)

	errorBuffer := bytes.Buffer{}
	cmd.Stdin = stdinRead
	cmd.Stdout = stdoutWrite
	cmd.Stderr = &errorBuffer
	cmd.ExtraFiles = []*os.File{bulkReaderRead, bulkWriterWrite}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting subprocess: %w", err)
	}
	cmdToWaitFor := cmd
	defer func() {
		if cmdToWaitFor != nil {
			if err := cmdToWaitFor.Wait(); err != nil {
				if errorBuffer.String() != "" {
					logrus.Debug(errorBuffer.String())
				}
			}
		}
	}()
	stdinRead.Close()
	stdinRead = nil
	stdoutWrite.Close()
	stdoutWrite = nil
	bulkReaderRead.Close()
	bulkReaderRead = nil
	bulkWriterWrite.Close()
	bulkWriterWrite = nil
	killAndReturn := func(err error, step string) error {
		if err2 := cmd.Process.Kill(); err2 != nil {
			return fmt.Errorf("killing subprocess: %v; %s: %w", err2, step, err)
		}
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.EPIPE) {
			err2 := cmd.Wait()
			if errorText := strings.TrimFunc(errorBuffer.String(), unicode.IsSpace); errorText != "" {
				err = fmt.Errorf("%s: %w", errorText, err)
			}
			if err2 != nil {
				return fmt.Errorf("waiting on subprocess: %v; %s: %w", err2, step, err)
			}
		}
		return fmt.Errorf("%v: %w", step, err)
	}
	if err = encoder.Encode(req); err != nil {
		return nil, killAndReturn(err, "error encoding work request for copier subprocess")
	}
	if err = decoder.Decode(&resp); err != nil {
		if errors.Is(err, io.EOF) && errorBuffer.Len() > 0 {
			return nil, killAndReturn(errors.New(errorBuffer.String()), "error in copier subprocess")
		}
		return nil, killAndReturn(err, "error decoding response from copier subprocess")
	}
	if err = encoder.Encode(&request{Request: requestQuit}); err != nil {
		return nil, killAndReturn(err, "error encoding quit request for copier subprocess")
	}
	stdinWrite.Close()
	stdinWrite = nil
	stdoutRead.Close()
	stdoutRead = nil
	var wg sync.WaitGroup
	var readError, writeError error
	wg.Add(1)
	go func() {
		_, writeError = io.Copy(bulkWriter, bulkWriterRead)
		bulkWriterRead.Close()
		bulkWriterRead = nil
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		_, readError = io.Copy(bulkReaderWrite, bulkReader)
		bulkReaderWrite.Close()
		bulkReaderWrite = nil
		wg.Done()
	}()
	wg.Wait()
	cmdToWaitFor = nil
	if err = cmd.Wait(); err != nil {
		if errorBuffer.String() != "" {
			err = fmt.Errorf("%s", errorBuffer.String())
		}
		return nil, err
	}
	if cmd.ProcessState.Exited() && !cmd.ProcessState.Success() {
		err = fmt.Errorf("subprocess exited with error")
		if errorBuffer.String() != "" {
			err = fmt.Errorf("%s", errorBuffer.String())
		}
		return nil, err
	}
	loggedOutput := strings.TrimSuffix(errorBuffer.String(), "\n")
	if len(loggedOutput) > 0 {
		for output := range strings.SplitSeq(loggedOutput, "\n") {
			logrus.Debug(output)
		}
	}
	if readError != nil {
		return nil, fmt.Errorf("passing bulk input to subprocess: %w", readError)
	}
	if writeError != nil {
		return nil, fmt.Errorf("passing bulk output from subprocess: %w", writeError)
	}
	return resp, nil
}

func copierMain() {
	var chrooted bool
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	previousRequestRoot := ""

	// Attempt a user and host lookup to force libc (glibc, and possibly others that use dynamic
	// modules to handle looking up user and host information) to load modules that match the libc
	// our binary is currently using.  Hopefully they're loaded on first use, so that they won't
	// need to be loaded after we've chrooted into the rootfs, which could include modules that
	// don't match our libc and which can't be loaded, or modules which we don't want to execute
	// because we don't trust their code.
	_, _ = user.Lookup("buildah")
	_, _ = net.LookupHost("localhost")

	// Set logging.
	if level := os.Getenv("LOGLEVEL"); level != "" {
		if ll, err := strconv.Atoi(level); err == nil {
			logrus.SetLevel(logrus.Level(ll))
		}
	}

	// Set up descriptors for receiving and sending tarstreams.
	bulkReader := os.NewFile(3, "bulk-reader")
	bulkWriter := os.NewFile(4, "bulk-writer")

	for {
		// Read a request.
		req := new(request)
		if err := decoder.Decode(req); err != nil {
			fmt.Fprintf(os.Stderr, "error decoding request from copier parent process: %v", err)
			os.Exit(1)
		}
		if req.Request == requestQuit {
			// Making Quit a specific request means that we could
			// run Stat() at a caller's behest before using the
			// same process for Get() or Put().  Maybe later.
			break
		}

		// Multiple requests should list the same root, because we
		// can't un-chroot to chroot to some other location.
		if previousRequestRoot != "" {
			// Check that we got the same input value for
			// where-to-chroot-to.
			if req.Root != previousRequestRoot {
				fmt.Fprintf(os.Stderr, "error: can't change location of chroot from %q to %q", previousRequestRoot, req.Root)
				os.Exit(1)
			}
			previousRequestRoot = req.Root
		} else {
			// Figure out where to chroot to, if we weren't told.
			if req.Root == "" {
				root, err := currentVolumeRoot()
				if err != nil {
					fmt.Fprintf(os.Stderr, "error determining root of current volume: %v", err)
					os.Exit(1)
				}
				req.Root = root
			}
			// Change to the specified root directory.
			var err error
			chrooted, err = chroot(req.Root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v", err)
				os.Exit(1)
			}
		}

		req.preservedRoot = req.Root
		req.rootPrefix = string(os.PathSeparator)
		req.preservedDirectory = req.Directory
		req.preservedGlobs = slices.Clone(req.Globs)
		if chrooted {
			// We'll need to adjust some things now that the root
			// directory isn't what it was.  Make the directory and
			// globs absolute paths for simplicity's sake.
			absoluteDirectory := req.Directory
			if !filepath.IsAbs(req.Directory) {
				absoluteDirectory = filepath.Join(req.Root, cleanerReldirectory(req.Directory))
			}
			relativeDirectory, err := convertToRelSubdirectory(req.preservedRoot, absoluteDirectory)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error rewriting %q to be relative to %q: %v", absoluteDirectory, req.preservedRoot, err)
				os.Exit(1)
			}
			req.Directory = filepath.Clean(string(os.PathSeparator) + relativeDirectory)
			absoluteGlobs := make([]string, 0, len(req.Globs))
			for i, glob := range req.preservedGlobs {
				if filepath.IsAbs(glob) {
					relativeGlob, err := convertToRelSubdirectory(req.preservedRoot, glob)
					if err != nil {
						fmt.Fprintf(os.Stderr, "error rewriting %q to be relative to %q: %v", glob, req.preservedRoot, err)
						os.Exit(1)
					}
					absoluteGlobs = append(absoluteGlobs, filepath.Clean(string(os.PathSeparator)+relativeGlob))
				} else {
					absoluteGlobs = append(absoluteGlobs, filepath.Join(req.Directory, cleanerReldirectory(req.Globs[i])))
				}
			}
			req.Globs = absoluteGlobs
			req.rootPrefix = req.Root
			req.Root = string(os.PathSeparator)
		} else {
			// Make the directory and globs absolute paths for
			// simplicity's sake.
			if !filepath.IsAbs(req.Directory) {
				req.Directory = filepath.Join(req.Root, cleanerReldirectory(req.Directory))
			}
			absoluteGlobs := make([]string, 0, len(req.Globs))
			for i, glob := range req.preservedGlobs {
				if filepath.IsAbs(glob) {
					absoluteGlobs = append(absoluteGlobs, req.Globs[i])
				} else {
					absoluteGlobs = append(absoluteGlobs, filepath.Join(req.Directory, cleanerReldirectory(req.Globs[i])))
				}
			}
			req.Globs = absoluteGlobs
		}
		resp, cb, err := copierHandler(bulkReader, bulkWriter, *req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error handling request %#v from copier parent process: %v", *req, err)
			os.Exit(1)
		}
		// Encode the response.
		if err := encoder.Encode(resp); err != nil {
			fmt.Fprintf(os.Stderr, "error encoding response %#v for copier parent process: %v", *req, err)
			os.Exit(1)
		}
		// If there's bulk data to transfer, run the callback to either
		// read or write it.
		if cb != nil {
			if err = cb(); err != nil {
				fmt.Fprintf(os.Stderr, "error during bulk transfer for %#v: %v", *req, err)
				os.Exit(1)
			}
		}
	}
}

func copierHandler(bulkReader io.Reader, bulkWriter io.Writer, req request) (*response, func() error, error) {
	// NewPatternMatcher splits patterns into components using
	// os.PathSeparator, implying that it expects OS-specific naming
	// conventions.
	excludes := req.Excludes()
	pm, err := fileutils.NewPatternMatcher(excludes)
	if err != nil {
		return nil, nil, fmt.Errorf("processing excludes list %v: %w", excludes, err)
	}

	var idMappings *idtools.IDMappings
	uidMap, gidMap := req.UIDMap(), req.GIDMap()
	if len(uidMap) > 0 && len(gidMap) > 0 {
		idMappings = idtools.NewIDMappingsFromMaps(uidMap, gidMap)
	}

	switch req.Request {
	default:
		return nil, nil, fmt.Errorf("not an implemented request type: %q", req.Request)
	case requestEval:
		resp := copierHandlerEval(req)
		return resp, nil, nil
	case requestStat:
		resp := copierHandlerStat(req, pm, idMappings)
		return resp, nil, nil
	case requestGet:
		return copierHandlerGet(bulkWriter, req, pm, idMappings)
	case requestPut:
		return copierHandlerPut(bulkReader, req, idMappings)
	case requestMkdir:
		return copierHandlerMkdir(req, idMappings)
	case requestRemove:
		resp := copierHandlerRemove(req)
		return resp, nil, nil
	case requestEnsure:
		resp := copierHandlerEnsure(req, idMappings)
		return resp, nil, nil
	case requestConditionalRemove:
		resp := copierHandlerConditionalRemove(req, idMappings)
		return resp, nil, nil
	case requestQuit:
		return nil, nil, nil
	}
}

// pathIsExcluded computes path relative to root, then asks the pattern matcher
// if the result is excluded.  Returns the relative path and the matcher's
// results.
func pathIsExcluded(root, path string, pm *fileutils.PatternMatcher) (string, bool, error) {
	rel, err := convertToRelSubdirectory(root, path)
	if err != nil {
		return "", false, fmt.Errorf("copier: error computing path of %q relative to root %q: %w", path, root, err)
	}
	if pm == nil {
		return rel, false, nil
	}
	if rel == "." {
		// special case
		return rel, false, nil
	}
	// Matches uses filepath.FromSlash() to convert candidates before
	// checking if they match the patterns it's been given, implying that
	// it expects Unix-style paths.
	matches, err := pm.Matches(filepath.ToSlash(rel)) //nolint:staticcheck
	if err != nil {
		return rel, false, fmt.Errorf("copier: error checking if %q is excluded: %w", rel, err)
	}
	if matches {
		return rel, true, nil
	}
	return rel, false, nil
}

// resolvePath resolves symbolic links in paths, treating the specified
// directory as the root.
// Resolving the path this way, and using the result, is in no way secure
// against another process manipulating the content that we're looking at, and
// it is not expected to be.
// This helps us approximate chrooted behavior on systems and in test cases
// where chroot isn't available.
func resolvePath(root, path string, evaluateFinalComponent bool, pm *fileutils.PatternMatcher) (string, error) {
	rel, err := convertToRelSubdirectory(root, path)
	if err != nil {
		return "", fmt.Errorf("making path %q relative to %q", path, root)
	}
	workingPath := root
	followed := 0
	components := strings.Split(rel, string(os.PathSeparator))
	excluded := false
	for len(components) > 0 {
		// if anything we try to examine is excluded, then resolution has to "break"
		_, thisExcluded, err := pathIsExcluded(root, filepath.Join(workingPath, components[0]), pm)
		if err != nil {
			return "", err
		}
		excluded = excluded || thisExcluded
		if !excluded {
			if target, err := os.Readlink(filepath.Join(workingPath, components[0])); err == nil && (len(components) != 1 || evaluateFinalComponent) {
				followed++
				if followed > maxLoopsFollowed {
					return "", &os.PathError{
						Op:   "open",
						Path: path,
						Err:  syscall.ELOOP,
					}
				}
				if filepath.IsAbs(target) || looksLikeAbs(target) {
					// symlink to an absolute path - prepend the
					// root directory to that absolute path to
					// replace the current location, and resolve
					// the remaining components
					workingPath = root
					components = append(strings.Split(target, string(os.PathSeparator)), components[1:]...)
					continue
				}
				// symlink to a relative path - add the link target to
				// the current location to get the next location, and
				// resolve the remaining components
				rel, err := convertToRelSubdirectory(root, filepath.Join(workingPath, target))
				if err != nil {
					return "", fmt.Errorf("making path %q relative to %q", filepath.Join(workingPath, target), root)
				}
				workingPath = root
				components = append(strings.Split(filepath.Clean(string(os.PathSeparator)+rel), string(os.PathSeparator)), components[1:]...)
				continue
			}
		}
		// append the current component's name to get the next location
		workingPath = filepath.Join(workingPath, components[0])
		if workingPath == filepath.Join(root, "..") {
			// attempted to go above the root using a relative path .., scope it
			workingPath = root
		}
		// ready to handle the next component
		components = components[1:]
	}
	return workingPath, nil
}

func copierHandlerEval(req request) *response {
	errorResponse := func(fmtspec string, args ...any) *response {
		return &response{Error: fmt.Sprintf(fmtspec, args...), Eval: evalResponse{}}
	}
	resolvedTarget, err := resolvePath(req.Root, req.Directory, true, nil)
	if err != nil {
		return errorResponse("copier: eval: error resolving %q: %v", req.Directory, err)
	}
	return &response{Eval: evalResponse{Evaluated: filepath.Join(req.rootPrefix, resolvedTarget)}}
}

func copierHandlerStat(req request, pm *fileutils.PatternMatcher, idMappings *idtools.IDMappings) *response {
	errorResponse := func(fmtspec string, args ...any) *response {
		return &response{Error: fmt.Sprintf(fmtspec, args...), Stat: statResponse{}}
	}
	if len(req.Globs) == 0 {
		return errorResponse("copier: stat: expected at least one glob pattern, got none")
	}
	var stats []*StatsForGlob
	for i, glob := range req.Globs {
		s := StatsForGlob{
			Glob: req.preservedGlobs[i],
		}
		// glob this pattern
		globMatched, err := extendedGlob(glob)
		if err != nil {
			s.Error = fmt.Sprintf("copier: stat: %q while matching glob pattern %q", err.Error(), glob)
		}

		if len(globMatched) == 0 && strings.ContainsAny(glob, "*?[") {
			continue
		}
		// collect the matches
		s.Globbed = make([]string, 0, len(globMatched))
		s.Results = make(map[string]*StatForItem)
		for _, globbed := range globMatched {
			rel, excluded, err := pathIsExcluded(req.Root, globbed, pm)
			if err != nil {
				return errorResponse("copier: stat: %v", err)
			}
			if excluded {
				continue
			}
			// if the glob was an absolute path, reconstruct the
			// path that we should hand back for the match
			var resultName string
			if filepath.IsAbs(req.preservedGlobs[i]) {
				resultName = filepath.Join(req.rootPrefix, globbed)
			} else {
				relResult := rel
				if req.Directory != req.Root {
					relResult, err = convertToRelSubdirectory(req.Directory, globbed)
					if err != nil {
						return errorResponse("copier: stat: error making %q relative to %q: %v", globbed, req.Directory, err)
					}
				}
				resultName = relResult
			}
			result := StatForItem{Name: resultName}
			s.Globbed = append(s.Globbed, resultName)
			s.Results[resultName] = &result
			// lstat the matched value
			linfo, err := os.Lstat(globbed)
			if err != nil {
				result.Error = err.Error()
				continue
			}
			result.Size = linfo.Size()
			result.Mode = linfo.Mode()
			result.UID, result.GID = -1, -1
			if uid, gid, err := owner(linfo); err == nil {
				if idMappings != nil && !idMappings.Empty() {
					hostPair := idtools.IDPair{UID: uid, GID: gid}
					uid, gid, err = idMappings.ToContainer(hostPair)
					if err != nil {
						return errorResponse("copier: stat: mapping host filesystem owners %#v to container filesystem owners: %w", hostPair, err)
					}
				}
				result.UID, result.GID = int64(uid), int64(gid)
			}
			result.ModTime = linfo.ModTime()
			result.IsDir = linfo.IsDir()
			result.IsRegular = result.Mode.IsRegular()
			result.IsSymlink = (linfo.Mode() & os.ModeType) == os.ModeSymlink
			checkForArchive := req.StatOptions.CheckForArchives
			if result.IsSymlink {
				// if the match was a symbolic link, read it
				immediateTarget, err := os.Readlink(globbed)
				if err != nil {
					result.Error = err.Error()
					continue
				}
				// record where it points, both by itself (it
				// could be a relative link) and in the context
				// of the chroot
				result.ImmediateTarget = immediateTarget
				resolvedTarget, err := resolvePath(req.Root, globbed, true, pm)
				if err != nil {
					return errorResponse("copier: stat: error resolving %q: %v", globbed, err)
				}
				// lstat the thing that we point to
				info, err := os.Lstat(resolvedTarget)
				if err != nil {
					result.Error = err.Error()
					continue
				}
				// replace IsArchive/IsDir/IsRegular with info about the target
				if info.Mode().IsRegular() && req.StatOptions.CheckForArchives {
					result.IsArchive = isArchivePath(resolvedTarget)
					checkForArchive = false
				}
				result.IsDir = info.IsDir()
				result.IsRegular = info.Mode().IsRegular()
			}
			if result.IsRegular && checkForArchive {
				// we were asked to check on this, and it
				// wasn't a symlink, in which case we'd have
				// already checked what the link points to
				result.IsArchive = isArchivePath(globbed)
			}
		}
		// no unskipped matches -> error
		if len(s.Globbed) == 0 {
			s.Globbed = nil
			s.Results = nil
			s.Error = fmt.Sprintf("copier: stat: %q: %v", glob, syscall.ENOENT)
		}
		stats = append(stats, &s)
	}
	// no matches -> error
	if len(stats) == 0 {
		s := StatsForGlob{
			Error: fmt.Sprintf("copier: stat: %q: %v", req.Globs, syscall.ENOENT),
		}
		stats = append(stats, &s)
	}
	return &response{Stat: statResponse{Globs: stats}}
}

func errorIsPermission(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied")
}

func getParents(path string, stopPath string) []string {
	out := []string{}
	for path != "/" && path != "." && path != stopPath {
		path = filepath.Dir(path)
		if path == stopPath {
			continue
		}
		out = append(out, path)
	}
	slices.Reverse(out)
	return out
}

func checkLinks(item string, req request, info os.FileInfo) (string, os.FileInfo, error) {
	// chase links. if we hit a dead end, we should just fail
	oldItem := item
	followedLinks := 0
	const maxFollowedLinks = 16
	for !req.GetOptions.NoDerefSymlinks && info.Mode()&os.ModeType == os.ModeSymlink && followedLinks < maxFollowedLinks {
		path, err := os.Readlink(item)
		if err != nil {
			continue
		}
		if filepath.IsAbs(path) || looksLikeAbs(path) {
			path = filepath.Join(req.Root, path)
		} else {
			path = filepath.Join(filepath.Dir(item), path)
		}
		item = path
		if _, err = convertToRelSubdirectory(req.Root, item); err != nil {
			return "", nil, fmt.Errorf("copier: get: computing path of %q(%q) relative to %q: %w", oldItem, item, req.Root, err)
		}
		if info, err = os.Lstat(item); err != nil {
			return "", nil, fmt.Errorf("copier: get: lstat %q(%q): %w", oldItem, item, err)
		}
		followedLinks++
	}
	if followedLinks >= maxFollowedLinks {
		return "", nil, fmt.Errorf("copier: get: resolving symlink %q(%q): %w", oldItem, item, syscall.ELOOP)
	}
	return item, info, nil
}

func copierHandlerGet(bulkWriter io.Writer, req request, pm *fileutils.PatternMatcher, idMappings *idtools.IDMappings) (*response, func() error, error) {
	statRequest := req
	statRequest.Request = requestStat
	statResponse := copierHandlerStat(req, pm, idMappings)
	errorResponse := func(fmtspec string, args ...any) (*response, func() error, error) {
		return &response{Error: fmt.Sprintf(fmtspec, args...), Stat: statResponse.Stat, Get: getResponse{}}, nil, nil
	}
	if statResponse.Error != "" {
		return errorResponse("%s", statResponse.Error)
	}
	if len(req.Globs) == 0 {
		return errorResponse("copier: get: expected at least one glob pattern, got 0")
	}
	// build a queue of items by globbing
	type queueItem struct {
		glob    string
		parents []string
	}
	var queue []queueItem
	globMatchedCount := 0
	for _, glob := range req.Globs {
		globMatched, err := extendedGlob(glob)
		if err != nil {
			return errorResponse("copier: get: glob %q: %v", glob, err)
		}
		for _, path := range globMatched {
			var parents []string
			if req.GetOptions.Parents {
				parents = getParents(path, req.Directory)
			}
			globMatchedCount++
			queue = append(queue, queueItem{glob: path, parents: parents})
		}
	}
	// no matches -> error
	if len(queue) == 0 {
		return errorResponse("copier: get: globs %v matched nothing (%d filtered out): %v", req.Globs, globMatchedCount, syscall.ENOENT)
	}
	topInfo, err := os.Stat(req.Directory)
	if err != nil {
		return errorResponse("copier: get: error reading info about directory %q: %v", req.Directory, err)
	}
	cb := func() error {
		tw := tar.NewWriter(bulkWriter)
		defer tw.Close()
		hardlinkChecker := new(hardlinkChecker)
		itemsCopied := 0
		addedParents := map[string]struct{}{}
		for i, qItem := range queue {
			item := qItem.glob
			// if we're not discarding the names of individual directories, keep track of this one
			relNamePrefix := ""
			if req.GetOptions.KeepDirectoryNames {
				relNamePrefix = filepath.Base(item)
			}
			// if the named thing-to-read is a symlink, dereference it
			info, err := os.Lstat(item)
			if err != nil {
				return fmt.Errorf("copier: get: lstat %q: %w", item, err)
			}
			if req.GetOptions.Parents && info.Mode().IsDir() {
				if !slices.Contains(qItem.parents, item) {
					qItem.parents = append(qItem.parents, item)
				}
			}
			// Copy parents in to tarball first if exists
			for _, parent := range qItem.parents {
				oldParent := parent
				parentInfo, err := os.Lstat(parent)
				if err != nil {
					return fmt.Errorf("copier: get: lstat %q: %w", parent, err)
				}
				parent, parentInfo, err = checkLinks(parent, req, parentInfo)
				if err != nil {
					return err
				}
				parentName, err := convertToRelSubdirectory(req.Directory, oldParent)
				if err != nil {
					return fmt.Errorf("copier: get: error computing path of %q relative to %q: %w", parent, req.Directory, err)
				}
				if parentName == "" || parentName == "." {
					// skip the "." entry
					continue
				}

				if _, ok := addedParents[parentName]; ok {
					continue
				}
				addedParents[parentName] = struct{}{}

				if err := copierHandlerGetOne(parentInfo, "", parentName, parent, req.GetOptions, tw, hardlinkChecker, idMappings); err != nil {
					if req.GetOptions.IgnoreUnreadable && errorIsPermission(err) {
						continue
					} else if errors.Is(err, os.ErrNotExist) {
						logrus.Warningf("copier: file disappeared while reading: %q", parent)
						return nil
					}
					return fmt.Errorf("copier: get: %q: %w", queue[i].glob, err)
				}
				itemsCopied++
			}

			item, info, err = checkLinks(item, req, info)
			if err != nil {
				return err
			}

			// evaluate excludes relative to the root directory
			if info.Mode().IsDir() {
				// we don't expand any of the contents that are archives
				options := req.GetOptions
				options.ExpandArchives = false
				walkfn := func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						if options.IgnoreUnreadable && errorIsPermission(err) {
							if info != nil && d.IsDir() {
								return filepath.SkipDir
							}
							return nil
						} else if errors.Is(err, os.ErrNotExist) {
							logrus.Warningf("copier: file disappeared while reading: %q", path)
							return nil
						}
						return fmt.Errorf("copier: get: error reading %q: %w", path, err)
					}
					if d.Type() == os.ModeSocket {
						logrus.Warningf("copier: skipping socket %q", d.Name())
						return nil
					}
					// compute the path of this item
					// relative to the top-level directory,
					// for the tar header
					rel, relErr := convertToRelSubdirectory(item, path)
					if relErr != nil {
						return fmt.Errorf("copier: get: error computing path of %q relative to top directory %q: %w", path, item, relErr)
					}
					// prefix the original item's name if we're keeping it
					if relNamePrefix != "" {
						rel = filepath.Join(relNamePrefix, rel)
					}
					if rel == "" || rel == "." {
						// skip the "." entry
						return nil
					}
					skippedPath, skip, err := pathIsExcluded(req.Root, path, pm)
					if err != nil {
						return err
					}
					if skip {
						if d.IsDir() {
							// if there are no "include
							// this anyway" patterns at
							// all, we don't need to
							// descend into this particular
							// directory if it's a directory
							if !pm.Exclusions() {
								return filepath.SkipDir
							}
							// if there are exclusion
							// patterns for which this
							// path is a prefix, we
							// need to keep descending
							for _, pattern := range pm.Patterns() {
								if !pattern.Exclusion() {
									continue
								}
								spec := strings.Trim(pattern.String(), string(os.PathSeparator))
								trimmedPath := strings.Trim(skippedPath, string(os.PathSeparator))
								if strings.HasPrefix(spec+string(os.PathSeparator), trimmedPath) {
									// we can't just skip over
									// this directory
									return nil
								}
							}
							// there are exclusions, but
							// none of them apply here
							return filepath.SkipDir
						}
						// skip this item, but if we're
						// a directory, a more specific
						// but-include-this for
						// something under it might
						// also be in the excludes list
						return nil
					}
					// if it's a symlink, read its target
					symlinkTarget := ""
					if d.Type() == os.ModeSymlink {
						target, err := os.Readlink(path)
						if err != nil {
							return fmt.Errorf("copier: get: readlink(%q(%q)): %w", rel, path, err)
						}
						symlinkTarget = target
					}
					info, err := d.Info()
					if err != nil {
						return err
					}
					// if it's a directory and we're staying on one device, and it's on a
					// different device than the one we started from, skip its contents
					var ok error
					if d.IsDir() && req.GetOptions.NoCrossDevice {
						if !sameDevice(topInfo, info) {
							ok = filepath.SkipDir
						}
					}
					if req.GetOptions.Parents {
						rel, err = convertToRelSubdirectory(req.Directory, path)
						if err != nil {
							return fmt.Errorf("copier: get: error computing path of %q relative to %q: %w", path, req.Root, err)
						}
					}
					// add the item to the outgoing tar stream
					if err := copierHandlerGetOne(info, symlinkTarget, rel, path, options, tw, hardlinkChecker, idMappings); err != nil {
						if req.GetOptions.IgnoreUnreadable && errorIsPermission(err) {
							return ok
						} else if errors.Is(err, os.ErrNotExist) {
							logrus.Warningf("copier: file disappeared while reading: %q", path)
							return nil
						}
						return err
					}
					return ok
				}
				// walk the directory tree, checking/adding items individually
				if err := filepath.WalkDir(item, walkfn); err != nil {
					return fmt.Errorf("copier: get: %q(%q): %w", queue[i].glob, item, err)
				}
				itemsCopied++
			} else {
				_, skip, err := pathIsExcluded(req.Root, item, pm)
				if err != nil {
					return err
				}
				if skip {
					continue
				}

				name := filepath.Base(queue[i].glob)
				if req.GetOptions.Parents {
					name, err = convertToRelSubdirectory(req.Directory, queue[i].glob)
					if err != nil {
						return fmt.Errorf("copier: get: error computing path of %q relative to %q: %w", item, req.Root, err)
					}
					if name == "" || name == "." {
						// skip the "." entry
						continue
					}
				}

				if err := copierHandlerGetOne(info, "", name, item, req.GetOptions, tw, hardlinkChecker, idMappings); err != nil {
					if req.GetOptions.IgnoreUnreadable && errorIsPermission(err) {
						continue
					}
					return fmt.Errorf("copier: get: %q: %w", queue[i].glob, err)
				}
				itemsCopied++
			}
		}
		if itemsCopied == 0 {
			return fmt.Errorf("copier: get: copied no items: %w", syscall.ENOENT)
		}
		return nil
	}
	return &response{Stat: statResponse.Stat, Get: getResponse{}}, cb, nil
}

func handleRename(rename map[string]string, name string) string {
	if rename == nil {
		return name
	}
	// header names always use '/', so use path instead of filepath to manipulate it
	if directMapping, ok := rename[name]; ok {
		return directMapping
	}
	prefix, remainder := path.Split(name)
	for prefix != "" {
		if mappedPrefix, ok := rename[prefix]; ok {
			return path.Join(mappedPrefix, remainder)
		}
		if prefix[len(prefix)-1] == '/' {
			prefix = prefix[:len(prefix)-1]
			if mappedPrefix, ok := rename[prefix]; ok {
				return path.Join(mappedPrefix, remainder)
			}
		}
		newPrefix, middlePart := path.Split(prefix)
		if newPrefix == prefix {
			return name
		}
		prefix = newPrefix
		remainder = path.Join(middlePart, remainder)
	}
	return name
}

// mapWithPrefixedKeysWithoutKeyPrefix returns a map containing every element
// of m that had p as a prefix in its (string) key, with that prefix stripped
// from its key. items are shallow-copied using assignment. if m is nil, the
// returned map will be nil, otherwise it will at least have been allocated
func mapWithPrefixedKeysWithoutKeyPrefix[K any](m map[string]K, p string) map[string]K {
	if m == nil {
		return m
	}
	cloned := make(map[string]K, len(m))
	for k, v := range m {
		if after, ok := strings.CutPrefix(k, p); ok {
			cloned[after] = v
		}
	}
	return cloned
}

func copierHandlerGetOne(srcfi os.FileInfo, symlinkTarget, name, contentPath string, options GetOptions, tw *tar.Writer, hardlinkChecker *hardlinkChecker, idMappings *idtools.IDMappings) error {
	// build the header using the name provided
	hdr, err := tar.FileInfoHeader(srcfi, symlinkTarget)
	if err != nil {
		return fmt.Errorf("generating tar header for %s (%s): %w", contentPath, symlinkTarget, err)
	}
	if name != "" {
		hdr.Name = filepath.ToSlash(name)
	}
	hdr.Uname, hdr.Gname = "", ""
	if options.Rename != nil {
		hdr.Name = handleRename(options.Rename, hdr.Name)
	}
	if options.StripSetuidBit {
		hdr.Mode &^= cISUID
	}
	if options.StripSetgidBit {
		hdr.Mode &^= cISGID
	}
	if options.StripStickyBit {
		hdr.Mode &^= cISVTX
	}
	// read extended attributes
	var xattrs map[string]string
	if !options.StripXattrs {
		xattrs, err = Lgetxattrs(contentPath)
		if err != nil {
			return fmt.Errorf("getting extended attributes for %q: %w", contentPath, err)
		}
		if len(xattrs) > 0 && hdr.PAXRecords == nil {
			hdr.PAXRecords = make(map[string]string, len(xattrs))
		}
	}
	for k, v := range xattrs {
		hdr.PAXRecords[xattrPAXRecordNamespace+k] = v
	}
	if hdr.Typeflag == tar.TypeReg {
		// if it's an archive and we're extracting archives, read the
		// file and spool out its contents in-line.  (if we just
		// inlined the whole file, we'd also be inlining the EOF marker
		// it contains)
		if options.ExpandArchives && isArchivePath(contentPath) {
			f, err := os.Open(contentPath)
			if err != nil {
				return fmt.Errorf("opening file for reading archive contents: %w", err)
			}
			defer f.Close()
			rc, _, err := compression.AutoDecompress(f)
			if err != nil {
				return fmt.Errorf("decompressing %s: %w", contentPath, err)
			}
			defer rc.Close()
			tr := tar.NewReader(rc)
			hdr, err := tr.Next()
			for err == nil {
				if options.Rename != nil {
					hdr.Name = handleRename(options.Rename, hdr.Name)
				}
				if options.Timestamp != nil {
					timestamp := options.Timestamp.UTC()
					hdr.ModTime = timestamp
					if !hdr.AccessTime.IsZero() {
						hdr.AccessTime = timestamp
					}
					if !hdr.ChangeTime.IsZero() {
						hdr.ChangeTime = timestamp
					}
				}
				if err = tw.WriteHeader(hdr); err != nil {
					return fmt.Errorf("writing tar header from %q to pipe: %w", contentPath, err)
				}
				if hdr.Size != 0 {
					n, err := io.Copy(tw, tr)
					if err != nil {
						return fmt.Errorf("extracting content from archive %s: %s: %w", contentPath, hdr.Name, err)
					}
					if n != hdr.Size {
						return fmt.Errorf("extracting contents of archive %s: incorrect length for %q", contentPath, hdr.Name)
					}
					tw.Flush()
				}
				hdr, err = tr.Next()
			}
			if err != io.EOF {
				return fmt.Errorf("extracting contents of archive %s: %w", contentPath, err)
			}
			return nil
		}
		// if this regular file is hard linked to something else we've
		// already added, set up to output a TypeLink entry instead of
		// a TypeReg entry
		target := hardlinkChecker.Check(srcfi)
		if target != "" {
			hdr.Typeflag = tar.TypeLink
			hdr.Linkname = filepath.ToSlash(target)
			hdr.Size = 0
		} else {
			// note the device/inode pair for this file
			hardlinkChecker.Add(srcfi, name)
		}
	}
	// map the ownership for the archive
	if idMappings != nil && !idMappings.Empty() {
		hostPair := idtools.IDPair{UID: hdr.Uid, GID: hdr.Gid}
		hdr.Uid, hdr.Gid, err = idMappings.ToContainer(hostPair)
		if err != nil {
			return fmt.Errorf("mapping host filesystem owners %#v to container filesystem owners: %w", hostPair, err)
		}
	}
	// force ownership and/or permissions, if requested
	if hdr.Typeflag == tar.TypeDir {
		if options.ChownDirs != nil {
			hdr.Uid, hdr.Gid = options.ChownDirs.UID, options.ChownDirs.GID
		}
		if options.ChmodDirs != nil {
			hdr.Mode = int64(*options.ChmodDirs)
		}
		if !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
	} else {
		if options.ChownFiles != nil {
			hdr.Uid, hdr.Gid = options.ChownFiles.UID, options.ChownFiles.GID
		}
		if options.ChmodFiles != nil {
			hdr.Mode = int64(*options.ChmodFiles)
		}
	}
	// read fflags, if any
	if err := archive.ReadFileFlagsToTarHeader(contentPath, hdr); err != nil {
		return fmt.Errorf("getting fflags: %w", err)
	}
	var f *os.File
	switch hdr.Typeflag {
	case tar.TypeReg:
		// open the file first so that we don't write a header for it if we can't actually read it
		f, err = os.Open(contentPath)
		if err != nil {
			return fmt.Errorf("opening file for adding its contents to archive: %w", err)
		}
		defer f.Close()
	case tar.TypeDir:
		// open the directory file first to make sure we can access it.
		f, err = os.Open(contentPath)
		if err != nil {
			return fmt.Errorf("opening directory for adding its contents to archive: %w", err)
		}
		defer f.Close()
	}
	if options.Timestamp != nil {
		timestamp := options.Timestamp.UTC()
		hdr.ModTime = timestamp
		if !hdr.AccessTime.IsZero() {
			hdr.AccessTime = timestamp
		}
		if !hdr.ChangeTime.IsZero() {
			hdr.ChangeTime = timestamp
		}
	}
	// output the header
	if err = tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("writing header for %s (%s): %w", contentPath, hdr.Name, err)
	}
	if hdr.Typeflag == tar.TypeReg {
		// output the content
		n, err := io.Copy(tw, f)
		if err != nil {
			return fmt.Errorf("copying %s: %w", contentPath, err)
		}
		if n != hdr.Size {
			return fmt.Errorf("copying %s: incorrect size (expected %d bytes, read %d bytes)", contentPath, n, hdr.Size)
		}
		tw.Flush()
	}
	return nil
}

func copierHandlerPut(bulkReader io.Reader, req request, idMappings *idtools.IDMappings) (*response, func() error, error) {
	errorResponse := func(fmtspec string, args ...any) (*response, func() error, error) {
		return &response{Error: fmt.Sprintf(fmtspec, args...), Put: putResponse{}}, nil, nil
	}
	dirUID, dirGID, defaultDirUID, defaultDirGID := 0, 0, 0, 0
	if req.PutOptions.ChownDirs != nil {
		dirUID, dirGID = req.PutOptions.ChownDirs.UID, req.PutOptions.ChownDirs.GID
		defaultDirUID, defaultDirGID = dirUID, dirGID
	}
	defaultDirMode := os.FileMode(0o755)
	if req.PutOptions.ChmodDirs != nil {
		defaultDirMode = *req.PutOptions.ChmodDirs
	}
	if req.PutOptions.DefaultDirOwner != nil {
		defaultDirUID, defaultDirGID = req.PutOptions.DefaultDirOwner.UID, req.PutOptions.DefaultDirOwner.GID
	}
	if req.PutOptions.DefaultDirMode != nil {
		defaultDirMode = *req.PutOptions.DefaultDirMode
	}
	var fileUID, fileGID *int
	if req.PutOptions.ChownFiles != nil {
		fileUID, fileGID = &req.PutOptions.ChownFiles.UID, &req.PutOptions.ChownFiles.GID
	}
	if idMappings != nil && !idMappings.Empty() {
		containerDirPair := idtools.IDPair{UID: dirUID, GID: dirGID}
		hostDirPair, err := idMappings.ToHost(containerDirPair)
		if err != nil {
			return errorResponse("copier: put: error mapping container filesystem owner %d:%d to host filesystem owners: %v", dirUID, dirGID, err)
		}
		dirUID, dirGID = hostDirPair.UID, hostDirPair.GID
		defaultDirUID, defaultDirGID = hostDirPair.UID, hostDirPair.GID
		if req.PutOptions.ChownFiles != nil {
			containerFilePair := idtools.IDPair{UID: *fileUID, GID: *fileGID}
			hostFilePair, err := idMappings.ToHost(containerFilePair)
			if err != nil {
				return errorResponse("copier: put: error mapping container filesystem owner %d:%d to host filesystem owners: %v", fileUID, fileGID, err)
			}
			fileUID, fileGID = &hostFilePair.UID, &hostFilePair.GID
		}
	}
	directoryModes := make(map[string]os.FileMode)
	ensureDirectoryUnderRoot := func(directory string) error {
		rel, err := convertToRelSubdirectory(req.Root, directory)
		if err != nil {
			return fmt.Errorf("%q is not a subdirectory of %q: %w", directory, req.Root, err)
		}
		subdir := ""
		for component := range strings.SplitSeq(rel, string(os.PathSeparator)) {
			subdir = filepath.Join(subdir, component)
			path := filepath.Join(req.Root, subdir)
			if err := os.Mkdir(path, 0o700); err == nil {
				if err = lchown(path, defaultDirUID, defaultDirGID); err != nil {
					return fmt.Errorf("copier: put: error setting owner of %q to %d:%d: %w", path, defaultDirUID, defaultDirGID, err)
				}
				// make a conditional note to set this directory's permissions
				// later, but not if we already had an explicitly-provided mode
				if _, ok := directoryModes[path]; !ok {
					directoryModes[path] = defaultDirMode
				}
			} else {
				// FreeBSD can return EISDIR for "mkdir /":
				// https://bugs.freebsd.org/bugzilla/show_bug.cgi?id=59739.
				if !errors.Is(err, os.ErrExist) && !errors.Is(err, syscall.EISDIR) {
					return fmt.Errorf("copier: put: error checking directory %q: %w", path, err)
				}
			}
		}
		return nil
	}
	makeDirectoryWriteable := func(directory string) error {
		if _, ok := directoryModes[directory]; !ok {
			st, err := os.Lstat(directory)
			if err != nil {
				return fmt.Errorf("copier: put: error reading permissions of directory %q: %w", directory, err)
			}
			mode := st.Mode()
			directoryModes[directory] = mode
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("copier: put: error making directory %q writable: %w", directory, err)
		}
		return nil
	}
	createFile := func(path string, tr *tar.Reader) (int64, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
		if err != nil && errors.Is(err, os.ErrExist) {
			if req.PutOptions.NoOverwriteDirNonDir {
				if st, err2 := os.Lstat(path); err2 == nil && st.IsDir() {
					return 0, fmt.Errorf("copier: put: error creating file at %q: %w", path, err)
				}
			}
			if err = os.RemoveAll(path); err != nil {
				if os.IsPermission(err) {
					if err := makeDirectoryWriteable(filepath.Dir(path)); err != nil {
						return 0, err
					}
					err = os.RemoveAll(path)
				}
				if err != nil {
					return 0, fmt.Errorf("copier: put: error removing item to be overwritten %q: %w", path, err)
				}
			}
			f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
		}
		if err != nil && os.IsPermission(err) {
			if err = makeDirectoryWriteable(filepath.Dir(path)); err != nil {
				return 0, err
			}
			f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
		}
		if err != nil {
			return 0, fmt.Errorf("copier: put: error opening file %q for writing: %w", path, err)
		}
		defer f.Close()
		n, err := io.Copy(f, tr)
		if err != nil {
			return n, fmt.Errorf("copier: put: error writing file %q: %w", path, err)
		}
		return n, nil
	}
	targetDirectory, err := resolvePath(req.Root, req.Directory, true, nil)
	if err != nil {
		return errorResponse("copier: put: error resolving %q: %v", req.Directory, err)
	}
	info, err := os.Lstat(targetDirectory)
	if err == nil {
		if !info.IsDir() {
			return errorResponse("copier: put: %s (%s): exists but is not a directory", req.Directory, targetDirectory)
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return errorResponse("copier: put: %s: %v", req.Directory, err)
		}
		if err := ensureDirectoryUnderRoot(req.Directory); err != nil {
			return errorResponse("copier: put: %v", err)
		}
	}
	cb := func() error {
		type directoryAndTimes struct {
			directory    string
			atime, mtime time.Time
		}
		var directoriesAndTimes []directoryAndTimes
		defer func() {
			for i := range directoriesAndTimes {
				directoryAndTimes := directoriesAndTimes[len(directoriesAndTimes)-i-1]
				if err := lutimes(false, directoryAndTimes.directory, directoryAndTimes.atime, directoryAndTimes.mtime); err != nil {
					logrus.Debugf("error setting access and modify timestamps on %q to %s and %s: %v", directoryAndTimes.directory, directoryAndTimes.atime, directoryAndTimes.mtime, err)
				}
			}
			for directory, mode := range directoryModes {
				if err := os.Chmod(directory, mode); err != nil {
					logrus.Debugf("error setting permissions of %q to 0%o: %v", directory, uint32(mode), err)
				}
			}
		}()
		ignoredItems := make(map[string]struct{})
		tr := tar.NewReader(bulkReader)
		hdr, err := tr.Next()
		for err == nil {
			nameBeforeRenaming := hdr.Name
			if len(hdr.Name) == 0 {
				// no name -> ignore the entry
				ignoredItems[nameBeforeRenaming] = struct{}{}
				hdr, err = tr.Next()
				continue
			}
			if req.PutOptions.Rename != nil {
				hdr.Name = handleRename(req.PutOptions.Rename, hdr.Name)
			}
			// figure out who should own this new item
			if idMappings != nil && !idMappings.Empty() {
				containerPair := idtools.IDPair{UID: hdr.Uid, GID: hdr.Gid}
				hostPair, err := idMappings.ToHost(containerPair)
				if err != nil {
					return fmt.Errorf("mapping container filesystem owner 0,0 to host filesystem owners: %w", err)
				}
				hdr.Uid, hdr.Gid = hostPair.UID, hostPair.GID
			}
			if hdr.Typeflag == tar.TypeDir {
				if req.PutOptions.ChownDirs != nil {
					hdr.Uid, hdr.Gid = dirUID, dirGID
				}
			} else {
				if req.PutOptions.ChownFiles != nil {
					hdr.Uid, hdr.Gid = *fileUID, *fileGID
				}
			}
			// make sure the parent directory exists, including for tar.TypeXGlobalHeader entries
			// that we otherwise ignore, because that's what docker build does with them
			path := filepath.Join(targetDirectory, cleanerReldirectory(filepath.FromSlash(hdr.Name)))
			if err := ensureDirectoryUnderRoot(filepath.Dir(path)); err != nil {
				return err
			}
			// figure out what the permissions should be
			if req.PutOptions.StripSetuidBit && hdr.Mode&cISUID == cISUID {
				hdr.Mode &^= cISUID
			}
			if req.PutOptions.StripSetgidBit && hdr.Mode&cISGID == cISGID {
				hdr.Mode &^= cISGID
			}
			if req.PutOptions.StripStickyBit && hdr.Mode&cISVTX == cISVTX {
				hdr.Mode &^= cISVTX
			}
			if hdr.Typeflag == tar.TypeDir {
				if req.PutOptions.ChmodDirs != nil {
					hdr.Mode = int64(*req.PutOptions.ChmodDirs)
				}
			} else {
				if req.PutOptions.ChmodFiles != nil {
					hdr.Mode = int64(*req.PutOptions.ChmodFiles)
				}
			}
			// create the new item
			devMajor := uint32(hdr.Devmajor)
			devMinor := uint32(hdr.Devminor)
			mode := os.FileMode(hdr.Mode) & os.ModePerm
			switch hdr.Typeflag {
			// no type flag for sockets
			default:
				return fmt.Errorf("unrecognized Typeflag %c", hdr.Typeflag)
			case tar.TypeReg:
				var written int64
				written, err = createFile(path, tr)
				// only check the length if there wasn't an error, which we'll
				// check along with errors for other types of entries
				if err == nil && written != hdr.Size {
					return fmt.Errorf("copier: put: error creating regular file %q: incorrect length (%d != %d)", path, written, hdr.Size)
				}
			case tar.TypeLink:
				var linkTarget string
				if _, ignoredTarget := ignoredItems[hdr.Linkname]; ignoredTarget {
					// hard link to an ignored item: skip this, too
					ignoredItems[nameBeforeRenaming] = struct{}{}
					goto nextHeader
				}
				if req.PutOptions.Rename != nil {
					hdr.Linkname = handleRename(req.PutOptions.Rename, hdr.Linkname)
				}
				if linkTarget, err = resolvePath(targetDirectory, filepath.Join(req.Root, filepath.FromSlash(hdr.Linkname)), true, nil); err != nil {
					return fmt.Errorf("resolving hardlink target path %q under root %q", hdr.Linkname, req.Root)
				}
				if err = os.Link(linkTarget, path); err != nil && errors.Is(err, os.ErrExist) {
					if req.PutOptions.NoOverwriteDirNonDir {
						if st, err := os.Lstat(path); err == nil && st.IsDir() {
							break
						}
					}
					if err = os.RemoveAll(path); err == nil {
						err = os.Link(linkTarget, path)
					}
				}
			case tar.TypeSymlink:
				// if req.PutOptions.Rename != nil {
				//	todo: the general solution requires resolving to an absolute path, handling
				//	renaming, and then possibly converting back to a relative symlink
				// }
				if err = os.Symlink(filepath.FromSlash(hdr.Linkname), filepath.FromSlash(path)); err != nil && errors.Is(err, os.ErrExist) {
					if req.PutOptions.NoOverwriteDirNonDir {
						if st, err := os.Lstat(path); err == nil && st.IsDir() {
							break
						}
					}
					if err = os.RemoveAll(path); err == nil {
						err = os.Symlink(filepath.FromSlash(hdr.Linkname), filepath.FromSlash(path))
					}
				}
			case tar.TypeChar:
				if req.PutOptions.IgnoreDevices {
					ignoredItems[nameBeforeRenaming] = struct{}{}
					goto nextHeader
				}
				if err = mknod(path, chrMode(0o600), int(mkdev(devMajor, devMinor))); err != nil && errors.Is(err, os.ErrExist) {
					if req.PutOptions.NoOverwriteDirNonDir {
						if st, err := os.Lstat(path); err == nil && st.IsDir() {
							break
						}
					}
					if err = os.RemoveAll(path); err == nil {
						err = mknod(path, chrMode(0o600), int(mkdev(devMajor, devMinor)))
					}
				}
			case tar.TypeBlock:
				if req.PutOptions.IgnoreDevices {
					ignoredItems[nameBeforeRenaming] = struct{}{}
					goto nextHeader
				}
				if err = mknod(path, blkMode(0o600), int(mkdev(devMajor, devMinor))); err != nil && errors.Is(err, os.ErrExist) {
					if req.PutOptions.NoOverwriteDirNonDir {
						if st, err := os.Lstat(path); err == nil && st.IsDir() {
							break
						}
					}
					if err = os.RemoveAll(path); err == nil {
						err = mknod(path, blkMode(0o600), int(mkdev(devMajor, devMinor)))
					}
				}
			case tar.TypeDir:
				// FreeBSD can return EISDIR for "mkdir /":
				// https://bugs.freebsd.org/bugzilla/show_bug.cgi?id=59739.
				if err = os.Mkdir(path, 0o700); err != nil && (errors.Is(err, os.ErrExist) || errors.Is(err, syscall.EISDIR)) {
					if st, stErr := os.Lstat(path); stErr == nil && !st.IsDir() {
						if req.PutOptions.NoOverwriteNonDirDir {
							break
						}
						if err = os.Remove(path); err == nil {
							err = os.Mkdir(path, 0o700)
						}
					} else {
						err = stErr
					}
					// either we removed it and retried, or it was a directory,
					// in which case we want to just add the new stuff under it
				}
				// make a note of the directory's times.  we
				// might create items under it, which will
				// cause the mtime to change after we correct
				// it, so we'll need to correct it again later
				directoriesAndTimes = append(directoriesAndTimes, directoryAndTimes{
					directory: path,
					atime:     hdr.AccessTime,
					mtime:     hdr.ModTime,
				})
				// set the mode here unconditionally, in case the directory is in
				// the archive more than once for whatever reason
				directoryModes[path] = mode
			case tar.TypeFifo:
				if err = mkfifo(path, 0o600); err != nil && errors.Is(err, os.ErrExist) {
					if req.PutOptions.NoOverwriteDirNonDir {
						if st, err := os.Lstat(path); err == nil && st.IsDir() {
							break
						}
					}
					if err = os.RemoveAll(path); err == nil {
						err = mkfifo(path, 0o600)
					}
				}
			case tar.TypeXGlobalHeader:
				// Per archive/tar, PAX uses these to specify key=value information
				// applies to all subsequent entries.  The one in reported in #2717,
				// https://www.openssl.org/source/openssl-1.1.1g.tar.gz, includes a
				// comment=(40 byte hex string) at the start, possibly a digest.
				// Don't try to create whatever path was used for the header.
				goto nextHeader
			}
			// check for errors
			if err != nil {
				return fmt.Errorf("copier: put: error creating %q: %w", path, err)
			}
			// set ownership
			if err = lchown(path, hdr.Uid, hdr.Gid); err != nil {
				return fmt.Errorf("copier: put: error setting ownership of %q to %d:%d: %w", path, hdr.Uid, hdr.Gid, err)
			}
			// set permissions, except for symlinks, since we don't
			// have an lchmod, and directories, which we'll fix up
			// on our way out so that we don't get tripped up by
			// directories which we're not supposed to be able to
			// write to, but which we'll need to create content in
			if hdr.Typeflag != tar.TypeSymlink && hdr.Typeflag != tar.TypeDir {
				if err = os.Chmod(path, mode); err != nil {
					return fmt.Errorf("copier: put: error setting permissions on %q to 0%o: %w", path, mode, err)
				}
			}
			// set other bits that might have been reset by chown()
			if hdr.Typeflag != tar.TypeSymlink {
				if hdr.Mode&cISUID == cISUID {
					mode |= os.ModeSetuid
				}
				if hdr.Mode&cISGID == cISGID {
					mode |= os.ModeSetgid
				}
				if hdr.Mode&cISVTX == cISVTX {
					mode |= os.ModeSticky
				}
				if hdr.Typeflag == tar.TypeDir {
					// if/when we do the final setting of permissions on this
					// directory, make sure to incorporate these bits, too
					directoryModes[path] = mode
				}
				if err = os.Chmod(path, mode); err != nil {
					return fmt.Errorf("copier: put: setting additional permissions on %q to 0%o: %w", path, mode, err)
				}
			}
			// set xattrs, including some that might have been reset by chown()
			if !req.PutOptions.StripXattrs {
				xattrs := mapWithPrefixedKeysWithoutKeyPrefix(hdr.PAXRecords, xattrPAXRecordNamespace)
				if err = Lsetxattrs(path, xattrs); err != nil {
					if !req.PutOptions.IgnoreXattrErrors {
						return fmt.Errorf("copier: put: error setting extended attributes on %q: %w", path, err)
					}
				}
			}
			// set time
			if hdr.AccessTime.IsZero() || hdr.AccessTime.Before(hdr.ModTime) {
				hdr.AccessTime = hdr.ModTime
			}
			if err = lutimes(hdr.Typeflag == tar.TypeSymlink, path, hdr.AccessTime, hdr.ModTime); err != nil {
				return fmt.Errorf("setting access and modify timestamps on %q to %s and %s: %w", path, hdr.AccessTime, hdr.ModTime, err)
			}
			// set fflags if supported
			if err := archive.WriteFileFlagsFromTarHeader(path, hdr); err != nil {
				return fmt.Errorf("copier: put: error setting fflags on %q: %w", path, err)
			}
		nextHeader:
			hdr, err = tr.Next()
		}
		if err != io.EOF {
			return fmt.Errorf("reading tar stream: expected EOF: %w", err)
		}
		return nil
	}
	return &response{Error: "", Put: putResponse{}}, cb, nil
}

func copierHandlerMkdir(req request, idMappings *idtools.IDMappings) (*response, func() error, error) {
	errorResponse := func(fmtspec string, args ...any) (*response, func() error, error) { //nolint:unparam
		return &response{Error: fmt.Sprintf(fmtspec, args...), Mkdir: mkdirResponse{}}, nil, nil
	}
	dirUID, dirGID := 0, 0
	if req.MkdirOptions.ChownNew != nil {
		dirUID, dirGID = req.MkdirOptions.ChownNew.UID, req.MkdirOptions.ChownNew.GID
	}
	dirMode := os.FileMode(0o755)
	if req.MkdirOptions.ChmodNew != nil {
		dirMode = *req.MkdirOptions.ChmodNew
	}
	if idMappings != nil && !idMappings.Empty() {
		containerDirPair := idtools.IDPair{UID: dirUID, GID: dirGID}
		hostDirPair, err := idMappings.ToHost(containerDirPair)
		if err != nil {
			return errorResponse("copier: mkdir: error mapping container filesystem owner %d:%d to host filesystem owners: %v", dirUID, dirGID, err)
		}
		dirUID, dirGID = hostDirPair.UID, hostDirPair.GID
	}

	directory, err := resolvePath(req.Root, req.Directory, true, nil)
	if err != nil {
		return errorResponse("copier: mkdir: error resolving %q: %v", req.Directory, err)
	}

	rel, err := convertToRelSubdirectory(req.Root, directory)
	if err != nil {
		return errorResponse("copier: mkdir: error computing path of %q relative to %q: %v", directory, req.Root, err)
	}

	subdir := ""
	var created []string
	for component := range strings.SplitSeq(rel, string(os.PathSeparator)) {
		subdir = filepath.Join(subdir, component)
		path := filepath.Join(req.Root, subdir)
		if err := os.Mkdir(path, 0o700); err == nil {
			if err = chown(path, dirUID, dirGID); err != nil {
				return errorResponse("copier: mkdir: error setting owner of %q to %d:%d: %v", path, dirUID, dirGID, err)
			}
			if err = chmod(path, dirMode); err != nil {
				return errorResponse("copier: mkdir: error setting permissions on %q to 0%o: %v", path, dirMode)
			}
			created = append(created, path)
		} else {
			// FreeBSD can return EISDIR for "mkdir /":
			// https://bugs.freebsd.org/bugzilla/show_bug.cgi?id=59739.
			if !errors.Is(err, os.ErrExist) && !errors.Is(err, syscall.EISDIR) {
				return errorResponse("copier: mkdir: error checking directory %q: %v", path, err)
			}
		}
	}
	// set timestamps last, in case we needed to create some nested directories, which would
	// update the timestamps on directories that we'd just set timestamps on, if we had done
	// that immediately
	if req.MkdirOptions.ModTimeNew != nil {
		when := *req.MkdirOptions.ModTimeNew
		for _, newDirectory := range created {
			if err = lutimes(false, newDirectory, when, when); err != nil {
				return errorResponse("copier: mkdir: error setting datestamp on %q: %v", newDirectory, err)
			}
		}
	}

	return &response{Error: "", Mkdir: mkdirResponse{}}, nil, nil
}

func copierHandlerRemove(req request) *response {
	errorResponse := func(fmtspec string, args ...any) *response {
		return &response{Error: fmt.Sprintf(fmtspec, args...), Remove: removeResponse{}}
	}
	resolvedTarget, err := resolvePath(req.Root, req.Directory, false, nil)
	if err != nil {
		return errorResponse("copier: remove: %v", err)
	}
	if req.RemoveOptions.All {
		err = os.RemoveAll(resolvedTarget)
	} else {
		err = os.Remove(resolvedTarget)
	}
	if err != nil {
		return errorResponse("copier: remove %q: %v", req.Directory, err)
	}
	return &response{Error: "", Remove: removeResponse{}}
}

// EnsurePath is a single item being passed to an Ensure() call.
type EnsurePath struct {
	Path     string          // a pathname, relative to the Directory, possibly relative to the root
	Typeflag byte            // can be either TypeReg or TypeDir, everything else is currently ignored
	ModTime  *time.Time      // mtime to set on newly-created items, default is to leave them be
	Chmod    *os.FileMode    // mode, defaults to 000 for files and 700 for directories
	Chown    *idtools.IDPair // owner settings to set on newly-created items, defaults to 0:0
}

// EnsureOptions controls parts of Ensure()'s behavior.
type EnsureOptions struct {
	UIDMap, GIDMap []idtools.IDMap // map from containerIDs to hostIDs in the chroot
	Paths          []EnsurePath
}

// EnsureParentPath is a parent (or grandparent, or...) directory of an item
// created by Ensure(), along with information about it, from before the item
// in question was created.  If the information about this directory hasn't
// changed when commit-time rolls around, it's most likely that this directory
// is only being considered for inclusion in the layer because it was pulled
// up, and it was not actually changed.
type EnsureParentPath = ConditionalRemovePath

// Ensure ensures that the specified mount point targets exist under the root.
// If the root directory is not specified, the current root directory is used.
// If root is specified and the current OS supports it, and the calling process
// has the necessary privileges, the operation is performed in a chrooted
// context.
// Returns a slice with the pathnames of items that needed to be created and a
// slice of affected parent directories and information about them.
func Ensure(root, directory string, options EnsureOptions) ([]string, []EnsureParentPath, error) {
	req := request{
		Request:       requestEnsure,
		Root:          root,
		Directory:     directory,
		EnsureOptions: options,
	}
	resp, err := copier(nil, nil, req)
	if err != nil {
		return nil, nil, err
	}
	if resp.Error != "" {
		return nil, nil, errors.New(resp.Error)
	}
	return resp.Ensure.Created, resp.Ensure.Noted, nil
}

func copierHandlerEnsure(req request, idMappings *idtools.IDMappings) *response {
	errorResponse := func(fmtspec string, args ...any) *response {
		return &response{Error: fmt.Sprintf(fmtspec, args...), Ensure: ensureResponse{}}
	}
	slices.SortFunc(req.EnsureOptions.Paths, func(a, b EnsurePath) int { return strings.Compare(a.Path, b.Path) })
	var created []string
	notedByName := map[string]EnsureParentPath{}
	for _, item := range req.EnsureOptions.Paths {
		uid, gid := 0, 0
		if item.Chown != nil {
			uid, gid = item.Chown.UID, item.Chown.UID
		}
		var mode os.FileMode
		switch item.Typeflag {
		case tar.TypeReg:
			mode = 0o000
		case tar.TypeDir:
			mode = 0o700
		default:
			continue
		}
		if item.Chmod != nil {
			mode = *item.Chmod
		}
		if idMappings != nil && !idMappings.Empty() {
			containerDirPair := idtools.IDPair{UID: uid, GID: gid}
			hostDirPair, err := idMappings.ToHost(containerDirPair)
			if err != nil {
				return errorResponse("copier: ensure: error mapping container filesystem owner %d:%d to host filesystem owners: %v", uid, gid, err)
			}
			uid, gid = hostDirPair.UID, hostDirPair.GID
		}
		directory, err := resolvePath(req.Root, req.Directory, true, nil)
		if err != nil {
			return errorResponse("copier: ensure: error resolving %q: %v", req.Directory, err)
		}

		rel, err := convertToRelSubdirectory(req.Root, directory)
		if err != nil {
			return errorResponse("copier: ensure: error computing path of %q relative to %q: %v", directory, req.Root, err)
		}

		subdir := ""
		components := strings.Split(filepath.Join(rel, item.Path), string(os.PathSeparator))
		components = slices.DeleteFunc(components, func(s string) bool { return s == "" || s == "." })
		for i, component := range components {
			parentPath := subdir
			if parentPath == "" {
				parentPath = "."
			}
			leaf := filepath.Join(parentPath, component)
			parentInfo, err := os.Stat(filepath.Join(req.Root, parentPath))
			if err != nil {
				return errorResponse("copier: ensure: checking datestamps on %q (%d: %v): %v", parentPath, i, components, err)
			}
			if parentPath != "." {
				parentModTime := parentInfo.ModTime().UTC()
				parentMode := parentInfo.Mode()
				uid, gid, err := owner(parentInfo)
				if err != nil {
					return errorResponse("copier: ensure: error reading owner of %q: %v", parentPath, err)
				}
				notedByName[parentPath] = EnsureParentPath{
					Path:    parentPath,
					ModTime: &parentModTime,
					Mode:    &parentMode,
					Owner:   &idtools.IDPair{UID: uid, GID: gid},
				}
			}
			if i < len(components)-1 || item.Typeflag == tar.TypeDir {
				err = os.Mkdir(filepath.Join(req.Root, leaf), mode)
				subdir = leaf
			} else if item.Typeflag == tar.TypeReg {
				var f *os.File
				if f, err = os.OpenFile(filepath.Join(req.Root, leaf), os.O_CREATE|os.O_EXCL|os.O_RDWR, mode); err == nil {
					f.Close()
				}
			} else {
				continue
			}
			if err == nil {
				createdLeaf := leaf
				if len(createdLeaf) > 1 {
					createdLeaf = strings.TrimPrefix(createdLeaf, string(os.PathSeparator))
				}
				created = append(created, createdLeaf)
				if err = chown(filepath.Join(req.Root, leaf), uid, uid); err != nil {
					return errorResponse("copier: ensure: error setting owner of %q to %d:%d: %v", leaf, uid, gid, err)
				}
				if err = chmod(filepath.Join(req.Root, leaf), mode); err != nil {
					return errorResponse("copier: ensure: error setting permissions on %q to 0%o: %v", leaf, mode)
				}
				if item.ModTime != nil {
					if err := os.Chtimes(filepath.Join(req.Root, leaf), *item.ModTime, *item.ModTime); err != nil {
						return errorResponse("copier: ensure: resetting datestamp on %q: %v", leaf, err)
					}
				}
			} else {
				// FreeBSD can return EISDIR for "mkdir /":
				// https://bugs.freebsd.org/bugzilla/show_bug.cgi?id=59739.
				if !errors.Is(err, os.ErrExist) && !errors.Is(err, syscall.EISDIR) {
					return errorResponse("copier: ensure: error checking item %q: %v", leaf, err)
				}
			}
			if err := os.Chtimes(filepath.Join(req.Root, parentPath), parentInfo.ModTime(), parentInfo.ModTime()); err != nil {
				return errorResponse("copier: ensure: resetting datestamp on %q: %v", parentPath, err)
			}
		}
	}
	slices.Sort(created)
	noted := make([]EnsureParentPath, 0, len(notedByName))
	for _, n := range notedByName {
		if slices.Contains(created, n.Path) {
			continue
		}
		noted = append(noted, n)
	}
	slices.SortFunc(noted, func(a, b EnsureParentPath) int { return strings.Compare(a.Path, b.Path) })
	return &response{Error: "", Ensure: ensureResponse{Created: created, Noted: noted}}
}

// ConditionalRemovePath is a single item being passed to an ConditionalRemove() call.
type ConditionalRemovePath struct {
	Path    string          // a pathname, relative to the Directory, possibly relative to the root
	ModTime *time.Time      // mtime to expect this item to have, if it's a condition
	Mode    *os.FileMode    // mode to expect this item to have, if it's a condition
	Owner   *idtools.IDPair // owner to expect this item to have, if it's a condition
}

// ConditionalRemoveOptions controls parts of ConditionalRemove()'s behavior.
type ConditionalRemoveOptions struct {
	UIDMap, GIDMap []idtools.IDMap // map from containerIDs to hostIDs in the chroot
	Paths          []ConditionalRemovePath
}

// ConditionalRemove removes the set of named items if they're present and
// currently match the additional conditions, returning the list of items it
// removed.  Directories will also only be removed if they have no contents,
// and will be left in place otherwise.
func ConditionalRemove(root, directory string, options ConditionalRemoveOptions) ([]string, error) {
	req := request{
		Request:                  requestConditionalRemove,
		Root:                     root,
		Directory:                directory,
		ConditionalRemoveOptions: options,
	}
	resp, err := copier(nil, nil, req)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.ConditionalRemove.Removed, nil
}

func copierHandlerConditionalRemove(req request, idMappings *idtools.IDMappings) *response {
	errorResponse := func(fmtspec string, args ...any) *response {
		return &response{Error: fmt.Sprintf(fmtspec, args...), ConditionalRemove: conditionalRemoveResponse{}}
	}
	slices.SortFunc(req.ConditionalRemoveOptions.Paths, func(a, b ConditionalRemovePath) int { return strings.Compare(b.Path, a.Path) })
	var removed []string
	for _, item := range req.ConditionalRemoveOptions.Paths {
		uid, gid := 0, 0
		if item.Owner != nil {
			uid, gid = item.Owner.UID, item.Owner.GID
		}
		if idMappings != nil && !idMappings.Empty() {
			containerDirPair := idtools.IDPair{UID: uid, GID: gid}
			hostDirPair, err := idMappings.ToHost(containerDirPair)
			if err != nil {
				return errorResponse("copier: conditionalRemove: error mapping container filesystem owner %d:%d to host filesystem owners: %v", uid, gid, err)
			}
			uid, gid = hostDirPair.UID, hostDirPair.GID
		}
		directory, err := resolvePath(req.Root, req.Directory, true, nil)
		if err != nil {
			return errorResponse("copier: conditionalRemove: error resolving %q: %v", req.Directory, err)
		}

		rel, err := convertToRelSubdirectory(req.Root, directory)
		if err != nil {
			return errorResponse("copier: conditionalRemove: error computing path of %q relative to %q: %v", directory, req.Root, err)
		}

		components := strings.Split(filepath.Join(rel, item.Path), string(os.PathSeparator))
		components = slices.DeleteFunc(components, func(s string) bool { return s == "" || s == "." })
		if len(components) == 0 {
			continue
		}
		itemPath := filepath.Join(append([]string{req.Root}, components...)...)
		itemInfo, err := os.Lstat(itemPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return errorResponse("copier: conditionalRemove: checking on candidate %q: %v", itemPath, err)
			}
			// okay?
			removed = append(removed, item.Path)
			continue
		}
		parentPath := filepath.Dir(itemPath)
		parentInfo, err := os.Stat(parentPath)
		if err != nil {
			return errorResponse("copier: conditionalRemove: checking on parent directory %q: %v", parentPath, err)
		}

		if item.Mode != nil && itemInfo.Mode().Perm()&fs.ModePerm != *item.Mode&fs.ModePerm {
			// mismatch, modified? ignore
			continue
		}
		if item.ModTime != nil && !item.ModTime.Equal(itemInfo.ModTime()) {
			// mismatch, modified? ignore
			continue
		}
		if item.Owner != nil {
			ownerUID, ownerGID, err := owner(itemInfo)
			if err != nil {
				return errorResponse("copier: conditionalRemove: checking ownership of %q: %v", itemPath, err)
			}
			if uid != ownerUID || gid != ownerGID {
				// mismatch, modified? ignore
				continue
			}
		}
		if err := os.Remove(itemPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			if !errors.Is(err, syscall.EEXIST) && !errors.Is(err, syscall.ENOTEMPTY) {
				return errorResponse("copier: conditionalRemove: removing %q: %v", itemPath, err)
			}
			// okay? not removed, but it wasn't empty, so okay?
			continue
		}
		removed = append(removed, item.Path)
		if err := os.Chtimes(parentPath, parentInfo.ModTime(), parentInfo.ModTime()); err != nil {
			return errorResponse("copier: conditionalRemove: resetting datestamp on %q: %v", parentPath, err)
		}
	}
	slices.Sort(removed)
	return &response{Error: "", ConditionalRemove: conditionalRemoveResponse{Removed: removed}}
}
