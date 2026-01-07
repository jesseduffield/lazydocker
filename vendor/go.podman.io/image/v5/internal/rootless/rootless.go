package rootless

import (
	"os"
	"strconv"
)

// GetRootlessEUID returns the UID of the current user (in the parent userNS, if any)
//
// Podman and similar software, in “rootless” configuration, when run as a non-root
// user, very early switches to a user namespace, where Geteuid() == 0 (but does not
// switch to a limited mount namespace); so, code relying on Geteuid() would use
// system-wide paths in e.g. /var, when the user is actually not privileged to write to
// them, and expects state to be stored in the home directory.
//
// If Podman is setting up such a user namespace, it records the original UID in an
// environment variable, allowing us to make choices based on the actual user’s identity.
func GetRootlessEUID() int {
	euidEnv := os.Getenv("_CONTAINERS_ROOTLESS_UID")
	if euidEnv != "" {
		euid, _ := strconv.Atoi(euidEnv)
		return euid
	}
	return os.Geteuid()
}
