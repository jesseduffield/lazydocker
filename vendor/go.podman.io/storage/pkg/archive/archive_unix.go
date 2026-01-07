//go:build !windows

package archive

import (
	"archive/tar"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/system"
	"golang.org/x/sys/unix"
)

func init() {
	sysStatOverride = statUnix
}

// statUnix populates hdr from system-dependent fields of fi without performing
// any OS lookups.
// Adapted from Moby.
func statUnix(fi os.FileInfo, hdr *tar.Header) error {
	s, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	hdr.Uid = int(s.Uid)
	hdr.Gid = int(s.Gid)

	if s.Mode&unix.S_IFBLK != 0 ||
		s.Mode&unix.S_IFCHR != 0 {
		// _nolint_: Whether this conversion is required is hardware- and OS-dependent: the value might be uint64 on Linux, int32 on macOS.
		// So, this might trigger either "uncovert" (if the conversion is unnecessary) or "nolintlint" (if it is required)
		hdr.Devmajor = int64(unix.Major(uint64(s.Rdev))) //nolint:unconvert,nolintlint
		hdr.Devminor = int64(unix.Minor(uint64(s.Rdev))) //nolint:unconvert,nolintlint
	}

	return nil
}

// fixVolumePathPrefix does platform specific processing to ensure that if
// the path being passed in is not in a volume path format, convert it to one.
func fixVolumePathPrefix(srcPath string) string {
	return srcPath
}

// getWalkRoot calculates the root path when performing a TarWithOptions.
// We use a separate function as this is platform specific. On Linux, we
// can't use filepath.Join(srcPath,include) because this will clean away
// a trailing "." or "/" which may be important.
func getWalkRoot(srcPath string, include string) string {
	return srcPath + string(filepath.Separator) + include
}

// CanonicalTarNameForPath returns platform-specific filepath
// to canonical posix-style path for tar archival. p is relative
// path.
func CanonicalTarNameForPath(p string) (string, error) {
	return p, nil // already unix-style
}

// chmodTarEntry is used to adjust the file permissions used in tar header based
// on the platform the archival is done.

func chmodTarEntry(perm os.FileMode) os.FileMode {
	return perm // noop for unix as golang APIs provide perm bits correctly
}

func setHeaderForSpecialDevice(hdr *tar.Header, name string, stat any) {
	s, ok := stat.(*syscall.Stat_t)

	if ok {
		// Currently go does not fill in the major/minors
		if s.Mode&unix.S_IFBLK != 0 ||
			s.Mode&unix.S_IFCHR != 0 {
			// _nolint_: Whether this conversion is required is hardware- and OS-dependent: the value might be uint64 on Linux, int32 on macOS.
			// So, this might trigger either "uncovert" (if the conversion is unnecessary) or "nolintlint" (if it is required)
			hdr.Devmajor = int64(major(uint64(s.Rdev))) //nolint: unconvert,nolintlint
			hdr.Devminor = int64(minor(uint64(s.Rdev))) //nolint: unconvert,nolintlint
		}
	}
}

func getInodeFromStat(stat any) (inode uint64) {
	s, ok := stat.(*syscall.Stat_t)

	if ok {
		inode = s.Ino
	}

	return inode
}

func getFileUIDGID(stat any) (idtools.IDPair, error) {
	s, ok := stat.(*syscall.Stat_t)

	if !ok {
		return idtools.IDPair{}, errors.New("cannot convert stat value to syscall.Stat_t")
	}
	return idtools.IDPair{UID: int(s.Uid), GID: int(s.Gid)}, nil
}

func major(device uint64) uint64 {
	return (device >> 8) & 0xfff
}

func minor(device uint64) uint64 {
	return (device & 0xff) | ((device >> 12) & 0xfff00)
}

// handleTarTypeBlockCharFifo is an OS-specific helper function used by
// createTarFile to handle the following types of header: Block; Char; Fifo
func handleTarTypeBlockCharFifo(hdr *tar.Header, path string) error {
	mode := uint32(hdr.Mode & 0o7777)
	switch hdr.Typeflag {
	case tar.TypeBlock:
		mode |= unix.S_IFBLK
	case tar.TypeChar:
		mode |= unix.S_IFCHR
	case tar.TypeFifo:
		mode |= unix.S_IFIFO
	}

	return system.Mknod(path, mode, system.Mkdev(hdr.Devmajor, hdr.Devminor))
}

// Hardlink without symlinks
func handleLLink(targetPath, path string) error {
	// Note: on Linux, the link syscall will not follow symlinks.
	// This behavior is implementation-dependent since
	// POSIX.1-2008 so to make it clear that we need non-symlink
	// following here we use the linkat syscall which has a flags
	// field to select symlink following or not.
	return unix.Linkat(unix.AT_FDCWD, targetPath, unix.AT_FDCWD, path, 0)
}
