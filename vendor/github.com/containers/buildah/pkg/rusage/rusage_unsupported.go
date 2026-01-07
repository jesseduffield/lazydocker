//go:build windows

package rusage

import (
	"fmt"
	"syscall"
)

func get() (Rusage, error) {
	return Rusage{}, fmt.Errorf("getting resource usage: %w", syscall.ENOTSUP)
}

// Supported returns true if resource usage counters are supported on this OS.
func Supported() bool {
	return false
}
