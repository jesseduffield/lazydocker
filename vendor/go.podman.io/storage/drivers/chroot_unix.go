//go:build !windows

package graphdriver

import (
	"fmt"
	"os"
	"syscall"
)

// chrootOrChdir() is either a chdir() to the specified path, or a chroot() to the
// specified path followed by chdir() to the new root directory
func chrootOrChdir(path string) error {
	if err := syscall.Chroot(path); err != nil {
		return fmt.Errorf("chrooting to %q: %w", path, err)
	}
	if err := syscall.Chdir(string(os.PathSeparator)); err != nil {
		return fmt.Errorf("changing to %q: %w", path, err)
	}
	return nil
}
