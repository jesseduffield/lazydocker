//go:build freebsd || netbsd

package types

const (
	// these are default path for run and graph root for rootful users
	// for rootless path is constructed via getRootlessStorageOpts
	defaultRunRoot   string = "/var/run/containers/storage"
	defaultGraphRoot string = "/var/db/containers/storage"
	SystemConfigFile        = "/usr/local/share/containers/storage.conf"
)

// defaultConfigFile path to the system wide storage.conf file
var (
	defaultOverrideConfigFile = "/usr/local/etc/containers/storage.conf"
)

// canUseRootlessOverlay returns true if the overlay driver can be used for rootless containers
func canUseRootlessOverlay() bool {
	return false
}
