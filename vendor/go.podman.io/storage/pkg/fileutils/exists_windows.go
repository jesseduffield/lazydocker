package fileutils

import (
	"os"
)

// Exists checks whether a file or directory exists at the given path.
func Exists(path string) error {
	_, err := os.Stat(path)
	return err
}

// Lexists checks whether a file or directory exists at the given path, without
// resolving symlinks
func Lexists(path string) error {
	_, err := os.Lstat(path)
	return err
}
