//go:build !(linux || freebsd) || !cgo

package rootless

import (
	"errors"
	"os"

	"go.podman.io/storage/pkg/idtools"
)

// IsRootless returns whether the user is rootless
func IsRootless() bool {
	uid := os.Geteuid()
	// os.Geteuid() on Windows returns -1
	if uid == -1 {
		return false
	}
	return uid != 0
}

// BecomeRootInUserNS re-exec podman in a new userNS.  It returns whether podman was re-executed
// into a new user namespace and the return code from the re-executed podman process.
// If podman was re-executed the caller needs to propagate the error code returned by the child
// process.  It is a convenience function for BecomeRootInUserNSWithOpts with a default configuration.
func BecomeRootInUserNS(_ string) (bool, int, error) {
	return false, -1, errors.New("this function is not supported on this os")
}

// GetRootlessUID returns the UID of the user in the parent userNS
func GetRootlessUID() int {
	return -1
}

// GetRootlessGID returns the GID of the user in the parent userNS
func GetRootlessGID() int {
	return -1
}

// TryJoinFromFilePaths attempts to join the namespaces of the pid files in paths.
// This is useful when there are already running containers and we
// don't have a pause process yet.  We can use the paths to the conmon
// processes to attempt joining their namespaces.
func TryJoinFromFilePaths(_ string, _ []string) (bool, int, error) {
	return false, -1, errors.New("this function is not supported on this os")
}

// ConfigurationMatches checks whether the additional uids/gids configured for the user
// match the current user namespace.
func ConfigurationMatches() (bool, error) {
	return true, nil
}

// GetConfiguredMappings returns the additional IDs configured for the current user.
func GetConfiguredMappings(_ bool) ([]idtools.IDMap, []idtools.IDMap, error) {
	return nil, nil, errors.New("this function is not supported on this os")
}

// IsFdInherited checks whether the fd is opened and valid to use
func IsFdInherited(_ int) bool {
	return false
}
