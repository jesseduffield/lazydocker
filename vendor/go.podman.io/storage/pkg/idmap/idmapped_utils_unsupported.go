//go:build !linux

package idmap

import (
	"fmt"

	"go.podman.io/storage/pkg/idtools"
)

// CreateIDMappedMount creates a IDMapped bind mount from SOURCE to TARGET using the user namespace
// for the PID process.
func CreateIDMappedMount(source, target string, pid int) error {
	return fmt.Errorf("IDMapped mounts are not supported")
}

// CreateUsernsProcess forks the current process and creates a user namespace using the specified
// mappings.  It returns the pid of the new process.
func CreateUsernsProcess(uidMaps []idtools.IDMap, gidMaps []idtools.IDMap) (int, func(), error) {
	return -1, nil, fmt.Errorf("IDMapped mounts are not supported")
}
