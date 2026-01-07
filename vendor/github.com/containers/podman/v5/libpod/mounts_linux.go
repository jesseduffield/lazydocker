//go:build !remote

package libpod

const (
	// MountPrivate represents the private mount option.
	MountPrivate = "private"
	// MountRPrivate represents the rprivate mount option.
	MountRPrivate = "rprivate"
	// MountShared represents the shared mount option.
	MountShared = "shared"
	// MountRShared represents the rshared mount option.
	MountRShared = "rshared"
	// MountSlave represents the slave mount option.
	MountSlave = "slave"
	// MountRSlave represents the rslave mount option.
	MountRSlave = "rslave"
)
