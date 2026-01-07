//go:build !windows && !linux && !darwin

package chrootarchive

import "golang.org/x/sys/unix"

func realChroot(path string) error {
	if err := unix.Chroot(path); err != nil {
		return err
	}
	return unix.Chdir("/")
}

func chroot(path string) error {
	return realChroot(path)
}
