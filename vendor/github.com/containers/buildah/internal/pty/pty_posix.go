//go:build freebsd && cgo

package pty

// #include <fcntl.h>
// #include <stdlib.h>
import "C"

import (
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func openpt() (int, error) {
	fd, err := C.posix_openpt(C.O_RDWR)
	if err != nil {
		return -1, err
	}
	if _, err := C.grantpt(fd); err != nil {
		return -1, err
	}
	return int(fd), nil
}

func ptsname(fd int) (string, error) {
	path, err := C.ptsname(C.int(fd))
	if err != nil {
		return "", err
	}
	return C.GoString(path), nil
}

func unlockpt(fd int) error {
	if _, err := C.unlockpt(C.int(fd)); err != nil {
		return err
	}
	return nil
}

// GetPtyDescriptors allocates a new pseudoterminal and returns the control and
// pseudoterminal file descriptors.
func GetPtyDescriptors() (int, int, error) {
	// Create a pseudo-terminal and open the control side
	controlFd, err := openpt()
	if err != nil {
		logrus.Errorf("error opening PTY control side using posix_openpt: %v", err)
		return -1, -1, err
	}
	if err = unlockpt(controlFd); err != nil {
		logrus.Errorf("error unlocking PTY: %v", err)
		return -1, -1, err
	}
	// Get a handle for the other end.
	ptyName, err := ptsname(controlFd)
	if err != nil {
		logrus.Errorf("error getting PTY name: %v", err)
		return -1, -1, err
	}
	ptyFd, err := unix.Open(ptyName, unix.O_RDWR, 0)
	if err != nil {
		logrus.Errorf("error opening PTY: %v", err)
		return -1, -1, err
	}
	return controlFd, ptyFd, nil
}
