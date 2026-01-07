//go:build linux

package overlay

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/system"
)

func scanForMountProgramIndicators(home string) (detected bool, err error) {
	err = filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if detected {
			return fs.SkipDir
		}
		if err != nil {
			return err
		}
		basename := filepath.Base(path)
		if strings.HasPrefix(basename, archive.WhiteoutPrefix) {
			detected = true
			return fs.SkipDir
		}
		if d.IsDir() {
			xattrs, err := system.Llistxattr(path)
			if err != nil && !errors.Is(err, system.ENOTSUP) {
				return err
			}
			for _, xattr := range xattrs {
				if strings.HasPrefix(xattr, "user.fuseoverlayfs.") || strings.HasPrefix(xattr, "user.containers.") {
					detected = true
					return fs.SkipDir
				}
			}
		}
		return nil
	})
	return detected, err
}
