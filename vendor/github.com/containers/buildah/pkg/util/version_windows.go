package util

import (
	"errors"
)

func ReadKernelVersion() (string, error) {
	return "", errors.New("readKernelVersion not supported on windows")
}
