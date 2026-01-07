package types

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
	return false
}
