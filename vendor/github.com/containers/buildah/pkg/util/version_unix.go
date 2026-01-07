//go:build !windows

package util

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func ReadKernelVersion() (string, error) {
	var uname unix.Utsname
	if err := unix.Uname(&uname); err != nil {
		return "", err
	}
	n := bytes.IndexByte(uname.Release[:], 0)
	return string(uname.Release[:n]), nil
}
