//go:build !linux

package fileutils

import (
	"io"
	"os"
)

// ReflinkOrCopy attempts to reflink the source to the destination fd.
// If reflinking fails or is unsupported, it falls back to io.Copy().
func ReflinkOrCopy(src, dst *os.File) error {
	_, err := io.Copy(dst, src)
	return err
}
