package types

import (
	commonTypes "go.podman.io/common/libnetwork/types"
)

// NetworkPruneReport containers the name of network and an error
// associated in its pruning (removal)
// swagger:model NetworkPruneReport
type NetworkPruneReport struct {
	Name  string
	Error error
}

// NetworkReloadReport describes the results of reloading a container network.
type NetworkReloadReport struct {
	Id  string
	Err error
}

// NetworkConnectOptions describes options for connecting
// a container to a network
type NetworkConnectOptions struct {
	Container string `json:"container"`
	commonTypes.PerNetworkOptions
}

// NetworkRmReport describes the results of network removal
type NetworkRmReport struct {
	Name string
	Err  error
}

type NetworkCreateReport struct {
	Name string
}

type NetworkInspectReport struct {
	commonTypes.Network

	Containers map[string]NetworkContainerInfo `json:"containers"`
}

type NetworkContainerInfo struct {
	// Name of the container
	Name string `json:"name"`

	// Interfaces configured for this container with their addresses
	Interfaces map[string]commonTypes.NetInterface `json:"interfaces,omitempty"`
}
