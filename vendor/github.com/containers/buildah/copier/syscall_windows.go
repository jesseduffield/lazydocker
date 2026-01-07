//go:build windows

package copier

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var canChroot = false

func chroot(path string) (bool, error) {
	return false, nil
}

func chrMode(mode os.FileMode) uint32 {
	return windows.S_IFCHR | uint32(mode)
}

func blkMode(mode os.FileMode) uint32 {
	return windows.S_IFBLK | uint32(mode)
}

func mkdev(major, minor uint32) uint64 {
	return 0
}

func mkfifo(path string, mode uint32) error {
	return syscall.ENOSYS
}

func mknod(path string, mode uint32, dev int) error {
	return syscall.ENOSYS
}

func chmod(path string, mode os.FileMode) error {
	err := os.Chmod(path, mode)
	if err != nil && errors.Is(err, syscall.EWINDOWS) {
		return nil
	}
	return err
}

func chown(path string, uid, gid int) error {
	err := os.Chown(path, uid, gid)
	if err != nil && errors.Is(err, syscall.EWINDOWS) {
		return nil
	}
	return err
}

func lchown(path string, uid, gid int) error {
	err := os.Lchown(path, uid, gid)
	if err != nil && errors.Is(err, syscall.EWINDOWS) {
		return nil
	}
	return err
}

func lutimes(isSymlink bool, path string, atime, mtime time.Time) error {
	if isSymlink {
		return nil
	}
	if atime.IsZero() || mtime.IsZero() {
		now := time.Now()
		if atime.IsZero() {
			atime = now
		}
		if mtime.IsZero() {
			mtime = now
		}
	}
	return windows.UtimesNano(path, []windows.Timespec{windows.NsecToTimespec(atime.UnixNano()), windows.NsecToTimespec(mtime.UnixNano())})
}

func owner(info os.FileInfo) (int, int, error) {
	return -1, -1, syscall.ENOSYS
}

// sameDevice returns true since we can't be sure that they're not on the same device
func sameDevice(a, b os.FileInfo) bool {
	return true
}
