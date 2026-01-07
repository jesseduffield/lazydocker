//go:build !remote && (linux || freebsd)

package libpod

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/buildah/copier"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/copy"
)

// statOnHost stats the specified path *on the host*.  It returns the file info
// along with the resolved root and the resolved path.  Both paths are absolute
// to the host's root.  Note that the paths may resolved outside the
// container's mount point (e.g., to a volume or bind mount).
func (c *Container) statOnHost(mountPoint string, containerPath string) (*copier.StatForItem, string, string, error) {
	// Now resolve the container's path.  It may hit a volume, it may hit a
	// bind mount, it may be relative.
	resolvedRoot, resolvedPath, _, err := c.resolvePath(mountPoint, containerPath)
	if err != nil {
		return nil, "", "", err
	}

	statInfo, err := secureStat(resolvedRoot, resolvedPath)
	return statInfo, resolvedRoot, resolvedPath, err
}

func (c *Container) stat(containerMountPoint string, containerPath string) (*define.FileInfo, string, string, error) {
	var (
		resolvedRoot     string
		resolvedPath     string
		absContainerPath string
		statInfo         *copier.StatForItem
		statErr          error
	)

	// Make sure that "/" copies the *contents* of the mount point and not
	// the directory.
	if containerPath == "/" {
		containerPath = "/."
	}

	// Wildcards are not allowed.
	// TODO: it's now technically possible wildcards.
	// We may consider enabling support in the future.
	if strings.Contains(containerPath, "*") {
		return nil, "", "", copy.ErrENOENT
	}

	statInfo, resolvedRoot, resolvedPath, statErr = c.statInContainer(containerMountPoint, containerPath)
	if statErr != nil {
		if statInfo == nil {
			return nil, "", "", statErr
		}
		// Not all errors from secureStat map to ErrNotExist, so we
		// have to look into the error string.  Turning it into an
		// ENOENT lets the API handlers return the correct status code
		// which is crucial for the remote client.
		if os.IsNotExist(statErr) || strings.Contains(statErr.Error(), "o such file or directory") {
			statErr = copy.ErrENOENT
		}
	}

	switch {
	case statInfo.IsSymlink:
		// Symlinks are already evaluated and always relative to the
		// container's mount point.
		absContainerPath = statInfo.ImmediateTarget
	case strings.HasPrefix(resolvedPath, containerMountPoint):
		// If the path is on the container's mount point, strip it off.
		absContainerPath = strings.TrimPrefix(resolvedPath, containerMountPoint)
		absContainerPath = filepath.Join("/", absContainerPath)
	default:
		// No symlink and not on the container's mount point, so let's
		// move it back to the original input.  It must have evaluated
		// to a volume or bind mount but we cannot return host paths.
		absContainerPath = containerPath
	}

	// Preserve the base path as specified by the user.  The `filepath`
	// packages likes to remove trailing slashes and dots that are crucial
	// to the copy logic.
	absContainerPath = copy.PreserveBasePath(containerPath, absContainerPath)
	resolvedPath = copy.PreserveBasePath(containerPath, resolvedPath)

	info := &define.FileInfo{
		IsDir:      statInfo.IsDir,
		Name:       filepath.Base(absContainerPath),
		Size:       statInfo.Size,
		Mode:       statInfo.Mode,
		ModTime:    statInfo.ModTime,
		LinkTarget: absContainerPath,
	}

	return info, resolvedRoot, resolvedPath, statErr
}

// secureStat extracts file info for path in a chroot'ed environment in root.
func secureStat(root string, path string) (*copier.StatForItem, error) {
	var glob string
	var err error

	// If root and path are equal, then dir must be empty and the glob must
	// be ".".
	if filepath.Clean(root) == filepath.Clean(path) {
		glob = "."
	} else {
		glob, err = filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
	}

	globStats, err := copier.Stat(root, "", copier.StatOptions{}, []string{glob})
	if err != nil {
		return nil, err
	}

	if len(globStats) != 1 {
		return nil, fmt.Errorf("internal error: secureStat: expected 1 item but got %d", len(globStats))
	}
	if len(globStats) != 1 {
		return nil, fmt.Errorf("internal error: secureStat: expected 1 result but got %d", len(globStats[0].Results))
	}

	// NOTE: the key in the map differ from `glob` when hitting symlink.
	// Hence, we just take the first (and only) key/value pair.
	for _, stat := range globStats[0].Results {
		var statErr error
		if stat.Error != "" {
			statErr = errors.New(stat.Error)
		}
		// If necessary evaluate the symlink
		if stat.IsSymlink {
			target, err := copier.Eval(root, path, copier.EvalOptions{})
			if err != nil {
				return nil, fmt.Errorf("evaluating symlink in container: %w", err)
			}
			// Need to make sure the symlink is relative to the root!
			target = strings.TrimPrefix(target, root)
			target = filepath.Join("/", target)
			stat.ImmediateTarget = target
		}
		return stat, statErr
	}

	// Nothing found!
	return nil, copy.ErrENOENT
}
