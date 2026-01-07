//go:build (linux || freebsd) && cni

package cni

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"

	"github.com/sirupsen/logrus"
	internalutil "go.podman.io/common/libnetwork/internal/util"
	"go.podman.io/common/libnetwork/types"
)

func (n *cniNetwork) NetworkUpdate(_ string, _ types.NetworkUpdateOptions) error {
	return fmt.Errorf("NetworkUpdate is not supported for backend CNI: %w", types.ErrInvalidArg)
}

// NetworkCreate will take a partial filled Network and fill the
// missing fields. It creates the Network and returns the full Network.
func (n *cniNetwork) NetworkCreate(net types.Network, options *types.NetworkCreateOptions) (types.Network, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return types.Network{}, err
	}
	network, err := n.networkCreate(&net, false)
	if err != nil {
		if options != nil && options.IgnoreIfExists && errors.Is(err, types.ErrNetworkExists) {
			if network, ok := n.networks[net.Name]; ok {
				return *network.libpodNet, nil
			}
		}
		return types.Network{}, err
	}
	// add the new network to the map
	n.networks[network.libpodNet.Name] = network
	return *network.libpodNet, nil
}

// networkCreate will fill out the given network struct and return the new network entry.
// If defaultNet is true it will not validate against used subnets and it will not write the cni config to disk.
func (n *cniNetwork) networkCreate(newNetwork *types.Network, defaultNet bool) (*network, error) {
	if len(newNetwork.NetworkDNSServers) > 0 {
		return nil, fmt.Errorf("NetworkDNSServers cannot be configured for backend CNI: %w", types.ErrInvalidArg)
	}
	// if no driver is set use the default one
	if newNetwork.Driver == "" {
		newNetwork.Driver = types.DefaultNetworkDriver
	}

	// FIXME: Should we use a different type for network create without the ID field?
	// the caller is not allowed to set a specific ID
	if newNetwork.ID != "" {
		return nil, fmt.Errorf("ID can not be set for network create: %w", types.ErrInvalidArg)
	}

	err := internalutil.CommonNetworkCreate(n, newNetwork)
	if err != nil {
		return nil, err
	}

	err = validateIPAMDriver(newNetwork)
	if err != nil {
		return nil, err
	}

	// Only get the used networks for validation if we do not create the default network.
	// The default network should not be validated against used subnets, we have to ensure
	// that this network can always be created even when a subnet is already used on the host.
	// This could happen if you run a container on this net, then the cni interface will be
	// created on the host and "block" this subnet from being used again.
	// Therefore the next podman command tries to create the default net again and it would
	// fail because it thinks the network is used on the host.
	var usedNetworks []*net.IPNet
	if !defaultNet && newNetwork.Driver == types.BridgeNetworkDriver {
		usedNetworks, err = internalutil.GetUsedSubnets(n)
		if err != nil {
			return nil, err
		}
	}

	switch newNetwork.Driver {
	case types.BridgeNetworkDriver:
		internalutil.MapDockerBridgeDriverOptions(newNetwork)
		err = internalutil.CreateBridge(n, newNetwork, usedNetworks, n.defaultsubnetPools, true)
		if err != nil {
			return nil, err
		}
	case types.MacVLANNetworkDriver, types.IPVLANNetworkDriver:
		err = createIPMACVLAN(newNetwork)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported driver %s: %w", newNetwork.Driver, types.ErrInvalidArg)
	}

	err = internalutil.ValidateSubnets(newNetwork, !newNetwork.Internal, usedNetworks)
	if err != nil {
		return nil, err
	}

	// generate the network ID
	newNetwork.ID = getNetworkIDFromName(newNetwork.Name)

	// when we do not have ipam we must disable dns
	internalutil.IpamNoneDisableDNS(newNetwork)

	// FIXME: Should this be a hard error?
	if newNetwork.DNSEnabled && newNetwork.Internal && hasDNSNamePlugin(n.cniPluginDirs) {
		logrus.Warnf("dnsname and internal networks are incompatible. dnsname plugin not configured for network %s", newNetwork.Name)
		newNetwork.DNSEnabled = false
	}

	cniConf, path, err := n.createCNIConfigListFromNetwork(newNetwork, !defaultNet)
	if err != nil {
		return nil, err
	}
	return &network{cniNet: cniConf, libpodNet: newNetwork, filename: path}, nil
}

// NetworkRemove will remove the Network with the given name or ID.
// It does not ensure that the network is unused.
func (n *cniNetwork) NetworkRemove(nameOrID string) error {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return err
	}

	network, err := n.getNetwork(nameOrID)
	if err != nil {
		return err
	}

	// Removing the default network is not allowed.
	if network.libpodNet.Name == n.defaultNetwork {
		return fmt.Errorf("default network %s cannot be removed", n.defaultNetwork)
	}

	// Remove the bridge network interface on the host.
	if network.libpodNet.Driver == types.BridgeNetworkDriver {
		deleteLink(network.libpodNet.NetworkInterface)
	}

	file := network.filename
	delete(n.networks, network.libpodNet.Name)

	// make sure to not error for ErrNotExist
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// NetworkList will return all known Networks. Optionally you can
// supply a list of filter functions. Only if a network matches all
// functions it is returned.
func (n *cniNetwork) NetworkList(filters ...types.FilterFunc) ([]types.Network, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return nil, err
	}

	networks := make([]types.Network, 0, len(n.networks))
outer:
	for _, net := range n.networks {
		for _, filter := range filters {
			// All filters have to match, if one does not match we can skip to the next network.
			if !filter(*net.libpodNet) {
				continue outer
			}
		}
		networks = append(networks, *net.libpodNet)
	}
	return networks, nil
}

// NetworkInspect will return the Network with the given name or ID.
func (n *cniNetwork) NetworkInspect(nameOrID string) (types.Network, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return types.Network{}, err
	}

	network, err := n.getNetwork(nameOrID)
	if err != nil {
		return types.Network{}, err
	}
	return *network.libpodNet, nil
}

func createIPMACVLAN(network *types.Network) error {
	if network.NetworkInterface != "" {
		interfaceNames, err := internalutil.GetLiveNetworkNames()
		if err != nil {
			return err
		}
		if !slices.Contains(interfaceNames, network.NetworkInterface) {
			return fmt.Errorf("parent interface %s does not exist", network.NetworkInterface)
		}
	}

	switch network.IPAMOptions[types.Driver] {
	// set default
	case "":
		if len(network.Subnets) == 0 {
			// if no subnets and no driver choose dhcp
			network.IPAMOptions[types.Driver] = types.DHCPIPAMDriver
		} else {
			network.IPAMOptions[types.Driver] = types.HostLocalIPAMDriver
		}
	case types.HostLocalIPAMDriver:
		if len(network.Subnets) == 0 {
			return errors.New("host-local ipam driver set but no subnets are given")
		}
	}

	if network.IPAMOptions[types.Driver] == types.DHCPIPAMDriver && network.Internal {
		return errors.New("internal is not supported with macvlan and dhcp ipam driver")
	}
	return nil
}

func validateIPAMDriver(n *types.Network) error {
	ipamDriver := n.IPAMOptions[types.Driver]
	switch ipamDriver {
	case "", types.HostLocalIPAMDriver:
	case types.DHCPIPAMDriver, types.NoneIPAMDriver:
		if len(n.Subnets) > 0 {
			return fmt.Errorf("%s ipam driver is set but subnets are given", ipamDriver)
		}
	default:
		return fmt.Errorf("unsupported ipam driver %q", ipamDriver)
	}
	return nil
}
