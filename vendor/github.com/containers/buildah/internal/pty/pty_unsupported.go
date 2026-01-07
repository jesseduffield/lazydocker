//go:build !linux && !(freebsd && cgo)

package pty

import (
	"errors"
)

// GetPtyDescriptors would allocate a new pseudoterminal and return the control and
// pseudoterminal file descriptors, if only it could.
func GetPtyDescriptors() (int, int, error) {
	return -1, -1, errors.New("GetPtyDescriptors not supported on this platform")
}
