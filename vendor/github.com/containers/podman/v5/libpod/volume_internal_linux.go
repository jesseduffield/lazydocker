//go:build !remote

package libpod

import (
	"golang.org/x/sys/unix"
)

func detachUnmount(mountPoint string) error {
	return unix.Unmount(mountPoint, unix.MNT_DETACH)
}
