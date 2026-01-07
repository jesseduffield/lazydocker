//go:build !windows && !freebsd

package fileutils

import (
	"os"

	"golang.org/x/sys/unix"
)

// Exists checks whether a file or directory exists at the given path.
// If the path is a symlink, the symlink is followed.
func Exists(path string) error {
	// It uses unix.Faccessat which is a faster operation compared to os.Stat for
	// simply checking the existence of a file.
	err := unix.Faccessat(unix.AT_FDCWD, path, unix.F_OK, unix.AT_EACCESS)
	if err != nil {
		return &os.PathError{Op: "faccessat", Path: path, Err: err}
	}
	return nil
}

// Lexists checks whether a file or directory exists at the given path.
// If the path is a symlink, the symlink itself is checked.
func Lexists(path string) error {
	// It uses unix.Faccessat which is a faster operation compared to os.Stat for
	// simply checking the existence of a file.
	err := unix.Faccessat(unix.AT_FDCWD, path, unix.F_OK, unix.AT_SYMLINK_NOFOLLOW|unix.AT_EACCESS)
	if err != nil {
		return &os.PathError{Op: "faccessat", Path: path, Err: err}
	}
	return nil
}
