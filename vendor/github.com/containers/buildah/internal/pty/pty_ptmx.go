//go:build linux

package pty

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// GetPtyDescriptors allocates a new pseudoterminal and returns the control and
// pseudoterminal file descriptors.  This implementation uses the /dev/ptmx
// device. The main advantage of using this instead of posix_openpt is that it
// avoids cgo.
func GetPtyDescriptors() (int, int, error) {
	// Create a pseudo-terminal -- open a copy of the master side.
	controlFd, err := unix.Open("/dev/ptmx", os.O_RDWR, 0o600)
	if err != nil {
		return -1, -1, fmt.Errorf("opening PTY master using /dev/ptmx: %v", err)
	}
	// Set the kernel's lock to "unlocked".
	locked := 0
	if result, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(controlFd), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&locked))); int(result) == -1 {
		return -1, -1, fmt.Errorf("unlocking PTY descriptor: %v", err)
	}
	// Get a handle for the other end.
	ptyFd, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(controlFd), unix.TIOCGPTPEER, unix.O_RDWR|unix.O_NOCTTY)
	if int(ptyFd) == -1 {
		if errno, isErrno := err.(syscall.Errno); !isErrno || (errno != syscall.EINVAL && errno != syscall.ENOTTY) {
			return -1, -1, fmt.Errorf("getting PTY descriptor: %v", err)
		}
		// EINVAL means the kernel's too old to understand TIOCGPTPEER.  Try TIOCGPTN.
		ptyN, err := unix.IoctlGetInt(controlFd, unix.TIOCGPTN)
		if err != nil {
			return -1, -1, fmt.Errorf("getting PTY number: %v", err)
		}
		ptyName := fmt.Sprintf("/dev/pts/%d", ptyN)
		fd, err := unix.Open(ptyName, unix.O_RDWR|unix.O_NOCTTY, 0o620)
		if err != nil {
			return -1, -1, fmt.Errorf("opening PTY %q: %v", ptyName, err)
		}
		ptyFd = uintptr(fd)
	}
	return controlFd, int(ptyFd), nil
}
