//go:build !remote

package libpod

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// pathAbs returns an absolute path.  If the specified path is
// relative, it will be resolved relative to the container's working dir.
func (c *Container) pathAbs(path string) string {
	if !filepath.IsAbs(path) {
		// If the containerPath is not absolute, it's relative to the
		// container's working dir.  To be extra careful, let's first
		// join the working dir with "/", and the add the containerPath
		// to it.
		path = filepath.Join(filepath.Join("/", c.WorkingDir()), path)
	}
	return path
}

// resolvePath resolves the container's mount point and the container
// path as specified by the user.  Both may resolve to paths outside of the
// container's mount point when the container path hits a volume or bind mount.
//
// It returns a bool, indicating whether containerPath resolves outside of
// mountPoint (e.g., via a mount or volume), the resolved root (e.g., container
// mount, bind mount or volume) and the resolved path on the root (absolute to
// the host). If the path is on a named volume, the volume is returned.
func (c *Container) resolvePath(mountPoint string, containerPath string) (string, string, *Volume, error) {
	// Let's first make sure we have a path relative to the mount point.
	pathRelativeToContainerMountPoint := c.pathAbs(containerPath)
	resolvedPathOnTheContainerMountPoint := filepath.Join(mountPoint, pathRelativeToContainerMountPoint)
	pathRelativeToContainerMountPoint = strings.TrimPrefix(pathRelativeToContainerMountPoint, mountPoint)
	pathRelativeToContainerMountPoint = filepath.Join("/", pathRelativeToContainerMountPoint)

	// Now we have an "absolute container Path" but not yet resolved on the
	// host (e.g., "/foo/bar/file.txt").  As mentioned above, we need to
	// check if "/foo/bar/file.txt" is on a volume or bind mount.  To do
	// that, we need to walk *down* the paths to the root.  Assuming
	// volume-1 is mounted to "/foo" and volume-2 is mounted to "/foo/bar",
	// we must select "/foo/bar".  Once selected, we need to rebase the
	// remainder (i.e, "/file.txt") on the volume's mount point on the
	// host.  Same applies to bind mounts.

	searchPath := pathRelativeToContainerMountPoint
	for {
		volume, err := findVolume(c, searchPath)
		if err != nil {
			return "", "", nil, err
		}
		if volume != nil {
			logrus.Debugf("Container path %q resolved to volume %q on path %q", containerPath, volume.Name(), searchPath)

			mountPoint, err := volume.MountPoint()
			if err != nil {
				return "", "", nil, err
			}
			if mountPoint == "" {
				return "", "", nil, fmt.Errorf("volume %s is not mounted, cannot copy into it", volume.Name())
			}

			// We found a matching volume for searchPath.  We now
			// need to first find the relative path of our input
			// path to the searchPath, and then join it with the
			// volume's mount point.
			pathRelativeToVolume := strings.TrimPrefix(pathRelativeToContainerMountPoint, searchPath)
			absolutePathOnTheVolumeMount, err := securejoin.SecureJoin(mountPoint, pathRelativeToVolume)
			if err != nil {
				return "", "", nil, err
			}
			return mountPoint, absolutePathOnTheVolumeMount, volume, nil
		}

		if mount := findBindMount(c, searchPath); mount != nil {
			logrus.Debugf("Container path %q resolved to bind mount %q:%q on path %q", containerPath, mount.Source, mount.Destination, searchPath)
			// We found a matching bind mount for searchPath.  We
			// now need to first find the relative path of our
			// input path to the searchPath, and then join it with
			// the source of the bind mount.
			pathRelativeToBindMount := strings.TrimPrefix(pathRelativeToContainerMountPoint, searchPath)
			absolutePathOnTheBindMount, err := securejoin.SecureJoin(mount.Source, pathRelativeToBindMount)
			if err != nil {
				return "", "", nil, err
			}
			return mount.Source, absolutePathOnTheBindMount, nil, nil
		}

		if searchPath == "/" {
			// Cannot go beyond "/", so we're done.
			break
		}
		// Walk *down* the path (e.g., "/foo/bar/x" -> "/foo/bar").
		searchPath = filepath.Dir(searchPath)
	}

	// No volume, no bind mount but just a normal path on the container.
	return mountPoint, resolvedPathOnTheContainerMountPoint, nil, nil
}

// findVolume checks if the specified containerPath matches the destination
// path of a Volume.  Returns a matching Volume or nil.
func findVolume(c *Container, containerPath string) (*Volume, error) {
	runtime := c.Runtime()
	cleanedContainerPath := filepath.Clean(containerPath)
	for _, vol := range c.config.NamedVolumes {
		if cleanedContainerPath == filepath.Clean(vol.Dest) {
			return runtime.GetVolume(vol.Name)
		}
	}
	return nil, nil
}

// isSubDir checks whether path is a subdirectory of root.
func isSubDir(path, root string) bool {
	// check if the specified container path is below a bind mount.
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, "../")
}

// isPathOnVolume returns true if the specified containerPath is a subdir of any
// Volume's destination.
func isPathOnVolume(c *Container, containerPath string) bool {
	cleanedContainerPath := filepath.Clean(containerPath)
	for _, vol := range c.config.NamedVolumes {
		cleanedDestination := filepath.Clean(vol.Dest)
		if cleanedContainerPath == cleanedDestination {
			return true
		}
		if isSubDir(cleanedContainerPath, cleanedDestination) {
			return true
		}
		for dest := cleanedDestination; dest != "/" && dest != "."; dest = filepath.Dir(dest) {
			if cleanedContainerPath == dest {
				return true
			}
		}
	}
	return false
}

// findBindMount checks if the specified containerPath matches the destination
// path of a Mount.  Returns a matching Mount or nil.
func findBindMount(c *Container, containerPath string) *specs.Mount {
	cleanedPath := filepath.Clean(containerPath)
	for _, m := range c.config.Spec.Mounts {
		if m.Type != define.TypeBind {
			continue
		}
		if cleanedPath == filepath.Clean(m.Destination) {
			mount := m
			return &mount
		}
	}
	return nil
}

// / isPathOnMount returns true if the specified containerPath is a subdir of any
// Mount's destination.
func isPathOnMount(c *Container, containerPath string) bool {
	cleanedContainerPath := filepath.Clean(containerPath)
	for _, m := range c.config.Spec.Mounts {
		cleanedDestination := filepath.Clean(m.Destination)
		if cleanedContainerPath == cleanedDestination {
			return true
		}
		if isSubDir(cleanedContainerPath, cleanedDestination) {
			return true
		}
		for dest := cleanedDestination; dest != "/" && dest != "."; dest = filepath.Dir(dest) {
			if cleanedContainerPath == dest {
				return true
			}
		}
	}
	return false
}
