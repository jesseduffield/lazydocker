//go:build !windows

package mount

import (
	"time"

	"golang.org/x/sys/unix"
)

func unmount(target string, flags int) error {
	var err error
	for range 50 {
		err = unix.Unmount(target, flags)
		switch err {
		case unix.EBUSY:
			time.Sleep(50 * time.Millisecond)
			continue
		case unix.EINVAL, nil:
			// Ignore "not mounted" error here. Note the same error
			// can be returned if flags are invalid, so this code
			// assumes that the flags value is always correct.
			return nil
		}
		break
	}

	return &mountError{
		op:     "umount",
		target: target,
		flags:  uintptr(flags),
		err:    err,
	}
}
