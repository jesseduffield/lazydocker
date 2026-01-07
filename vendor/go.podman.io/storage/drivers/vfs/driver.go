package vfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"github.com/vbatts/tar-split/tar/storage"
	graphdriver "go.podman.io/storage/drivers"
	"go.podman.io/storage/internal/dedup"
	"go.podman.io/storage/internal/tempdir"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/directory"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/parsers"
	"go.podman.io/storage/pkg/system"
)

const (
	defaultPerms = os.FileMode(0o555)
	tempDirName  = "tempdirs"
)

func init() {
	graphdriver.MustRegister("vfs", Init)
}

// Init returns a new VFS driver.
// This sets the home directory for the driver and returns NaiveDiffDriver.
func Init(home string, options graphdriver.Options) (graphdriver.Driver, error) {
	d := &Driver{
		name:       "vfs",
		home:       home,
		imageStore: options.ImageStore,
	}

	if err := os.MkdirAll(filepath.Join(home, "dir"), 0o700); err != nil {
		return nil, err
	}
	for _, option := range options.DriverOptions {
		key, val, err := parsers.ParseKeyValueOpt(option)
		if err != nil {
			return nil, err
		}
		key = strings.ToLower(key)
		switch key {
		case "vfs.imagestore", ".imagestore":
			d.additionalHomes = slices.AppendSeq(d.additionalHomes, strings.SplitSeq(val, ","))
			continue
		case "vfs.mountopt":
			return nil, fmt.Errorf("vfs driver does not support mount options")
		case ".ignore_chown_errors", "vfs.ignore_chown_errors":
			logrus.Debugf("vfs: ignore_chown_errors=%s", val)
			var err error
			d.ignoreChownErrors, err = strconv.ParseBool(val)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("vfs driver does not support %s options", key)
		}
	}

	d.updater = graphdriver.NewNaiveLayerIDMapUpdater(d)
	d.naiveDiff = graphdriver.NewNaiveDiffDriver(d, d.updater)

	return d, nil
}

// Driver holds information about the driver, home directory of the driver.
// Driver implements graphdriver.ProtoDriver. It uses only basic vfs operations.
// In order to support layering, files are copied from the parent layer into the new layer. There is no copy-on-write support.
// Driver must be wrapped in NaiveDiffDriver to be used as a graphdriver.Driver
type Driver struct {
	name              string
	home              string
	additionalHomes   []string
	ignoreChownErrors bool
	naiveDiff         graphdriver.DiffDriver
	updater           graphdriver.LayerIDMapUpdater
	imageStore        string
}

func (d *Driver) String() string {
	return "vfs"
}

// Status is used for implementing the graphdriver.ProtoDriver interface. VFS does not currently have any status information.
func (d *Driver) Status() [][2]string {
	return nil
}

// Metadata is used for implementing the graphdriver.ProtoDriver interface. VFS does not currently have any meta data.
func (d *Driver) Metadata(id string) (map[string]string, error) {
	return nil, nil //nolint: nilnil
}

// Cleanup is used to implement graphdriver.ProtoDriver. There is no cleanup required for this driver.
func (d *Driver) Cleanup() error {
	return nil
}

type fileGetNilCloser struct {
	storage.FileGetter
}

func (f fileGetNilCloser) Close() error {
	return nil
}

// DiffGetter returns a FileGetCloser that can read files from the directory that
// contains files for the layer differences. Used for direct access for tar-split.
func (d *Driver) DiffGetter(id string) (graphdriver.FileGetCloser, error) {
	p := d.dir(id)
	return fileGetNilCloser{storage.NewPathFileGetter(p)}, nil
}

// CreateFromTemplate creates a layer with the same contents and parent as another layer.
func (d *Driver) CreateFromTemplate(id, template string, templateIDMappings *idtools.IDMappings, parent string, parentIDMappings *idtools.IDMappings, opts *graphdriver.CreateOpts, readWrite bool) error {
	if readWrite {
		return d.CreateReadWrite(id, template, opts)
	}
	return d.Create(id, template, opts)
}

// ApplyDiff applies the new layer into a root
func (d *Driver) ApplyDiff(id, parent string, options graphdriver.ApplyDiffOpts) (size int64, err error) {
	if d.ignoreChownErrors {
		options.IgnoreChownErrors = d.ignoreChownErrors
	}
	return d.naiveDiff.ApplyDiff(id, parent, options)
}

// CreateReadWrite creates a layer that is writable for use as a container
// file system.
func (d *Driver) CreateReadWrite(id, parent string, opts *graphdriver.CreateOpts) error {
	return d.create(id, parent, opts, false)
}

// Create prepares the filesystem for the VFS driver and copies the directory for the given id under the parent.
func (d *Driver) Create(id, parent string, opts *graphdriver.CreateOpts) error {
	return d.create(id, parent, opts, true)
}

func (d *Driver) create(id, parent string, opts *graphdriver.CreateOpts, ro bool) (retErr error) {
	if opts != nil && len(opts.StorageOpt) != 0 {
		return fmt.Errorf("--storage-opt is not supported for vfs")
	}

	var uidMaps []idtools.IDMap
	var gidMaps []idtools.IDMap

	if opts != nil && opts.IDMappings != nil {
		uidMaps = opts.IDMappings.UIDs()
		gidMaps = opts.IDMappings.GIDs()
	}

	rootUID, rootGID, err := idtools.GetRootUIDGID(uidMaps, gidMaps)
	if err != nil {
		return err
	}

	dir := d.dir2(id, ro)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}

	defer func() {
		if retErr != nil {
			os.RemoveAll(dir)
		}
	}()

	rootPerms := defaultPerms
	if runtime.GOOS == "darwin" {
		rootPerms = os.FileMode(0o700)
	}

	idPair := idtools.IDPair{UID: rootUID, GID: rootGID}
	if parent != "" {
		st, err := system.Stat(d.dir(parent))
		if err != nil {
			return err
		}
		rootPerms = os.FileMode(st.Mode())
		idPair.UID = int(st.UID())
		idPair.GID = int(st.GID())
	}
	if err := idtools.MkdirAllAndChownNew(dir, rootPerms, idPair); err != nil {
		return err
	}
	labelOpts := []string{"level:s0"}
	if _, mountLabel, err := label.InitLabels(labelOpts); err == nil {
		if err := label.SetFileLabel(dir, mountLabel); err != nil {
			logrus.Debugf("Set %s label to %q file ended with error: %v", mountLabel, dir, err)
		}
	}
	if parent != "" {
		parentDir, err := d.Get(parent, graphdriver.MountOpts{})
		if err != nil {
			return fmt.Errorf("%s: %w", parent, err)
		}
		if err := dirCopy(parentDir, dir); err != nil {
			return err
		}
	}

	return nil
}

func (d *Driver) dir2(id string, useImageStore bool) string {
	var homedir string

	if useImageStore && d.imageStore != "" {
		homedir = filepath.Join(d.imageStore, d.String(), "dir", filepath.Base(id))
	} else {
		homedir = filepath.Join(d.home, "dir", filepath.Base(id))
	}
	if err := fileutils.Exists(homedir); err != nil {
		additionalHomes := d.additionalHomes[:]
		if d.imageStore != "" {
			additionalHomes = append(additionalHomes, d.imageStore)
		}
		for _, home := range additionalHomes {
			candidate := filepath.Join(home, d.String(), "dir", filepath.Base(id))
			fi, err := os.Stat(candidate)
			if err == nil && fi.IsDir() {
				return candidate
			}
		}
	}
	return homedir
}

func (d *Driver) dir(id string) string {
	return d.dir2(id, false)
}

// Remove deletes the content from the directory for a given id.
func (d *Driver) Remove(id string) error {
	return system.EnsureRemoveAll(d.dir(id))
}

func (d *Driver) GetTempDirRootDirs() []string {
	tempDirs := []string{filepath.Join(d.home, tempDirName)}
	// Include imageStore temp directory if it's configured
	// Writable layers can only be in d.home or d.imageStore, not in additionalHomes (which are read-only)
	if d.imageStore != "" {
		tempDirs = append(tempDirs, filepath.Join(d.imageStore, d.String(), tempDirName))
	}
	return tempDirs
}

// Determine the correct temp directory root based on where the layer actually exists.
func (d *Driver) getTempDirRoot(id string) string {
	layerDir := d.dir(id)
	if d.imageStore != "" {
		expectedLayerDir := filepath.Join(d.imageStore, d.String(), "dir", filepath.Base(id))
		if layerDir == expectedLayerDir {
			return filepath.Join(d.imageStore, d.String(), tempDirName)
		}
	}
	return filepath.Join(d.home, tempDirName)
}

func (d *Driver) DeferredRemove(id string) (tempdir.CleanupTempDirFunc, error) {
	tempDirRoot := d.getTempDirRoot(id)
	t, err := tempdir.NewTempDir(tempDirRoot)
	if err != nil {
		return nil, err
	}

	layerDir := d.dir(id)
	if err := t.StageDeletion(layerDir); err != nil {
		return t.Cleanup, err
	}
	return t.Cleanup, nil
}

// Get returns the directory for the given id.
func (d *Driver) Get(id string, options graphdriver.MountOpts) (_ string, retErr error) {
	dir := d.dir(id)

	for _, opt := range options.Options {
		if opt == "ro" {
			// ignore "ro" option
			continue
		}
		return "", fmt.Errorf("vfs driver does not support mount options")
	}
	if st, err := os.Stat(dir); err != nil {
		return "", err
	} else if !st.IsDir() {
		return "", fmt.Errorf("%s: not a directory", dir)
	}
	return dir, nil
}

// Put is a noop for vfs that return nil for the error, since this driver has no runtime resources to clean up.
func (d *Driver) Put(id string) error {
	// The vfs driver has no runtime resources (e.g. mounts)
	// to clean up, so we don't need anything here
	return nil
}

// ReadWriteDiskUsage returns the disk usage of the writable directory for the ID.
// For VFS, it queries the directory for this ID.
func (d *Driver) ReadWriteDiskUsage(id string) (*directory.DiskUsage, error) {
	return directory.Usage(d.dir(id))
}

// Exists checks to see if the directory exists for the given id.
func (d *Driver) Exists(id string) bool {
	err := fileutils.Exists(d.dir(id))
	return err == nil
}

// List layers (not including additional image stores)
func (d *Driver) ListLayers() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.home, "dir"))
	if err != nil {
		return nil, err
	}

	layers := make([]string, 0)

	for _, entry := range entries {
		id := entry.Name()
		// Does it look like a datadir directory?
		if !entry.IsDir() {
			continue
		}

		layers = append(layers, id)
	}

	return layers, err
}

// AdditionalImageStores returns additional image stores supported by the driver
func (d *Driver) AdditionalImageStores() []string {
	if len(d.additionalHomes) > 0 {
		return d.additionalHomes
	}
	return nil
}

// SupportsShifting tells whether the driver support shifting of the UIDs/GIDs to the provided mapping in an userNS
func (d *Driver) SupportsShifting(uidmap, gidmap []idtools.IDMap) bool {
	return d.updater.SupportsShifting(uidmap, gidmap)
}

// UpdateLayerIDMap updates ID mappings in a from matching the ones specified
// by toContainer to those specified by toHost.
func (d *Driver) UpdateLayerIDMap(id string, toContainer, toHost *idtools.IDMappings, mountLabel string) error {
	if err := d.updater.UpdateLayerIDMap(id, toContainer, toHost, mountLabel); err != nil {
		return err
	}
	dir := d.dir(id)
	rootIDs, err := toHost.ToHost(idtools.IDPair{UID: 0, GID: 0})
	if err != nil {
		return err
	}
	return os.Chown(dir, rootIDs.UID, rootIDs.GID)
}

// Changes produces a list of changes between the specified layer
// and its parent layer. If parent is "", then all changes will be ADD changes.
func (d *Driver) Changes(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) ([]archive.Change, error) {
	return d.naiveDiff.Changes(id, idMappings, parent, parentMappings, mountLabel)
}

// Diff produces an archive of the changes between the specified
// layer and its parent layer which may be "".
func (d *Driver) Diff(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (io.ReadCloser, error) {
	return d.naiveDiff.Diff(id, idMappings, parent, parentMappings, mountLabel)
}

// DiffSize calculates the changes between the specified id
// and its parent and returns the size in bytes of the changes
// relative to its base filesystem directory.
func (d *Driver) DiffSize(id string, idMappings *idtools.IDMappings, parent string, parentMappings *idtools.IDMappings, mountLabel string) (size int64, err error) {
	return d.naiveDiff.DiffSize(id, idMappings, parent, parentMappings, mountLabel)
}

// Dedup performs deduplication of the driver's storage.
func (d *Driver) Dedup(req graphdriver.DedupArgs) (graphdriver.DedupResult, error) {
	var dirs []string
	for _, layer := range req.Layers {
		dir := d.dir2(layer, false)
		if err := fileutils.Exists(dir); err == nil {
			dirs = append(dirs, dir)
		}
	}
	r, err := dedup.DedupDirs(dirs, req.Options)
	if err != nil {
		return graphdriver.DedupResult{}, err
	}
	return graphdriver.DedupResult{Deduped: r.Deduped}, nil
}
