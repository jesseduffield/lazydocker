//go:build !linux

package fsverity

import (
	"fmt"
)

// EnableVerity enables the verity feature on a file represented by the file descriptor 'fd'.  The file must be opened
// in read-only mode.
// The 'description' parameter is a human-readable description of the file.
func EnableVerity(description string, fd int) error {
	return fmt.Errorf("fs-verity is not supported on this platform")
}

// MeasureVerity measures and returns the verity digest for the file represented by 'fd'.
// The 'description' parameter is a human-readable description of the file.
func MeasureVerity(description string, fd int) (string, error) {
	return "", fmt.Errorf("fs-verity is not supported on this platform")
}
