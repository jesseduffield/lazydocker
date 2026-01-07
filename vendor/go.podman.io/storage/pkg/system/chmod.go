package system

import (
	"errors"
	"os"
	"syscall"
)

func Chmod(name string, mode os.FileMode) error {
	err := os.Chmod(name, mode)

	for err != nil && errors.Is(err, syscall.EINTR) {
		err = os.Chmod(name, mode)
	}

	return err
}
