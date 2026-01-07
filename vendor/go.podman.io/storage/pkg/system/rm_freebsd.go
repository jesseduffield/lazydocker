package system

import (
	"io/fs"
	"path/filepath"
)

// Reset file flags in a directory tree. This allows EnsureRemoveAll
// to delete trees which have the immutable flag set.
func resetFileFlags(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err := Lchflags(path, 0); err != nil {
			return err
		}
		return nil
	})
}
