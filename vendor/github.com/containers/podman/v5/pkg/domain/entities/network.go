package entities

import (
	"net"

	entitiesTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
)

// NetworkListOptions describes options for listing networks in cli
type NetworkListOptions struct {
	Format  string
	Quiet   bool
	Filters map[string][]string
}

// NetworkReloadOptions describes options for reloading container network
// configuration.
type NetworkReloadOptions struct {
	All    bool
	Latest bool
}

// NetworkReloadReport describes the results of reloading a container network.
type NetworkReloadReport = entitiesTypes.NetworkReloadReport

// NetworkRmOptions describes options for removing networks
type NetworkRmOptions struct {
	Force   bool
	Timeout *uint
}

// NetworkRmReport describes the results of network removal
type NetworkRmReport = entitiesTypes.NetworkRmReport

// NetworkCreateOptions describes options to create a network
type NetworkCreateOptions struct {
	DisableDNS        bool
	Driver            string
	Gateways          []net.IP
	Internal          bool
	Labels            map[string]string
	MacVLAN           string
	NetworkDNSServers []string
	Ranges            []string
	Subnets           []string
	Routes            []string
	IPv6              bool
	// Mapping of driver options and values.
	Options map[string]string
	// IgnoreIfExists if true, do not fail if the network already exists
	IgnoreIfExists bool
	// InterfaceName sets the NetworkInterface in the network config
	InterfaceName string
}

// NetworkUpdateOptions describes options to update a network
type NetworkUpdateOptions struct {
	AddDNSServers    []string `json:"adddnsservers"`
	RemoveDNSServers []string `json:"removednsservers"`
}

// NetworkCreateReport describes a created network for the cli
type NetworkCreateReport = entitiesTypes.NetworkCreateReport

// NetworkDisconnectOptions describes options for disconnecting
// containers from networks
type NetworkDisconnectOptions struct {
	Container string
	Force     bool
}

// NetworkConnectOptions describes options for connecting
// a container to a network
type NetworkConnectOptions = entitiesTypes.NetworkConnectOptions

// NetworkPruneReport containers the name of network and an error
// associated in its pruning (removal)
type NetworkPruneReport = entitiesTypes.NetworkPruneReport

// NetworkPruneOptions describes options for pruning unused networks
type NetworkPruneOptions struct {
	Filters map[string][]string
}

type NetworkInspectReport = entitiesTypes.NetworkInspectReport
type NetworkContainerInfo = entitiesTypes.NetworkContainerInfo
