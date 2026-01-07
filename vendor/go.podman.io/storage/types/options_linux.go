package types

import (
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// these are default path for run and graph root for rootful users
	// for rootless path is constructed via getRootlessStorageOpts
	defaultRunRoot   string = "/run/containers/storage"
	defaultGraphRoot string = "/var/lib/containers/storage"
	SystemConfigFile        = "/usr/share/containers/storage.conf"
)

// defaultConfigFile path to the system wide storage.conf file
var (
	defaultOverrideConfigFile = "/etc/containers/storage.conf"
)

// canUseRootlessOverlay returns true if the overlay driver can be used for rootless containers
func canUseRootlessOverlay() bool {
	// we check first for fuse-overlayfs since it is cheaper.
	if path, _ := exec.LookPath("fuse-overlayfs"); path != "" {
		return true
	}

	// We cannot use overlay.SupportsNativeOverlay since canUseRootlessOverlay is called by Podman
	// before we enter the user namespace and the driver we pick here is written in the podman database.
	// Checking the kernel version is usually not a good idea since the feature could be back-ported, e.g. RHEL
	// but this is just an heuristic and on RHEL we always install the storage.conf file.
	// native overlay for rootless was added upstream in 5.13 (at least the first version that we support), so check
	// that the kernel is >= 5.13.
	var uts unix.Utsname
	if err := unix.Uname(&uts); err == nil {
		parts := strings.Split(string(uts.Release[:]), ".")
		major, _ := strconv.Atoi(parts[0])
		if major >= 6 {
			return true
		}
		if major == 5 && len(parts) > 1 {
			minor, _ := strconv.Atoi(parts[1])
			if minor >= 13 {
				return true
			}
		}
	}
	return false
}
