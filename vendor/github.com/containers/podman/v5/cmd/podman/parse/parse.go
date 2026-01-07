//go:build !windows

package parse

import (
	"fmt"
	"strings"
)

// ValidateFileName returns an error if filename contains ":"
// as it is currently not supported
func ValidateFileName(filename string) error {
	if strings.Contains(filename, ":") {
		return fmt.Errorf("invalid filename (should not contain ':') %q", filename)
	}
	return nil
}
