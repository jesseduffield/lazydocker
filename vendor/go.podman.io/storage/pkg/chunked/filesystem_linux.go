package chunked

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/vbatts/tar-split/archive/tar"
	driversCopy "go.podman.io/storage/drivers/copy"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chunked/internal/minimal"
	storagePath "go.podman.io/storage/pkg/chunked/internal/path"
	"golang.org/x/sys/unix"
)

// procPathForFile returns an absolute path in /proc which
// refers to the file; see procPathForFd.
func procPathForFile(f *os.File) string {
	return procPathForFd(int(f.Fd()))
}

// procPathForFd returns an absolute path in /proc which
// refers to the file; this allows passing a file descriptor
// in places that don't accept a file descriptor.
func procPathForFd(fd int) string {
	return fmt.Sprintf("/proc/self/fd/%d", fd)
}

// fileMetadata is a wrapper around minimal.FileMetadata with additional private fields that
// are not part of the TOC document.
// Type: TypeChunk entries are stored in Chunks, the primary [fileMetadata] entries never use TypeChunk.
type fileMetadata struct {
	minimal.FileMetadata

	// chunks stores the TypeChunk entries relevant to this entry when FileMetadata.Type == TypeReg.
	chunks []*minimal.FileMetadata

	// skipSetAttrs is set when the file attributes must not be
	// modified, e.g. it is a hard link from a different source,
	// or a composefs file.
	skipSetAttrs bool
}

// splitPath takes a file path as input and returns two components: dir and base.
// Differently than filepath.Split(), this function handles some edge cases.
// If the path refers to a file in the root directory, the returned dir is "/".
// The returned base value is never empty, it never contains any slash and the
// value "..".
func splitPath(path string) (string, string, error) {
	path = storagePath.CleanAbsPath(path)
	dir, base := filepath.Split(path)
	if base == "" {
		base = "."
	}
	// Remove trailing slashes from dir, but make sure that "/" is preserved.
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "/"
	}

	if strings.Contains(base, "/") {
		// This should never happen, but be safe as the base is passed to *at syscalls.
		return "", "", fmt.Errorf("internal error: splitPath(%q) contains a slash", path)
	}
	return dir, base, nil
}

func doHardLink(dirfd, srcFd int, destFile string) error {
	destDir, destBase, err := splitPath(destFile)
	if err != nil {
		return err
	}
	destDirFd := dirfd
	if destDir != "/" {
		f, err := openOrCreateDirUnderRoot(dirfd, destDir, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		destDirFd = int(f.Fd())
	}

	doLink := func() error {
		// Using unix.AT_EMPTY_PATH requires CAP_DAC_READ_SEARCH while this variant that uses
		// /proc/self/fd doesn't and can be used with rootless.
		srcPath := procPathForFd(srcFd)
		err := unix.Linkat(unix.AT_FDCWD, srcPath, destDirFd, destBase, unix.AT_SYMLINK_FOLLOW)
		if err != nil {
			return &fs.PathError{Op: "linkat", Path: destFile, Err: err}
		}
		return nil
	}

	err = doLink()

	// if the destination exists, unlink it first and try again
	if err != nil && os.IsExist(err) {
		if err := unix.Unlinkat(destDirFd, destBase, 0); err != nil {
			return err
		}
		return doLink()
	}
	return err
}

func copyFileContent(srcFd int, fileMetadata *fileMetadata, dirfd int, mode os.FileMode, useHardLinks bool) (*os.File, int64, error) {
	destFile := fileMetadata.Name
	src := procPathForFd(srcFd)
	st, err := os.Stat(src)
	if err != nil {
		return nil, -1, fmt.Errorf("copy file content for %q: %w", destFile, err)
	}

	copyWithFileRange, copyWithFileClone := true, true

	if useHardLinks {
		err := doHardLink(dirfd, srcFd, destFile)
		if err == nil {
			// if the file was deduplicated with a hard link, skip overriding file metadata.
			fileMetadata.skipSetAttrs = true
			return nil, st.Size(), nil
		}
	}

	// If the destination file already exists, we shouldn't blow it away
	dstFile, err := openFileUnderRoot(dirfd, destFile, newFileFlags, mode)
	if err != nil {
		return nil, -1, fmt.Errorf("open file %q under rootfs for copy: %w", destFile, err)
	}

	err = driversCopy.CopyRegularToFile(src, dstFile, st, &copyWithFileRange, &copyWithFileClone)
	if err != nil {
		dstFile.Close()
		return nil, -1, fmt.Errorf("copy to file %q under rootfs: %w", destFile, err)
	}
	return dstFile, st.Size(), nil
}

func timeToTimespec(time *time.Time) (ts unix.Timespec) {
	if time == nil || time.IsZero() {
		// Return UTIME_OMIT special value
		ts.Sec = 0
		ts.Nsec = ((1 << 30) - 2)
		return ts
	}
	return unix.NsecToTimespec(time.UnixNano())
}

// chown changes the owner and group of the file at the specified path under the directory
// pointed by dirfd.
// If nofollow is true, the function will not follow symlinks.
// If path is empty, the function will change the owner and group of the file descriptor.
// absolutePath is the absolute path of the file, used only for error messages.
func chown(dirfd int, path string, uid, gid int, nofollow bool, absolutePath string) error {
	var err error
	flags := 0
	if nofollow {
		flags |= unix.AT_SYMLINK_NOFOLLOW
	} else if path == "" {
		flags |= unix.AT_EMPTY_PATH
	}
	err = unix.Fchownat(dirfd, path, uid, gid, flags)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf(`potentially insufficient UIDs or GIDs available in the user namespace (requested %d:%d for %s): Check /etc/subuid and /etc/subgid if configured locally and run "podman system migrate": %w`, uid, gid, path, err)
	}
	return &fs.PathError{Op: "fchownat", Path: absolutePath, Err: err}
}

// setFileAttrs sets the file attributes for file given metadata
func setFileAttrs(dirfd int, file *os.File, mode os.FileMode, metadata *fileMetadata, options *archive.TarOptions, usePath bool) error {
	if metadata.skipSetAttrs {
		return nil
	}
	if file == nil {
		return errors.New("invalid file")
	}
	fd := int(file.Fd())

	t, err := typeToTarType(metadata.Type)
	if err != nil {
		return err
	}

	// If it is a symlink, force to use the path
	if t == tar.TypeSymlink {
		usePath = true
	}

	baseName := ""
	if usePath {
		dirName := filepath.Dir(metadata.Name)
		if dirName != "" {
			parentFd, err := openFileUnderRoot(dirfd, dirName, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			defer parentFd.Close()

			dirfd = int(parentFd.Fd())
		}
		baseName = filepath.Base(metadata.Name)
	}

	doChown := func() error {
		var err error
		if usePath {
			err = chown(dirfd, baseName, metadata.UID, metadata.GID, true, metadata.Name)
		} else {
			err = chown(fd, "", metadata.UID, metadata.GID, false, metadata.Name)
		}
		if options.IgnoreChownErrors {
			return nil
		}
		return err
	}

	doSetXattr := func(k string, v []byte) error {
		err := unix.Fsetxattr(fd, k, v, 0)
		if err != nil {
			return &fs.PathError{Op: "fsetxattr", Path: metadata.Name, Err: err}
		}
		return nil
	}

	doUtimes := func() error {
		ts := []unix.Timespec{timeToTimespec(metadata.AccessTime), timeToTimespec(metadata.ModTime)}
		var err error
		if usePath {
			err = unix.UtimesNanoAt(dirfd, baseName, ts, unix.AT_SYMLINK_NOFOLLOW)
		} else {
			err = unix.UtimesNanoAt(unix.AT_FDCWD, procPathForFd(fd), ts, 0)
		}
		if err != nil {
			return &fs.PathError{Op: "utimensat", Path: metadata.Name, Err: err}
		}
		return nil
	}

	doChmod := func() error {
		var err error
		op := ""
		if usePath {
			err = unix.Fchmodat(dirfd, baseName, uint32(mode), unix.AT_SYMLINK_NOFOLLOW)
			op = "fchmodat"
		} else {
			err = unix.Fchmod(fd, uint32(mode))
			op = "fchmod"
		}
		if err != nil {
			return &fs.PathError{Op: op, Path: metadata.Name, Err: err}
		}
		return nil
	}

	if err := doChown(); err != nil {
		return err
	}

	canIgnore := func(err error) bool {
		return err == nil || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOTSUP)
	}

	for k, v := range metadata.Xattrs {
		if _, found := xattrsToIgnore[k]; found {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return fmt.Errorf("decode xattr %q: %w", v, err)
		}
		if err := doSetXattr(k, data); !canIgnore(err) {
			return fmt.Errorf("set xattr %s=%q for %q: %w", k, data, metadata.Name, err)
		}
	}

	if err := doUtimes(); !canIgnore(err) {
		return err
	}

	if err := doChmod(); !canIgnore(err) {
		return err
	}
	return nil
}

func openFileUnderRootFallback(dirfd int, name string, flags uint64, mode os.FileMode) (int, error) {
	root := procPathForFd(dirfd)

	targetRoot, err := os.Readlink(root)
	if err != nil {
		return -1, err
	}

	hasNoFollow := (flags & unix.O_NOFOLLOW) != 0

	var fd int
	// If O_NOFOLLOW is specified in the flags, then resolve only the parent directory and use the
	// last component as the path to openat().
	if hasNoFollow {
		dirName, baseName, err := splitPath(name)
		if err != nil {
			return -1, err
		}
		if dirName != "/" {
			newRoot, err := securejoin.SecureJoin(root, dirName)
			if err != nil {
				return -1, err
			}
			root = newRoot
		}

		parentDirfd, err := unix.Open(root, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return -1, &fs.PathError{Op: "open", Path: root, Err: err}
		}
		defer unix.Close(parentDirfd)

		fd, err = unix.Openat(parentDirfd, baseName, int(flags), uint32(mode))
		if err != nil {
			return -1, &fs.PathError{Op: "openat", Path: name, Err: err}
		}
	} else {
		newPath, err := securejoin.SecureJoin(root, name)
		if err != nil {
			return -1, err
		}
		fd, err = unix.Openat(dirfd, newPath, int(flags), uint32(mode))
		if err != nil {
			return -1, &fs.PathError{Op: "openat", Path: newPath, Err: err}
		}
	}

	target, err := os.Readlink(procPathForFd(fd))
	if err != nil {
		unix.Close(fd)
		return -1, err
	}

	// Add an additional check to make sure the opened fd is inside the rootfs
	if !strings.HasPrefix(target, targetRoot) {
		unix.Close(fd)
		return -1, fmt.Errorf("while resolving %q.  It resolves outside the root directory", name)
	}

	return fd, err
}

func openFileUnderRootOpenat2(dirfd int, name string, flags uint64, mode os.FileMode) (int, error) {
	how := unix.OpenHow{
		Flags:   flags,
		Mode:    uint64(mode & 0o7777),
		Resolve: unix.RESOLVE_IN_ROOT,
	}
	fd, err := unix.Openat2(dirfd, name, &how)
	if err != nil {
		return -1, &fs.PathError{Op: "openat2", Path: name, Err: err}
	}
	return fd, nil
}

// skipOpenat2 is set when openat2 is not supported by the underlying kernel and avoid
// using it again.
var skipOpenat2 int32

// openFileUnderRootRaw tries to open a file using openat2 and if it is not supported fallbacks to a
// userspace lookup.
func openFileUnderRootRaw(dirfd int, name string, flags uint64, mode os.FileMode) (int, error) {
	var fd int
	var err error
	if name == "" {
		fd, err := unix.Dup(dirfd)
		if err != nil {
			return -1, fmt.Errorf("failed to duplicate file descriptor %d: %w", dirfd, err)
		}
		return fd, nil
	}
	if atomic.LoadInt32(&skipOpenat2) > 0 {
		fd, err = openFileUnderRootFallback(dirfd, name, flags, mode)
	} else {
		fd, err = openFileUnderRootOpenat2(dirfd, name, flags, mode)
		// If the function failed with ENOSYS, switch off the support for openat2
		// and fallback to using safejoin.
		if err != nil && errors.Is(err, unix.ENOSYS) {
			atomic.StoreInt32(&skipOpenat2, 1)
			fd, err = openFileUnderRootFallback(dirfd, name, flags, mode)
		}
	}
	return fd, err
}

// openFileUnderRoot safely opens a file under the specified root directory using openat2
// dirfd is an open file descriptor to the target checkout directory.
// name is the path to open relative to dirfd.
// flags are the flags to pass to the open syscall.
// mode specifies the mode to use for newly created files.
func openFileUnderRoot(dirfd int, name string, flags uint64, mode os.FileMode) (*os.File, error) {
	fd, err := openFileUnderRootRaw(dirfd, name, flags, mode)
	if err == nil {
		return os.NewFile(uintptr(fd), name), nil
	}

	hasCreate := (flags & unix.O_CREAT) != 0
	if errors.Is(err, unix.ENOENT) && hasCreate {
		parent := filepath.Dir(name)
		if parent != "" {
			newDirfd, err2 := openOrCreateDirUnderRoot(dirfd, parent, 0)
			if err2 == nil {
				defer newDirfd.Close()
				fd, err := openFileUnderRootRaw(int(newDirfd.Fd()), filepath.Base(name), flags, mode)
				if err == nil {
					return os.NewFile(uintptr(fd), name), nil
				}
			}
		}
	}
	return nil, fmt.Errorf("open %q under the rootfs: %w", name, err)
}

// openOrCreateDirUnderRoot safely opens a directory or create it if it is missing.
// dirfd is an open file descriptor to the target checkout directory.
// name is the path to open relative to dirfd.
// mode specifies the mode to use for newly created files.
func openOrCreateDirUnderRoot(dirfd int, name string, mode os.FileMode) (*os.File, error) {
	fd, err := openFileUnderRootRaw(dirfd, name, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err == nil {
		return os.NewFile(uintptr(fd), name), nil
	}

	if errors.Is(err, unix.ENOENT) {
		parent := filepath.Dir(name)
		// do not create the root directory, it should always exist
		if parent != name {
			pDir, err2 := openOrCreateDirUnderRoot(dirfd, parent, mode)
			if err2 != nil {
				return nil, err
			}
			defer pDir.Close()

			baseName := filepath.Base(name)

			if err2 := unix.Mkdirat(int(pDir.Fd()), baseName, uint32(mode)); err2 != nil {
				return nil, &fs.PathError{Op: "mkdirat", Path: name, Err: err2}
			}

			fd, err = openFileUnderRootRaw(int(pDir.Fd()), baseName, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
			if err == nil {
				return os.NewFile(uintptr(fd), name), nil
			}
		}
	}
	return nil, err
}

// appendHole creates a hole with the specified size at the open fd.
// fd is the open file descriptor.
// name is the path to use for error messages.
// size is the size of the hole to create.
func appendHole(fd int, name string, size int64) error {
	off, err := unix.Seek(fd, size, unix.SEEK_CUR)
	if err != nil {
		return &fs.PathError{Op: "seek", Path: name, Err: err}
	}
	// Make sure the file size is changed.  It might be the last hole and no other data written afterwards.
	if err := unix.Ftruncate(fd, off); err != nil {
		return &fs.PathError{Op: "ftruncate", Path: name, Err: err}
	}
	return nil
}

func safeMkdir(dirfd int, mode os.FileMode, name string, metadata *fileMetadata, options *archive.TarOptions) error {
	parent, base, err := splitPath(name)
	if err != nil {
		return err
	}
	parentFd := dirfd
	if parent != "/" {
		parentFile, err := openOrCreateDirUnderRoot(dirfd, parent, 0)
		if err != nil {
			return err
		}
		defer parentFile.Close()
		parentFd = int(parentFile.Fd())
	}

	if err := unix.Mkdirat(parentFd, base, uint32(mode)); err != nil {
		if !os.IsExist(err) {
			return &fs.PathError{Op: "mkdirat", Path: name, Err: err}
		}
	}

	file, err := openFileUnderRoot(parentFd, base, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	return setFileAttrs(dirfd, file, mode, metadata, options, false)
}

func safeLink(dirfd int, mode os.FileMode, metadata *fileMetadata, options *archive.TarOptions) error {
	sourceFile, err := openFileUnderRoot(dirfd, metadata.Linkname, unix.O_PATH|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	err = doHardLink(dirfd, int(sourceFile.Fd()), metadata.Name)
	if err != nil {
		return err
	}

	newFile, err := openFileUnderRoot(dirfd, metadata.Name, unix.O_WRONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		// If the target is a symlink, open the file with O_PATH.
		if errors.Is(err, unix.ELOOP) {
			newFile, err := openFileUnderRoot(dirfd, metadata.Name, unix.O_PATH|unix.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
			defer newFile.Close()

			return setFileAttrs(dirfd, newFile, mode, metadata, options, true)
		}
		return err
	}
	defer newFile.Close()

	return setFileAttrs(dirfd, newFile, mode, metadata, options, false)
}

func safeSymlink(dirfd int, metadata *fileMetadata) error {
	destDir, destBase, err := splitPath(metadata.Name)
	if err != nil {
		return err
	}
	destDirFd := dirfd
	if destDir != "/" {
		f, err := openOrCreateDirUnderRoot(dirfd, destDir, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		destDirFd = int(f.Fd())
	}

	if err := unix.Symlinkat(metadata.Linkname, destDirFd, destBase); err != nil {
		return &fs.PathError{Op: "symlinkat", Path: metadata.Name, Err: err}
	}
	return nil
}

type whiteoutHandler struct {
	Dirfd int
	Root  string
}

func (d whiteoutHandler) Setxattr(path, name string, value []byte) error {
	file, err := openOrCreateDirUnderRoot(d.Dirfd, path, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := unix.Fsetxattr(int(file.Fd()), name, value, 0); err != nil {
		return &fs.PathError{Op: "fsetxattr", Path: path, Err: err}
	}
	return nil
}

func (d whiteoutHandler) Mknod(path string, mode uint32, dev int) error {
	dir, base, err := splitPath(path)
	if err != nil {
		return err
	}
	dirfd := d.Dirfd
	if dir != "/" {
		dir, err := openOrCreateDirUnderRoot(d.Dirfd, dir, 0)
		if err != nil {
			return err
		}
		defer dir.Close()

		dirfd = int(dir.Fd())
	}

	if err := unix.Mknodat(dirfd, base, mode, dev); err != nil {
		return &fs.PathError{Op: "mknodat", Path: path, Err: err}
	}

	return nil
}

func (d whiteoutHandler) Chown(path string, uid, gid int) error {
	file, err := openFileUnderRoot(d.Dirfd, path, unix.O_PATH, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	return chown(int(file.Fd()), "", uid, gid, false, path)
}

type readerAtCloser interface {
	io.ReaderAt
	io.Closer
}

// seekableFile is a struct that wraps an *os.File to provide an ImageSourceSeekable.
type seekableFile struct {
	reader readerAtCloser
}

func (f *seekableFile) Close() error {
	return f.reader.Close()
}

func (f *seekableFile) GetBlobAt(chunks []ImageSourceChunk) (chan io.ReadCloser, chan error, error) {
	streams := make(chan io.ReadCloser)
	errs := make(chan error)

	go func() {
		for _, chunk := range chunks {
			streams <- io.NopCloser(io.NewSectionReader(f.reader, int64(chunk.Offset), int64(chunk.Length)))
		}
		close(streams)
		close(errs)
	}()

	return streams, errs, nil
}

func newSeekableFile(reader readerAtCloser) *seekableFile {
	return &seekableFile{reader: reader}
}
