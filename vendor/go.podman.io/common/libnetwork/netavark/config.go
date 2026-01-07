//go:build linux || freebsd

package netavark

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"time"

	internalutil "go.podman.io/common/libnetwork/internal/util"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/storage/pkg/stringid"
)

func sliceRemoveDuplicates(strList []string) []string {
	list := make([]string, 0, len(strList))
	for _, item := range strList {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}
	return list
}

func (n *netavarkNetwork) commitNetwork(network *types.Network) error {
	if err := os.MkdirAll(n.networkConfigDir, 0o755); err != nil {
		return nil
	}
	confPath := filepath.Join(n.networkConfigDir, network.Name+".json")
	f, err := os.Create(confPath)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "     ")
	err = enc.Encode(network)
	if err != nil {
		return err
	}
	return nil
}

func (n *netavarkNetwork) NetworkUpdate(name string, options types.NetworkUpdateOptions) error {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return err
	}
	network, err := n.getNetwork(name)
	if err != nil {
		return err
	}
	// Nameservers must be IP Addresses.
	for _, dnsServer := range options.AddDNSServers {
		if net.ParseIP(dnsServer) == nil {
			return fmt.Errorf("unable to parse ip %s specified in AddDNSServer: %w", dnsServer, types.ErrInvalidArg)
		}
	}
	for _, dnsServer := range options.RemoveDNSServers {
		if net.ParseIP(dnsServer) == nil {
			return fmt.Errorf("unable to parse ip %s specified in RemoveDNSServer: %w", dnsServer, types.ErrInvalidArg)
		}
	}
	networkDNSServersBefore := network.NetworkDNSServers
	networkDNSServersAfter := []string{}
	for _, server := range networkDNSServersBefore {
		if slices.Contains(options.RemoveDNSServers, server) {
			continue
		}
		networkDNSServersAfter = append(networkDNSServersAfter, server)
	}
	networkDNSServersAfter = append(networkDNSServersAfter, options.AddDNSServers...)
	networkDNSServersAfter = sliceRemoveDuplicates(networkDNSServersAfter)
	network.NetworkDNSServers = networkDNSServersAfter
	if reflect.DeepEqual(networkDNSServersBefore, networkDNSServersAfter) {
		return nil
	}
	err = n.commitNetwork(network)
	if err != nil {
		return err
	}

	return n.execUpdate(network.Name, network.NetworkDNSServers)
}

// NetworkCreate will take a partial filled Network and fill the
// missing fields. It creates the Network and returns the full Network.
func (n *netavarkNetwork) NetworkCreate(net types.Network, options *types.NetworkCreateOptions) (types.Network, error) {
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
				return *network, nil
			}
		}
		return types.Network{}, err
	}
	// add the new network to the map
	n.networks[network.Name] = network
	return *network, nil
}

func (n *netavarkNetwork) networkCreate(newNetwork *types.Network, defaultNet bool) (*types.Network, error) {
	// if no driver is set use the default one
	if newNetwork.Driver == "" {
		newNetwork.Driver = types.DefaultNetworkDriver
	}
	if !defaultNet {
		// FIXME: Should we use a different type for network create without the ID field?
		// the caller is not allowed to set a specific ID
		if newNetwork.ID != "" {
			return nil, fmt.Errorf("ID can not be set for network create: %w", types.ErrInvalidArg)
		}

		// generate random network ID
		var i int
		for i = range 1000 {
			id := stringid.GenerateNonCryptoID()
			if _, err := n.getNetwork(id); err != nil {
				newNetwork.ID = id
				break
			}
		}
		if i == 1000 {
			return nil, errors.New("failed to create random network ID")
		}
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

		checkBridgeConflict := true
		// validate the given options,
		for key, value := range newNetwork.Options {
			switch key {
			case types.MTUOption:
				_, err = internalutil.ParseMTU(value)
				if err != nil {
					return nil, err
				}

			case types.VLANOption:
				_, err = internalutil.ParseVlan(value)
				if err != nil {
					return nil, err
				}
				// Unset used networks here to ensure that when using vlan networks
				// we do not error if the subnet is already in use on the host.
				// https://github.com/containers/podman/issues/25736
				usedNetworks = nil
				// If there is no vlan there should be no other config with the same bridge.
				// However with vlan we want to allow that so that you can have different
				// configs on the same bridge but different vlans
				// https://github.com/containers/common/issues/2095
				checkBridgeConflict = false

			case types.IsolateOption:
				val, err := internalutil.ParseIsolate(value)
				if err != nil {
					return nil, err
				}
				newNetwork.Options[types.IsolateOption] = val
			case types.MetricOption:
				_, err := strconv.ParseUint(value, 10, 32)
				if err != nil {
					return nil, err
				}
			case types.NoDefaultRoute:
				val, err := strconv.ParseBool(value)
				if err != nil {
					return nil, err
				}
				// rust only support "true" or "false" while go can parse 1 and 0 as well so we need to change it
				newNetwork.Options[types.NoDefaultRoute] = strconv.FormatBool(val)
			case types.VRFOption:
				if len(value) == 0 {
					return nil, errors.New("invalid vrf name")
				}
			case types.ModeOption:
				switch value {
				case types.BridgeModeManaged:
				case types.BridgeModeUnmanaged:
					// Unset used networks here to ensure that when using unmanaged networks
					// we do not error if the subnet is already in use on the host.
					// https://github.com/containers/common/issues/2322
					usedNetworks = nil
					// Also make sure we don't error if the bridge name is already used as well.
					checkBridgeConflict = false
				default:
					return nil, fmt.Errorf("unknown bridge mode %q", value)
				}
			default:
				return nil, fmt.Errorf("unsupported bridge network option %s", key)
			}
		}

		err = internalutil.CreateBridge(n, newNetwork, usedNetworks, n.defaultsubnetPools, checkBridgeConflict)
		if err != nil {
			return nil, err
		}

	case types.MacVLANNetworkDriver, types.IPVLANNetworkDriver:
		err = createIpvlanOrMacvlan(newNetwork)
		if err != nil {
			return nil, err
		}
	default:
		net, err := n.createPlugin(newNetwork)
		if err != nil {
			return nil, err
		}
		newNetwork = net
	}

	// when we do not have ipam we must disable dns
	internalutil.IpamNoneDisableDNS(newNetwork)

	// process NetworkDNSServers
	if len(newNetwork.NetworkDNSServers) > 0 && !newNetwork.DNSEnabled {
		return nil, fmt.Errorf("cannot set NetworkDNSServers if DNS is not enabled for the network: %w", types.ErrInvalidArg)
	}
	// validate ip address
	for _, dnsServer := range newNetwork.NetworkDNSServers {
		if net.ParseIP(dnsServer) == nil {
			return nil, fmt.Errorf("unable to parse ip %s specified in NetworkDNSServers: %w", dnsServer, types.ErrInvalidArg)
		}
	}

	// add gateway when not internal or dns enabled
	addGateway := !newNetwork.Internal || newNetwork.DNSEnabled
	err = internalutil.ValidateSubnets(newNetwork, addGateway, usedNetworks)
	if err != nil {
		return nil, err
	}

	// validate routes
	err = internalutil.ValidateRoutes(newNetwork.Routes)
	if err != nil {
		return nil, err
	}

	newNetwork.Created = time.Now()

	if !defaultNet {
		err = n.commitNetwork(newNetwork)
		if err != nil {
			return nil, err
		}
	}

	return newNetwork, nil
}

// ipvlan shares the same mac address so supporting DHCP is not really possible.
var errIpvlanNoDHCP = errors.New("ipam driver dhcp is not supported with ipvlan")

func createIpvlanOrMacvlan(network *types.Network) error {
	if network.NetworkInterface != "" {
		interfaceNames, err := internalutil.GetLiveNetworkNames()
		if err != nil {
			return err
		}
		if !slices.Contains(interfaceNames, network.NetworkInterface) {
			return fmt.Errorf("parent interface %s does not exist", network.NetworkInterface)
		}
	}

	driver := network.Driver
	isMacVlan := driver != types.IPVLANNetworkDriver

	// always turn dns off with macvlan, it is not implemented in netavark
	// and makes little sense to support with macvlan
	// see https://github.com/containers/netavark/pull/467
	network.DNSEnabled = false

	// we already validated the drivers before so we just have to set the default here
	switch network.IPAMOptions[types.Driver] {
	case "":
		if len(network.Subnets) == 0 {
			// if no subnets and no driver choose dhcp
			network.IPAMOptions[types.Driver] = types.DHCPIPAMDriver
			if !isMacVlan {
				return errIpvlanNoDHCP
			}
		} else {
			network.IPAMOptions[types.Driver] = types.HostLocalIPAMDriver
		}
	case types.HostLocalIPAMDriver:
		if len(network.Subnets) == 0 {
			return fmt.Errorf("%s driver needs at least one subnet specified when the host-local ipam driver is set", driver)
		}
	case types.DHCPIPAMDriver:
		if !isMacVlan {
			return errIpvlanNoDHCP
		}
		if len(network.Subnets) > 0 {
			return errors.New("ipam driver dhcp set but subnets are set")
		}
	}

	// validate the given options, we do not need them but just check to make sure they are valid
	for key, value := range network.Options {
		switch key {
		case types.ModeOption:
			if isMacVlan {
				if !slices.Contains(types.ValidMacVLANModes, value) {
					return fmt.Errorf("unknown macvlan mode %q", value)
				}
			} else {
				if !slices.Contains(types.ValidIPVLANModes, value) {
					return fmt.Errorf("unknown ipvlan mode %q", value)
				}
			}
		case types.MetricOption:
			_, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return err
			}
		case types.MTUOption:
			_, err := internalutil.ParseMTU(value)
			if err != nil {
				return err
			}
		case types.NoDefaultRoute:
			val, err := strconv.ParseBool(value)
			if err != nil {
				return err
			}
			// rust only support "true" or "false" while go can parse 1 and 0 as well so we need to change it
			network.Options[types.NoDefaultRoute] = strconv.FormatBool(val)
		case types.BclimOption:
			if isMacVlan {
				_, err := strconv.ParseInt(value, 10, 32)
				if err != nil {
					return fmt.Errorf("failed to parse %q option: %w", key, err)
				}
				// do not fallthrough for macvlan
				break
			}
			// bclim is only valid for macvlan not ipvlan so fallthrough to error case
			fallthrough
		default:
			return fmt.Errorf("unsupported %s network option %s", driver, key)
		}
	}
	return nil
}

// NetworkRemove will remove the Network with the given name or ID.
// It does not ensure that the network is unused.
func (n *netavarkNetwork) NetworkRemove(nameOrID string) error {
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
	if network.Name == n.defaultNetwork {
		return fmt.Errorf("default network %s cannot be removed", n.defaultNetwork)
	}

	// remove the ipam bucket for this network
	if err := n.removeNetworkIPAMBucket(network); err != nil {
		return err
	}

	file := filepath.Join(n.networkConfigDir, network.Name+".json")
	// make sure to not error for ErrNotExist
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	delete(n.networks, network.Name)
	return nil
}

// NetworkList will return all known Networks. Optionally you can
// supply a list of filter functions. Only if a network matches all
// functions it is returned.
func (n *netavarkNetwork) NetworkList(filters ...types.FilterFunc) ([]types.Network, error) {
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
			if !filter(*net) {
				continue outer
			}
		}
		networks = append(networks, *net)
	}
	return networks, nil
}

// NetworkInspect will return the Network with the given name or ID.
func (n *netavarkNetwork) NetworkInspect(nameOrID string) (types.Network, error) {
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
	return *network, nil
}

func validateIPAMDriver(n *types.Network) error {
	ipamDriver := n.IPAMOptions[types.Driver]
	switch ipamDriver {
	case "", types.HostLocalIPAMDriver, types.DHCPIPAMDriver:
	case types.NoneIPAMDriver:
		if len(n.Subnets) > 0 {
			return errors.New("none ipam driver is set but subnets are given")
		}
	default:
		return fmt.Errorf("unsupported ipam driver %q", ipamDriver)
	}
	return nil
}

var errInvalidPluginResult = errors.New("invalid plugin result")

func (n *netavarkNetwork) createPlugin(net *types.Network) (*types.Network, error) {
	path, err := getPlugin(net.Driver, n.pluginDirs)
	if err != nil {
		return nil, err
	}
	result := new(types.Network)
	err = n.execPlugin(path, []string{"create"}, net, result)
	if err != nil {
		return nil, fmt.Errorf("plugin %s failed: %w", path, err)
	}
	// now make sure that neither the name, ID, driver were changed by the plugin
	if net.Name != result.Name {
		return nil, fmt.Errorf("%w: changed network name", errInvalidPluginResult)
	}
	if net.ID != result.ID {
		return nil, fmt.Errorf("%w: changed network ID", errInvalidPluginResult)
	}
	if net.Driver != result.Driver {
		return nil, fmt.Errorf("%w: changed network driver", errInvalidPluginResult)
	}
	return result, nil
}

func getAllPlugins(dirs []string) []string {
	var plugins []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if !slices.Contains(plugins, name) {
					plugins = append(plugins, name)
				}
			}
		}
	}
	return plugins
}

func getPlugin(name string, dirs []string) (string, error) {
	for _, dir := range dirs {
		fullpath := filepath.Join(dir, name)
		st, err := os.Stat(fullpath)
		if err == nil && st.Mode().IsRegular() {
			return fullpath, nil
		}
	}
	return "", fmt.Errorf("failed to find driver or plugin %q", name)
}
