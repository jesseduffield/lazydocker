package fileutils

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// ReflinkOrCopy attempts to reflink the source to the destination fd.
// If reflinking fails or is unsupported, it falls back to io.Copy().
func ReflinkOrCopy(src, dst *os.File) error {
	err := unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
	if err == nil {
		return nil
	}

	_, err = io.Copy(dst, src)
	return err
}
