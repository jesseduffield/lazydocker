//go:build darwin

package unshare

import (
	"os"

	"github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/storage/pkg/idtools"
)

const (
	// UsernsEnvName is the environment variable, if set indicates in rootless mode
	UsernsEnvName = "_CONTAINERS_USERNS_CONFIGURED"
)

// IsRootless tells us if we are running in rootless mode
func IsRootless() bool {
	return true
}

// GetRootlessUID returns the UID of the user in the parent userNS
func GetRootlessUID() int {
	return os.Getuid()
}

// GetRootlessGID returns the GID of the user in the parent userNS
func GetRootlessGID() int {
	return os.Getgid()
}

// RootlessEnv returns the environment settings for the rootless containers
func RootlessEnv() []string {
	return append(os.Environ(), UsernsEnvName+"=")
}

// MaybeReexecUsingUserNamespace re-exec the process in a new namespace
func MaybeReexecUsingUserNamespace(evenForRoot bool) {
}

// GetHostIDMappings reads mappings for the specified process (or the current
// process if pid is "self" or an empty string) from the kernel.
func GetHostIDMappings(pid string) ([]specs.LinuxIDMapping, []specs.LinuxIDMapping, error) {
	return nil, nil, nil
}

// ParseIDMappings parses mapping triples.
func ParseIDMappings(uidmap, gidmap []string) ([]idtools.IDMap, []idtools.IDMap, error) {
	uid, err := idtools.ParseIDMap(uidmap, "userns-uid-map")
	if err != nil {
		return nil, nil, err
	}
	gid, err := idtools.ParseIDMap(gidmap, "userns-gid-map")
	if err != nil {
		return nil, nil, err
	}
	return uid, gid, nil
}
