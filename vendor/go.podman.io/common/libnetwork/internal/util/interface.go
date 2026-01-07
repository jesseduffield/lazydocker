package util

import "go.podman.io/common/libnetwork/types"

// This is a helper package to allow code sharing between the different
// network interfaces.

// NetUtil is a helper interface which all network interfaces should implement to allow easy code sharing.
type NetUtil interface {
	// ForEach executes the given function for each network
	ForEach(func(types.Network))
	// Len returns the number of networks
	Len() int
	// DefaultInterfaceName return the default interface name, this will be suffixed by a number
	DefaultInterfaceName() string
	// Network returns the network with the given name or ID.
	// It returns an error if the network is not found
	Network(nameOrID string) (*types.Network, error)
}
