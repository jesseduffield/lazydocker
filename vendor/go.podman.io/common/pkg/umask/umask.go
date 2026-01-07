package umask

import (
	"fmt"
	"os"
	"path/filepath"

	"go.podman.io/storage/pkg/fileutils"
)

// MkdirAllIgnoreUmask creates a directory by ignoring the currently set umask.
func MkdirAllIgnoreUmask(dir string, mode os.FileMode) error {
	parent := dir
	dirs := []string{}

	// Find all parent directories which would have been created by MkdirAll
	for {
		if err := fileutils.Exists(parent); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("cannot stat %s: %w", dir, err)
		}

		dirs = append(dirs, parent)
		newParent := filepath.Dir(parent)

		// Only possible if the root paths are not existing, which would be odd
		if parent == newParent {
			break
		}

		parent = newParent
	}

	if err := os.MkdirAll(dir, mode); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	for _, d := range dirs {
		if err := os.Chmod(d, mode); err != nil {
			return fmt.Errorf("chmod directory %s: %w", d, err)
		}
	}

	return nil
}

// WriteFileIgnoreUmask write the provided data to the path by ignoring the
// currently set umask.
func WriteFileIgnoreUmask(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod file: %w", err)
	}

	return nil
}
