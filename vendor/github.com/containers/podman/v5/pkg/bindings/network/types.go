package network

import (
	"net"
)

// CreateOptions are optional options for creating networks
//
//go:generate go run ../generator/generator.go CreateOptions
type CreateOptions struct {
	// DisableDNS turns off use of DNSMasq for name resolution
	// on the network
	DisableDNS *bool
	// Driver is the name of network driver
	Driver *string
	// Gateway of the network
	Gateway *net.IP
	// Internal turns off communication outside the networking
	// being created
	Internal *bool
	// Labels are metadata that can be associated with the network
	Labels map[string]string
	// MacVLAN is the name of the macvlan network to associate with
	MacVLAN *string
	// Range is the CIDR description of leasable IP addresses
	IPRange *net.IPNet `scheme:"range"`
	// Subnet to use
	Subnet *net.IPNet
	// IPv6 means the network is ipv6 capable
	IPv6 *bool
	// Options are a mapping of driver options and values.
	Options map[string]string
	// Name of the network
	Name *string
}

// InspectOptions are optional options for inspecting networks
//
//go:generate go run ../generator/generator.go InspectOptions
type InspectOptions struct {
}

// RemoveOptions are optional options for inspecting networks
//
//go:generate go run ../generator/generator.go RemoveOptions
type RemoveOptions struct {
	// Force removes the network even if it is being used
	Force   *bool
	Timeout *uint
}

// ListOptions are optional options for listing networks
//
//go:generate go run ../generator/generator.go ListOptions
type ListOptions struct {
	// Filters are applied to the list of networks to be more
	// specific on the output
	Filters map[string][]string
}

// NetworkUpdateOptions describes options to update a network
//
//go:generate go run ../generator/generator.go UpdateOptions
type UpdateOptions struct {
	AddDNSServers    []string `json:"adddnsservers"`
	RemoveDNSServers []string `json:"removednsservers"`
}

// DisconnectOptions are optional options for disconnecting
// containers from a network
//
//go:generate go run ../generator/generator.go DisconnectOptions
type DisconnectOptions struct {
	// Force indicates to remove the container from
	// the network forcibly
	Force *bool
}

// ExistsOptions are optional options for checking
// if a network exists
//
//go:generate go run ../generator/generator.go ExistsOptions
type ExistsOptions struct {
}

// PruneOptions are optional options for removing unused
// networks
//
//go:generate go run ../generator/generator.go PruneOptions
type PruneOptions struct {
	// Filters are applied to the prune of networks to be more
	// specific on choosing
	Filters map[string][]string
}

// ExtraCreateOptions are optional additional configuration flags for creating Networks
// that are not part of the network configuration
//
//go:generate go run ../generator/generator.go ExtraCreateOptions
type ExtraCreateOptions struct {
	// IgnoreIfExists if true, do not fail if the network already exists
	IgnoreIfExists *bool `schema:"ignoreIfExists"`
}
