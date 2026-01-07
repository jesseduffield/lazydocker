package system

import (
	"os"
	"syscall"
)

func Lchown(name string, uid, gid int) error {
	err := syscall.Lchown(name, uid, gid)

	for err == syscall.EINTR {
		err = syscall.Lchown(name, uid, gid)
	}

	if err != nil {
		return &os.PathError{Op: "lchown", Path: name, Err: err}
	}

	return nil
}
