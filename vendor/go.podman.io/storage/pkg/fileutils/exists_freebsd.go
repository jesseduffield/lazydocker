package fileutils

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Exists checks whether a file or directory exists at the given path.
// If the path is a symlink, the symlink is followed.
func Exists(path string) error {
	// It uses unix.Faccessat which is a faster operation compared to os.Stat for
	// simply checking the existence of a file.
	err := unix.Faccessat(unix.AT_FDCWD, path, unix.F_OK, 0)
	if err != nil {
		return &os.PathError{Op: "faccessat", Path: path, Err: err}
	}
	return nil
}

// Lexists checks whether a file or directory exists at the given path.
// If the path is a symlink, the symlink itself is checked.
func Lexists(path string) error {
	// FreeBSD before 15.0 does not support the AT_SYMLINK_NOFOLLOW flag for
	// faccessat. In this case, the call to faccessat will return EINVAL and
	// we fall back to using Lstat.
	err := unix.Faccessat(unix.AT_FDCWD, path, unix.F_OK, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		if errors.Is(err, syscall.EINVAL) {
			_, err = os.Lstat(path)
			return err
		}
		return &os.PathError{Op: "faccessat", Path: path, Err: err}
	}
	return nil
}
