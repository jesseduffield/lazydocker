//go:build !remote

package libpod

import (
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// No equivalent on FreeBSD?
func LabelVolumePath(_, _ string) error {
	return nil
}

// Unmount umounts a target directory
func Unmount(mount string) {
	if err := unix.Unmount(mount, unix.MNT_FORCE); err != nil {
		if err != syscall.EINVAL {
			logrus.Warnf("Failed to unmount %s : %v", mount, err)
		} else {
			logrus.Debugf("failed to unmount %s : %v", mount, err)
		}
	}
}
