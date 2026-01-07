//go:build (linux || freebsd) && cni

package cni

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containernetworking/cni/libcni"
	"github.com/sirupsen/logrus"
	internalutil "go.podman.io/common/libnetwork/internal/util"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/libnetwork/util"
	"golang.org/x/sys/unix"
)

func createNetworkFromCNIConfigList(conf *libcni.NetworkConfigList, confPath string) (*types.Network, error) {
	network := types.Network{
		Name:        conf.Name,
		ID:          getNetworkIDFromName(conf.Name),
		Labels:      map[string]string{},
		Options:     map[string]string{},
		IPAMOptions: map[string]string{},
	}

	cniJSON := make(map[string]any)
	err := json.Unmarshal(conf.Bytes, &cniJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal network config %s: %w", conf.Name, err)
	}
	if args, ok := cniJSON["args"]; ok {
		if key, ok := args.(map[string]any); ok {
			// read network labels and options from the conf file
			network.Labels = getNetworkArgsFromConfList(key, podmanLabelKey)
			network.Options = getNetworkArgsFromConfList(key, podmanOptionsKey)
		}
	}

	t, err := fileTime(confPath)
	if err != nil {
		return nil, err
	}
	network.Created = t

	firstPlugin := conf.Plugins[0]
	network.Driver = firstPlugin.Network.Type

	switch firstPlugin.Network.Type {
	case types.BridgeNetworkDriver:
		var bridge hostLocalBridge
		err := json.Unmarshal(firstPlugin.Bytes, &bridge)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal the bridge plugin config in %s: %w", confPath, err)
		}
		network.NetworkInterface = bridge.BrName

		// if isGateway is false we have an internal network
		if !bridge.IsGW {
			network.Internal = true
		}

		// set network options
		if bridge.MTU != 0 {
			network.Options[types.MTUOption] = strconv.Itoa(bridge.MTU)
		}
		if bridge.Vlan != 0 {
			network.Options[types.VLANOption] = strconv.Itoa(bridge.Vlan)
		}

		err = convertIPAMConfToNetwork(&network, &bridge.IPAM, confPath)
		if err != nil {
			return nil, err
		}

	case types.MacVLANNetworkDriver, types.IPVLANNetworkDriver:
		var vlan VLANConfig
		err := json.Unmarshal(firstPlugin.Bytes, &vlan)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal the macvlan plugin config in %s: %w", confPath, err)
		}
		network.NetworkInterface = vlan.Master

		// set network options
		if vlan.MTU != 0 {
			network.Options[types.MTUOption] = strconv.Itoa(vlan.MTU)
		}

		if vlan.Mode != "" {
			network.Options[types.ModeOption] = vlan.Mode
		}

		err = convertIPAMConfToNetwork(&network, &vlan.IPAM, confPath)
		if err != nil {
			return nil, err
		}

	default:
		// A warning would be good but users would get this warning every time so keep this at info level.
		logrus.Infof("Unsupported CNI config type %s in %s, this network can still be used but inspect or list cannot show all information",
			firstPlugin.Network.Type, confPath)
	}

	// check if the dnsname plugin is configured
	network.DNSEnabled = findPluginByName(conf.Plugins, "dnsname") != nil

	// now get isolation mode from firewall plugin
	firewall := findPluginByName(conf.Plugins, "firewall")
	if firewall != nil {
		var firewallConf firewallConfig
		err := json.Unmarshal(firewall.Bytes, &firewallConf)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal the firewall plugin config in %s: %w", confPath, err)
		}
		if firewallConf.IngressPolicy == ingressPolicySameBridge {
			network.Options[types.IsolateOption] = "true"
		}
	}

	return &network, nil
}

func findPluginByName(plugins []*libcni.NetworkConfig, name string) *libcni.NetworkConfig {
	for i := range plugins {
		if plugins[i].Network.Type == name {
			return plugins[i]
		}
	}
	return nil
}

// convertIPAMConfToNetwork converts A cni IPAMConfig to libpod network subnets.
// It returns an array of subnets and an extra bool if dhcp is configured.
func convertIPAMConfToNetwork(network *types.Network, ipam *ipamConfig, confPath string) error {
	switch ipam.PluginType {
	case "":
		network.IPAMOptions[types.Driver] = types.NoneIPAMDriver
	case types.DHCPIPAMDriver:
		network.IPAMOptions[types.Driver] = types.DHCPIPAMDriver
	case types.HostLocalIPAMDriver:
		network.IPAMOptions[types.Driver] = types.HostLocalIPAMDriver
		for _, r := range ipam.Ranges {
			for _, ipam := range r {
				s := types.Subnet{}

				// Do not use types.ParseCIDR() because we want the ip to be
				// the network address and not a random ip in the sub.
				_, sub, err := net.ParseCIDR(ipam.Subnet)
				if err != nil {
					return err
				}
				s.Subnet = types.IPNet{IPNet: *sub}

				// gateway
				var gateway net.IP
				if ipam.Gateway != "" {
					gateway = net.ParseIP(ipam.Gateway)
					if gateway == nil {
						return fmt.Errorf("failed to parse gateway ip %s", ipam.Gateway)
					}
					// convert to 4 byte if ipv4
					util.NormalizeIP(&gateway)
				} else if !network.Internal {
					// only add a gateway address if the network is not internal
					gateway, err = util.FirstIPInSubnet(sub)
					if err != nil {
						return fmt.Errorf("failed to get first ip in subnet %s", sub.String())
					}
				}
				s.Gateway = gateway

				var rangeStart net.IP
				var rangeEnd net.IP
				if ipam.RangeStart != "" {
					rangeStart = net.ParseIP(ipam.RangeStart)
					if rangeStart == nil {
						return fmt.Errorf("failed to parse range start ip %s", ipam.RangeStart)
					}
				}
				if ipam.RangeEnd != "" {
					rangeEnd = net.ParseIP(ipam.RangeEnd)
					if rangeEnd == nil {
						return fmt.Errorf("failed to parse range end ip %s", ipam.RangeEnd)
					}
				}
				if rangeStart != nil || rangeEnd != nil {
					s.LeaseRange = &types.LeaseRange{}
					s.LeaseRange.StartIP = rangeStart
					s.LeaseRange.EndIP = rangeEnd
				}
				if util.IsIPv6(s.Subnet.IP) {
					network.IPv6Enabled = true
				}
				network.Subnets = append(network.Subnets, s)
			}
		}
	default:
		// This is not an error. While we only support certain ipam drivers, we
		// cannot make it fail for unsupported ones. CNI is still able to use them,
		// just our translation logic cannot convert this into a Network.
		// For the same reason this is not warning, it would just be annoying for
		// everyone using a unknown ipam driver.
		logrus.Infof("unsupported ipam plugin %q in %s", ipam.PluginType, confPath)
		network.IPAMOptions[types.Driver] = ipam.PluginType
	}
	return nil
}

// getNetworkArgsFromConfList returns the map of args in a conflist, argType should be labels or options.
func getNetworkArgsFromConfList(args map[string]any, argType string) map[string]string {
	if args, ok := args[argType]; ok {
		if labels, ok := args.(map[string]any); ok {
			result := make(map[string]string, len(labels))
			for k, v := range labels {
				if v, ok := v.(string); ok {
					result[k] = v
				}
			}
			return result
		}
	}
	return map[string]string{}
}

// createCNIConfigListFromNetwork will create a cni config file from the given network.
// It returns the cni config and the path to the file where the config was written.
// Set writeToDisk to false to only add this network into memory.
func (n *cniNetwork) createCNIConfigListFromNetwork(network *types.Network, writeToDisk bool) (*libcni.NetworkConfigList, string, error) {
	var (
		routes     []ipamRoute
		ipamRanges [][]ipamLocalHostRangeConf
		ipamConf   *ipamConfig
		err        error
	)

	ipamDriver := network.IPAMOptions[types.Driver]
	switch ipamDriver {
	case types.HostLocalIPAMDriver:
		defIpv4Route := false
		defIpv6Route := false
		for _, subnet := range network.Subnets {
			ipam := newIPAMLocalHostRange(subnet.Subnet, subnet.LeaseRange, subnet.Gateway)
			ipamRanges = append(ipamRanges, []ipamLocalHostRangeConf{*ipam})

			// only add default route for not internal networks
			if !network.Internal {
				ipv6 := util.IsIPv6(subnet.Subnet.IP)
				if !ipv6 && defIpv4Route {
					continue
				}
				if ipv6 && defIpv6Route {
					continue
				}

				if ipv6 {
					defIpv6Route = true
				} else {
					defIpv4Route = true
				}
				route, err := newIPAMDefaultRoute(ipv6)
				if err != nil {
					return nil, "", err
				}
				routes = append(routes, route)
			}
		}
		conf := newIPAMHostLocalConf(routes, ipamRanges)
		ipamConf = &conf
	case types.DHCPIPAMDriver:
		ipamConf = &ipamConfig{PluginType: "dhcp"}

	case types.NoneIPAMDriver:
		// do nothing
	default:
		return nil, "", fmt.Errorf("unsupported ipam driver %q", ipamDriver)
	}

	opts, err := parseOptions(network.Options, network.Driver)
	if err != nil {
		return nil, "", err
	}

	isGateway := true
	ipMasq := true
	if network.Internal {
		isGateway = false
		ipMasq = false
	}
	// create CNI plugin configuration
	// explicitly use CNI version 0.4.0 here, to use v1.0.0 at least containernetwork-plugins-1.0.1 has to be installed
	// the dnsname plugin also needs to be updated for 1.0.0
	// TODO change to 1.0.0 when most distros support it
	ncList := newNcList(network.Name, "0.4.0", network.Labels, network.Options)
	var plugins []any

	switch network.Driver {
	case types.BridgeNetworkDriver:
		bridge := newHostLocalBridge(network.NetworkInterface, isGateway, ipMasq, opts.mtu, opts.vlan, ipamConf)
		plugins = append(plugins, bridge, newPortMapPlugin(), newFirewallPlugin(opts.isolate), newTuningPlugin())
		// if we find the dnsname plugin we add configuration for it
		if hasDNSNamePlugin(n.cniPluginDirs) && network.DNSEnabled {
			// Note: in the future we might like to allow for dynamic domain names
			plugins = append(plugins, newDNSNamePlugin(defaultPodmanDomainName))
		}

	case types.MacVLANNetworkDriver:
		plugins = append(plugins, newVLANPlugin(types.MacVLANNetworkDriver, network.NetworkInterface, opts.vlanPluginMode, opts.mtu, ipamConf))

	case types.IPVLANNetworkDriver:
		plugins = append(plugins, newVLANPlugin(types.IPVLANNetworkDriver, network.NetworkInterface, opts.vlanPluginMode, opts.mtu, ipamConf))

	default:
		return nil, "", fmt.Errorf("driver %q is not supported by cni", network.Driver)
	}
	ncList["plugins"] = plugins
	b, err := json.MarshalIndent(ncList, "", "   ")
	if err != nil {
		return nil, "", err
	}
	cniPathName := ""
	if writeToDisk {
		if err := os.MkdirAll(n.cniConfigDir, 0o755); err != nil {
			return nil, "", err
		}
		cniPathName = filepath.Join(n.cniConfigDir, network.Name+".conflist")
		err = os.WriteFile(cniPathName, b, 0o644)
		if err != nil {
			return nil, "", err
		}
		t, err := fileTime(cniPathName)
		if err != nil {
			return nil, "", err
		}
		network.Created = t
	} else {
		network.Created = time.Now()
	}
	config, err := libcni.ConfListFromBytes(b)
	if err != nil {
		return nil, "", err
	}
	return config, cniPathName, nil
}

func convertSpecgenPortsToCNIPorts(ports []types.PortMapping) ([]cniPortMapEntry, error) {
	cniPorts := make([]cniPortMapEntry, 0, len(ports))
	for _, port := range ports {
		if port.Protocol == "" {
			return nil, errors.New("port protocol should not be empty")
		}
		for protocol := range strings.SplitSeq(port.Protocol, ",") {
			if !slices.Contains([]string{"tcp", "udp", "sctp"}, protocol) {
				return nil, fmt.Errorf("unknown port protocol %s", protocol)
			}
			cniPort := cniPortMapEntry{
				HostPort:      int(port.HostPort),
				ContainerPort: int(port.ContainerPort),
				HostIP:        port.HostIP,
				Protocol:      protocol,
			}
			cniPorts = append(cniPorts, cniPort)
			for i := 1; i < int(port.Range); i++ {
				cniPort := cniPortMapEntry{
					HostPort:      int(port.HostPort) + i,
					ContainerPort: int(port.ContainerPort) + i,
					HostIP:        port.HostIP,
					Protocol:      protocol,
				}
				cniPorts = append(cniPorts, cniPort)
			}
		}
	}
	return cniPorts, nil
}

func removeMachinePlugin(conf *libcni.NetworkConfigList) *libcni.NetworkConfigList {
	plugins := make([]*libcni.NetworkConfig, 0, len(conf.Plugins))
	for _, net := range conf.Plugins {
		if net.Network.Type != "podman-machine" {
			plugins = append(plugins, net)
		}
	}
	conf.Plugins = plugins
	return conf
}

type options struct {
	vlan           int
	mtu            int
	vlanPluginMode string
	isolate        bool
}

func parseOptions(networkOptions map[string]string, networkDriver string) (*options, error) {
	opt := &options{}
	var err error
	for k, v := range networkOptions {
		switch k {
		case types.MTUOption:
			opt.mtu, err = internalutil.ParseMTU(v)
			if err != nil {
				return nil, err
			}

		case types.VLANOption:
			opt.vlan, err = internalutil.ParseVlan(v)
			if err != nil {
				return nil, err
			}

		case types.ModeOption:
			switch networkDriver {
			case types.MacVLANNetworkDriver:
				if !slices.Contains(types.ValidMacVLANModes, v) {
					return nil, fmt.Errorf("unknown macvlan mode %q", v)
				}
			case types.IPVLANNetworkDriver:
				if !slices.Contains(types.ValidIPVLANModes, v) {
					return nil, fmt.Errorf("unknown ipvlan mode %q", v)
				}
			default:
				return nil, fmt.Errorf("cannot set option \"mode\" with driver %q", networkDriver)
			}
			opt.vlanPluginMode = v

		case types.IsolateOption:
			if networkDriver != types.BridgeNetworkDriver {
				return nil, errors.New("isolate option is only supported with the bridge driver")
			}
			opt.isolate, err = strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("failed to parse isolate option: %w", err)
			}

		default:
			return nil, fmt.Errorf("unsupported network option %s", k)
		}
	}
	return opt, nil
}

func fileTime(file string) (time.Time, error) {
	var st unix.Stat_t
	for {
		err := unix.Stat(file, &st)
		if err == nil {
			break
		}
		if err != unix.EINTR { //nolint:errorlint // unix errors are bare
			return time.Time{}, &os.PathError{Path: file, Op: "stat", Err: err}
		}
	}
	return time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec)), nil //nolint:unconvert // On some platforms Sec and Nsec are int32.
}
