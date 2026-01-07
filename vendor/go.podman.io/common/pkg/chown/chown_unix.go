//go:build !windows

package chown

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ChangeHostPathOwnership changes the uid and gid ownership of a directory or file within the host.
// This is used by the volume U flag to change source volumes ownership.
func ChangeHostPathOwnership(path string, recursive bool, uid, gid int) error {
	// Validate if host path can be chowned
	isDangerous, err := DangerousHostPath(path)
	if err != nil {
		return fmt.Errorf("failed to validate if host path is dangerous: %w", err)
	}

	if isDangerous {
		return fmt.Errorf("chowning host path %q is not allowed. You can manually `chown -R %d:%d %s`", path, uid, gid, path)
	}

	// Chown host path
	if recursive {
		err := filepath.Walk(path, func(filePath string, f os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			//nolint:errcheck
			stat := f.Sys().(*syscall.Stat_t)
			// Get current ownership
			currentUID := int(stat.Uid)
			currentGID := int(stat.Gid)

			if uid != currentUID || gid != currentGID {
				return os.Lchown(filePath, uid, gid)
			}

			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to chown recursively host path: %w", err)
		}
	} else {
		// Get host path info
		f, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("failed to get host path information: %w", err)
		}

		//nolint:errcheck
		stat := f.Sys().(*syscall.Stat_t)
		// Get current ownership
		currentUID := int(stat.Uid)
		currentGID := int(stat.Gid)

		if uid != currentUID || gid != currentGID {
			if err := os.Lchown(path, uid, gid); err != nil {
				return fmt.Errorf("failed to chown host path: %w", err)
			}
		}
	}

	return nil
}
