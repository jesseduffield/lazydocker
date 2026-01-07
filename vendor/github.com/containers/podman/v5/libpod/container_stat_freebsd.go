//go:build !remote

package libpod

import (
	"github.com/containers/buildah/copier"
)

// On FreeBSD, jails use the global mount namespace, filtered to only
// the mounts the jail should see. This means that we can use
// statOnHost whether the container is running or not.
// container is running
func (c *Container) statInContainer(mountPoint string, containerPath string) (*copier.StatForItem, string, string, error) {
	return c.statOnHost(mountPoint, containerPath)
}
