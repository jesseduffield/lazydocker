package types

import (
	"encoding/json"
	"net"
	"time"
)

type ContainerNetwork interface {
	// NetworkCreate will take a partial filled Network and fill the
	// missing fields. It creates the Network and returns the full Network.
	NetworkCreate(Network, *NetworkCreateOptions) (Network, error)
	// NetworkUpdate will take network name and ID and updates network DNS Servers.
	NetworkUpdate(nameOrID string, options NetworkUpdateOptions) error
	// NetworkRemove will remove the Network with the given name or ID.
	NetworkRemove(nameOrID string) error
	// NetworkList will return all known Networks. Optionally you can
	// supply a list of filter functions. Only if a network matches all
	// functions it is returned.
	NetworkList(...FilterFunc) ([]Network, error)
	// NetworkInspect will return the Network with the given name or ID.
	NetworkInspect(nameOrID string) (Network, error)

	// Setup will setup the container network namespace. It returns
	// a map of StatusBlocks, the key is the network name.
	Setup(namespacePath string, options SetupOptions) (map[string]StatusBlock, error)
	// Teardown will teardown the container network namespace.
	Teardown(namespacePath string, options TeardownOptions) error

	// RunInRootlessNetns is used to run the given function in the rootless netns.
	// Only used as rootless and should return an error as root.
	RunInRootlessNetns(toRun func() error) error

	// RootlessNetnsInfo return extra information about the rootless netns.
	// Only valid when called after Setup().
	// Only used as rootless and should return an error as root.
	RootlessNetnsInfo() (*RootlessNetnsInfo, error)

	// Drivers will return the list of supported network drivers
	// for this interface.
	Drivers() []string

	// DefaultNetworkName will return the default network name
	// for this interface.
	DefaultNetworkName() string

	// NetworkInfo return the network information about backend type,
	// binary path, package version and so on.
	NetworkInfo() NetworkInfo
}

// Network describes the Network attributes.
type Network struct {
	// Name of the Network.
	Name string `json:"name"`
	// ID of the Network.
	ID string `json:"id"`
	// Driver for this Network, e.g. bridge, macvlan...
	Driver string `json:"driver"`
	// NetworkInterface is the network interface name on the host.
	NetworkInterface string `json:"network_interface,omitempty"`
	// Created contains the timestamp when this network was created.
	Created time.Time `json:"created,omitempty"`
	// Subnets to use for this network.
	Subnets []Subnet `json:"subnets,omitempty"`
	// Routes to use for this network.
	Routes []Route `json:"routes,omitempty"`
	// IPv6Enabled if set to true an ipv6 subnet should be created for this net.
	IPv6Enabled bool `json:"ipv6_enabled"`
	// Internal is whether the Network should not have external routes
	// to public or other Networks.
	Internal bool `json:"internal"`
	// DNSEnabled is whether name resolution is active for container on
	// this Network. Only supported with the bridge driver.
	DNSEnabled bool `json:"dns_enabled"`
	// List of custom DNS server for podman's DNS resolver at network level,
	// all the containers attached to this network will consider resolvers
	// configured at network level.
	NetworkDNSServers []string `json:"network_dns_servers,omitempty"`
	// Labels is a set of key-value labels that have been applied to the
	// Network.
	Labels map[string]string `json:"labels,omitempty"`
	// Options is a set of key-value options that have been applied to
	// the Network.
	Options map[string]string `json:"options,omitempty"`
	// IPAMOptions contains options used for the ip assignment.
	IPAMOptions map[string]string `json:"ipam_options,omitempty"`
}

// NetworkUpdateOptions for a given container.
type NetworkUpdateOptions struct {
	// List of custom DNS server for podman's DNS resolver.
	// Priority order will be kept as defined by user in the configuration.
	AddDNSServers    []string `json:"add_dns_servers,omitempty"`
	RemoveDNSServers []string `json:"remove_dns_servers,omitempty"`
}

// NetworkInfo contains the network information.
type NetworkInfo struct {
	Backend NetworkBackend `json:"backend"`
	Version string         `json:"version,omitempty"`
	Package string         `json:"package,omitempty"`
	Path    string         `json:"path,omitempty"`
	DNS     DNSNetworkInfo `json:"dns,omitempty"`
}

// DNSNetworkInfo contains the DNS information.
type DNSNetworkInfo struct {
	Version string `json:"version,omitempty"`
	Package string `json:"package,omitempty"`
	Path    string `json:"path,omitempty"`
}

// IPNet is used as custom net.IPNet type to add Marshal/Unmarshal methods.
type IPNet struct {
	net.IPNet
}

// ParseCIDR parse a string to IPNet.
func ParseCIDR(cidr string) (IPNet, error) {
	ip, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return IPNet{}, err
	}
	// convert to 4 bytes if ipv4
	ipv4 := ip.To4()
	if ipv4 != nil {
		ip = ipv4
	}
	subnet.IP = ip
	return IPNet{*subnet}, err
}

func (n *IPNet) MarshalText() ([]byte, error) {
	return []byte(n.String()), nil
}

func (n *IPNet) UnmarshalText(text []byte) error {
	subnet, err := ParseCIDR(string(text))
	if err != nil {
		return err
	}
	*n = subnet
	return nil
}

// HardwareAddr is the same as net.HardwareAddr except
// that it adds the json marshal/unmarshal methods.
// This allows us to read the mac from a json string
// and a byte array.
// swagger:model MacAddress
type HardwareAddr net.HardwareAddr

func (h *HardwareAddr) String() string {
	return (*net.HardwareAddr)(h).String()
}

func (h HardwareAddr) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

func (h *HardwareAddr) UnmarshalJSON(text []byte) error {
	if len(text) == 0 {
		*h = nil
		return nil
	}

	// if the json string start with a quote we got a string
	// unmarshal the string and parse the mac from this string
	if string(text[0]) == `"` {
		var macString string
		err := json.Unmarshal(text, &macString)
		if err == nil {
			mac, err := net.ParseMAC(macString)
			if err == nil {
				*h = HardwareAddr(mac)
				return nil
			}
		}
	}
	// not a string or got an error fallback to the normal parsing
	mac := make(net.HardwareAddr, 0, 6)
	// use the standard json unmarshal for backwards compat
	err := json.Unmarshal(text, &mac)
	if err != nil {
		return err
	}
	*h = HardwareAddr(mac)
	return nil
}

type Subnet struct {
	// Subnet for this Network in CIDR form.
	// swagger:strfmt string
	Subnet IPNet `json:"subnet"`
	// Gateway IP for this Network.
	// swagger:strfmt string
	Gateway net.IP `json:"gateway,omitempty"`
	// LeaseRange contains the range where IP are leased. Optional.
	LeaseRange *LeaseRange `json:"lease_range,omitempty"`
}

type Route struct {
	// Destination for this route in CIDR form.
	// swagger:strfmt string
	Destination IPNet `json:"destination"`
	// Gateway IP for this route.
	// swagger:strfmt string
	Gateway net.IP `json:"gateway"`
	// Metric for this route. Optional.
	Metric *uint32 `json:"metric,omitempty"`
}

// LeaseRange contains the range where IP are leased.
type LeaseRange struct {
	// StartIP first IP in the subnet which should be used to assign ips.
	// swagger:strfmt string
	StartIP net.IP `json:"start_ip,omitempty"`
	// EndIP last IP in the subnet which should be used to assign ips.
	// swagger:strfmt string
	EndIP net.IP `json:"end_ip,omitempty"`
}

// StatusBlock contains the network information about a container
// connected to one Network.
type StatusBlock struct {
	// Interfaces contains the created network interface in the container.
	// The map key is the interface name.
	Interfaces map[string]NetInterface `json:"interfaces,omitempty"`
	// DNSServerIPs nameserver addresses which should be added to
	// the containers resolv.conf file.
	DNSServerIPs []net.IP `json:"dns_server_ips,omitempty"`
	// DNSSearchDomains search domains which should be added to
	// the containers resolv.conf file.
	DNSSearchDomains []string `json:"dns_search_domains,omitempty"`
}

// NetInterface contains the settings for a given network interface.
type NetInterface struct {
	// Subnets list of assigned subnets with their gateway.
	Subnets []NetAddress `json:"subnets,omitempty"`
	// MacAddress for this Interface.
	MacAddress HardwareAddr `json:"mac_address"`
}

// NetAddress contains the ip address, subnet and gateway.
type NetAddress struct {
	// IPNet of this NetAddress. Note that this is a subnet but it has to contain the
	// actual ip of the network interface and not the network address.
	IPNet IPNet `json:"ipnet"`
	// Gateway for the network. This can be empty if there is no gateway, e.g. internal network.
	Gateway net.IP `json:"gateway,omitempty"`
}

// PerNetworkOptions are options which should be set on a per network basis.
type PerNetworkOptions struct {
	// StaticIPs for this container. Optional.
	// swagger:type []string
	StaticIPs []net.IP `json:"static_ips,omitempty"`
	// Aliases contains a list of names which the dns server should resolve
	// to this container. Should only be set when DNSEnabled is true on the Network.
	// If aliases are set but there is no dns support for this network the
	// network interface implementation should ignore this and NOT error.
	// Optional.
	Aliases []string `json:"aliases,omitempty"`
	// StaticMac for this container. Optional.
	// swagger:strfmt string
	StaticMAC HardwareAddr `json:"static_mac,omitempty"`
	// InterfaceName for this container. Required in the backend.
	// Optional in the frontend. Will be filled with ethX (where X is a integer) when empty.
	InterfaceName string `json:"interface_name"`
	// Driver-specific options for this container.
	Options map[string]string `json:"options,omitempty"`
}

// NetworkOptions for a given container.
type NetworkOptions struct {
	// ContainerID is the container id, used for iptables comments and ipam allocation.
	ContainerID string `json:"container_id"`
	// ContainerName is the container name.
	ContainerName string `json:"container_name"`
	// PortMappings contains the port mappings for this container
	PortMappings []PortMapping `json:"port_mappings,omitempty"`
	// Networks contains all networks with the PerNetworkOptions.
	// The map should contain at least one element.
	Networks map[string]PerNetworkOptions `json:"networks"`
	// List of custom DNS server for podman's DNS resolver.
	// Priority order will be kept as defined by user in the configuration.
	DNSServers []string `json:"dns_servers,omitempty"`
	// ContainerHostname is the configured DNS hostname of the container.
	ContainerHostname string `json:"container_hostname"`
}

// PortMapping is one or more ports that will be mapped into the container.
type PortMapping struct {
	// HostIP is the IP that we will bind to on the host.
	// If unset, assumed to be 0.0.0.0 (all interfaces).
	HostIP string `json:"host_ip"`
	// ContainerPort is the port number that will be exposed from the
	// container.
	// Mandatory.
	ContainerPort uint16 `json:"container_port"`
	// HostPort is the port number that will be forwarded from the host into
	// the container.
	// If omitted, a random port on the host (guaranteed to be over 1024)
	// will be assigned.
	HostPort uint16 `json:"host_port"`
	// Range is the number of ports that will be forwarded, starting at
	// HostPort and ContainerPort and counting up.
	// This is 1-indexed, so 1 is assumed to be a single port (only the
	// Hostport:Containerport mapping will be added), 2 is two ports (both
	// Hostport:Containerport and Hostport+1:Containerport+1), etc.
	// If unset, assumed to be 1 (a single port).
	// Both hostport + range and containerport + range must be less than
	// 65536.
	Range uint16 `json:"range"`
	// Protocol is the protocol forward.
	// Must be either "tcp", "udp", and "sctp", or some combination of these
	// separated by commas.
	// If unset, assumed to be TCP.
	Protocol string `json:"protocol"`
}

// OCICNIPortMapping maps to the standard CNI portmapping Capability.
// Deprecated: Do not use this struct for new fields. This only exists
// for backwards compatibility.
type OCICNIPortMapping struct {
	// HostPort is the port number on the host.
	HostPort int32 `json:"hostPort"`
	// ContainerPort is the port number inside the sandbox.
	ContainerPort int32 `json:"containerPort"`
	// Protocol is the protocol of the port mapping.
	Protocol string `json:"protocol"`
	// HostIP is the host ip to use.
	HostIP string `json:"hostIP"`
}

type SetupOptions struct {
	NetworkOptions
}

type TeardownOptions struct {
	NetworkOptions
}

type RootlessNetnsInfo struct {
	// IPAddresses used in the netns, must not be used for host.containers.internal
	IPAddresses []net.IP
	// DnsForwardIps ips used in resolv.conf
	//nolint:staticcheck //It wants this to be named DNSForwardIps but this would be a breaking change and thus is not worth it.
	DnsForwardIps []string
	// MapGuestIps should be used for the host.containers.internal entry when set
	MapGuestIps []string
}

// FilterFunc can be passed to NetworkList to filter the networks.
type FilterFunc func(Network) bool

type NetworkCreateOptions struct {
	// IgnoreIfExists if true, do not fail if the network already exists
	IgnoreIfExists bool
}
