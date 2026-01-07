//go:build (linux || freebsd) && cni

package cni

import (
	"net"
	"path/filepath"

	"go.podman.io/common/libnetwork/types"
	"go.podman.io/storage/pkg/fileutils"
)

const (
	defaultIPv4Route = "0.0.0.0/0"
	defaultIPv6Route = "::/0"
	// defaultPodmanDomainName is used for the dnsname plugin to define
	// a localized domain name for a created network.
	defaultPodmanDomainName = "dns.podman"

	// cniDeviceName is the default name for a new bridge, it should be suffixed with an integer.
	cniDeviceName = "cni-podman"

	// podmanLabelKey key used to store the podman network label in a cni config.
	podmanLabelKey = "podman_labels"

	// podmanOptionsKey key used to store the podman network options in a cni config.
	podmanOptionsKey = "podman_options"

	// ingressPolicySameBridge is used to only allow connection on the same bridge network.
	ingressPolicySameBridge = "same-bridge"
)

// cniPortMapEntry struct is used by the portmap plugin
// https://github.com/containernetworking/plugins/blob/649e0181fe7b3a61e708f3e4249a798f57f25cc5/plugins/meta/portmap/main.go#L43-L50
type cniPortMapEntry struct {
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"hostIP,omitempty"`
}

// hostLocalBridge describes a configuration for a bridge plugin
// https://github.com/containernetworking/plugins/tree/master/plugins/main/bridge#network-configuration-reference
type hostLocalBridge struct {
	PluginType   string          `json:"type"`
	BrName       string          `json:"bridge,omitempty"`
	IsGW         bool            `json:"isGateway"`
	IsDefaultGW  bool            `json:"isDefaultGateway,omitempty"`
	ForceAddress bool            `json:"forceAddress,omitempty"`
	IPMasq       bool            `json:"ipMasq,omitempty"`
	MTU          int             `json:"mtu,omitempty"`
	HairpinMode  bool            `json:"hairpinMode,omitempty"`
	PromiscMode  bool            `json:"promiscMode,omitempty"`
	Vlan         int             `json:"vlan,omitempty"`
	IPAM         ipamConfig      `json:"ipam"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

// ipamConfig describes an IPAM configuration
// https://github.com/containernetworking/plugins/tree/master/plugins/ipam/host-local#network-configuration-reference
type ipamConfig struct {
	PluginType  string                     `json:"type"`
	Routes      []ipamRoute                `json:"routes,omitempty"`
	ResolveConf string                     `json:"resolveConf,omitempty"`
	DataDir     string                     `json:"dataDir,omitempty"`
	Ranges      [][]ipamLocalHostRangeConf `json:"ranges,omitempty"`
}

// ipamLocalHostRangeConf describes the new style IPAM ranges.
type ipamLocalHostRangeConf struct {
	Subnet     string `json:"subnet"`
	RangeStart string `json:"rangeStart,omitempty"`
	RangeEnd   string `json:"rangeEnd,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
}

// ipamRoute describes a route in an ipam config.
type ipamRoute struct {
	Dest string `json:"dst"`
}

// portMapConfig describes the default portmapping config.
type portMapConfig struct {
	PluginType   string          `json:"type"`
	Capabilities map[string]bool `json:"capabilities"`
}

// VLANConfig describes the macvlan config.
type VLANConfig struct {
	PluginType   string          `json:"type"`
	Master       string          `json:"master"`
	IPAM         ipamConfig      `json:"ipam"`
	MTU          int             `json:"mtu,omitempty"`
	Mode         string          `json:"mode,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

// firewallConfig describes the firewall plugin.
type firewallConfig struct {
	PluginType    string `json:"type"`
	Backend       string `json:"backend"`
	IngressPolicy string `json:"ingressPolicy,omitempty"`
}

// tuningConfig describes the tuning plugin.
type tuningConfig struct {
	PluginType string `json:"type"`
}

// dnsNameConfig describes the dns container name resolution plugin config.
type dnsNameConfig struct {
	PluginType   string          `json:"type"`
	DomainName   string          `json:"domainName"`
	Capabilities map[string]bool `json:"capabilities"`
}

// ncList describes a generic map.
type ncList map[string]any

// newNcList creates a generic map of values with string
// keys and adds in version and network name.
func newNcList(name, version string, labels, options map[string]string) ncList {
	n := ncList{}
	n["cniVersion"] = version
	n["name"] = name
	args := map[string]map[string]string{}
	if len(labels) > 0 {
		args[podmanLabelKey] = labels
	}
	if len(options) > 0 {
		args[podmanOptionsKey] = options
	}
	if len(args) > 0 {
		n["args"] = args
	}
	return n
}

// newHostLocalBridge creates a new LocalBridge for host-local.
func newHostLocalBridge(name string, isGateWay, ipMasq bool, mtu, vlan int, ipamConf *ipamConfig) *hostLocalBridge {
	bridge := hostLocalBridge{
		PluginType:  "bridge",
		BrName:      name,
		IsGW:        isGateWay,
		IPMasq:      ipMasq,
		MTU:         mtu,
		HairpinMode: true,
		Vlan:        vlan,
	}
	if ipamConf != nil {
		bridge.IPAM = *ipamConf
		// if we use host-local set the ips cap to ensure we can set static ips via runtime config
		if ipamConf.PluginType == types.HostLocalIPAMDriver {
			bridge.Capabilities = map[string]bool{"ips": true}
		}
	}
	return &bridge
}

// newIPAMHostLocalConf creates a new IPAMHostLocal configuration.
func newIPAMHostLocalConf(routes []ipamRoute, ipamRanges [][]ipamLocalHostRangeConf) ipamConfig {
	ipamConf := ipamConfig{
		PluginType: "host-local",
		Routes:     routes,
	}

	ipamConf.Ranges = ipamRanges
	return ipamConf
}

// newIPAMLocalHostRange create a new IPAM range.
func newIPAMLocalHostRange(subnet types.IPNet, leaseRange *types.LeaseRange, gw net.IP) *ipamLocalHostRangeConf {
	hostRange := &ipamLocalHostRangeConf{
		Subnet: subnet.String(),
	}

	// a user provided a range, we add it here
	if leaseRange != nil {
		if leaseRange.StartIP != nil {
			hostRange.RangeStart = leaseRange.StartIP.String()
		}
		if leaseRange.EndIP != nil {
			hostRange.RangeEnd = leaseRange.EndIP.String()
		}
	}

	if gw != nil {
		hostRange.Gateway = gw.String()
	}
	return hostRange
}

// newIPAMRoute creates a new IPAM route configuration
// nolint:interfacer
func newIPAMRoute(r *net.IPNet) ipamRoute {
	return ipamRoute{Dest: r.String()}
}

// newIPAMDefaultRoute creates a new IPAMDefault route of
// 0.0.0.0/0 for IPv4 or ::/0 for IPv6.
func newIPAMDefaultRoute(isIPv6 bool) (ipamRoute, error) {
	route := defaultIPv4Route
	if isIPv6 {
		route = defaultIPv6Route
	}
	_, n, err := net.ParseCIDR(route)
	if err != nil {
		return ipamRoute{}, err
	}
	return newIPAMRoute(n), nil
}

// newPortMapPlugin creates a predefined, default portmapping
// configuration.
func newPortMapPlugin() portMapConfig {
	return portMapConfig{
		PluginType:   "portmap",
		Capabilities: map[string]bool{"portMappings": true},
	}
}

// newFirewallPlugin creates a generic firewall plugin.
func newFirewallPlugin(isolate bool) firewallConfig {
	fw := firewallConfig{
		PluginType: "firewall",
	}
	if isolate {
		fw.IngressPolicy = ingressPolicySameBridge
	}
	return fw
}

// newTuningPlugin creates a generic tuning section.
func newTuningPlugin() tuningConfig {
	return tuningConfig{
		PluginType: "tuning",
	}
}

// newDNSNamePlugin creates the dnsname config with a given
// domainname.
func newDNSNamePlugin(domainName string) dnsNameConfig {
	return dnsNameConfig{
		PluginType:   "dnsname",
		DomainName:   domainName,
		Capabilities: map[string]bool{"aliases": true},
	}
}

// hasDNSNamePlugin looks to see if the dnsname cni plugin is present.
func hasDNSNamePlugin(paths []string) bool {
	for _, p := range paths {
		if err := fileutils.Exists(filepath.Join(p, "dnsname")); err == nil {
			return true
		}
	}
	return false
}

// newVLANPlugin creates a macvlanconfig with a given device name.
func newVLANPlugin(pluginType, device, mode string, mtu int, ipam *ipamConfig) VLANConfig {
	m := VLANConfig{
		PluginType: pluginType,
	}
	if ipam != nil {
		m.IPAM = *ipam
	}
	if mtu > 0 {
		m.MTU = mtu
	}
	if len(mode) > 0 {
		m.Mode = mode
	}
	// CNI is supposed to use the default route if a
	// parent device is not provided
	if len(device) > 0 {
		m.Master = device
	}
	caps := make(map[string]bool)
	caps["ips"] = true
	// if we use host-local set the ips cap to ensure we can set static ips via runtime config
	if m.IPAM.PluginType == types.HostLocalIPAMDriver {
		m.Capabilities = caps
	}
	return m
}
