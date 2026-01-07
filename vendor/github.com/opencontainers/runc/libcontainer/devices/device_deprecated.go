package devices

import "github.com/opencontainers/cgroups/devices/config"

// Deprecated: use [github.com/opencontainers/cgroups/devices/config].
const (
	Wildcard       = config.Wildcard
	WildcardDevice = config.WildcardDevice
	BlockDevice    = config.BlockDevice
	CharDevice     = config.CharDevice
	FifoDevice     = config.FifoDevice
)

// Deprecated: use [github.com/opencontainers/cgroups/devices/config].
type (
	Device      = config.Device
	Permissions = config.Permissions
	Type        = config.Type
	Rule        = config.Rule
)
