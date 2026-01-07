//go:build netbsd || freebsd || darwin

package archive

import (
	"archive/tar"
	"os"

	"golang.org/x/sys/unix"
)

func handleLChmod(hdr *tar.Header, path string, hdrInfo os.FileInfo, forceMask *os.FileMode) error {
	permissionsMask := hdrInfo.Mode()
	if forceMask != nil {
		permissionsMask = *forceMask
	}
	return unix.Fchmodat(unix.AT_FDCWD, path, uint32(permissionsMask), unix.AT_SYMLINK_NOFOLLOW)
}
