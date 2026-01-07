package etchosts

import (
	"net"
	"sync"

	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/libnetwork/util"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/machine"
	"go.podman.io/storage/pkg/unshare"
)

// HostContainersInternalOptions contains the options for GetHostContainersInternalIP().
type HostContainersInternalOptions struct {
	// Conf is the containers.Conf, must not be nil
	Conf *config.Config
	// NetStatus is the network status for the container,
	// if this is set networkInterface must not be nil
	NetStatus map[string]types.StatusBlock
	// NetworkInterface of the current runtime
	NetworkInterface types.ContainerNetwork
	// Exclude are then ips that should not be returned, this is
	// useful to prevent returning the same ip as in the container.
	Exclude []net.IP
	// PreferIP is a ip that should be used if set but it has a
	// lower priority than the containers.conf config option.
	// This is used for the pasta --map-guest-addr ip.
	PreferIP string
}

// Lookup "host.containers.internal" dns name so we can add it to /etc/hosts when running inside podman machine.
var machineHostContainersInternalIP = sync.OnceValue(func() string {
	var errMsg string
	addrs, err := net.LookupIP(HostContainersInternal)
	if err == nil {
		if len(addrs) > 0 {
			return addrs[0].String()
		}
		errMsg = "lookup result is empty"
	} else {
		errMsg = err.Error()
	}
	logrus.Warnf("Failed to resolve %s for the host entry ip address: %s", HostContainersInternal, errMsg)
	return ""
})

// GetHostContainersInternalIP returns the host.containers.internal ip.
func GetHostContainersInternalIP(opts HostContainersInternalOptions) string {
	switch opts.Conf.Containers.HostContainersInternalIP {
	case "":
		// If empty (default) we will automatically choose one below.
		// If machine using gvproxy we let the gvproxy dns server handle resolve the name and then use that ip.
		if machine.IsGvProxyBased() {
			return machineHostContainersInternalIP()
		}
	case "none":
		return ""
	default:
		return opts.Conf.Containers.HostContainersInternalIP
	}

	// caller has a specific ip it prefers
	if opts.PreferIP != "" {
		return opts.PreferIP
	}

	ip := ""
	// Only use the bridge ip when root, as rootless the interfaces are created
	// inside the special netns and not the host so we cannot use them.
	if unshare.IsRootless() {
		return util.GetLocalIPExcluding(opts.Exclude)
	}
	for net, status := range opts.NetStatus {
		network, err := opts.NetworkInterface.NetworkInspect(net)
		// only add the host entry for bridge networks
		// ip/macvlan gateway is normally not on the host
		if err != nil || network.Driver != types.BridgeNetworkDriver {
			continue
		}
		for _, netInt := range status.Interfaces {
			for _, netAddress := range netInt.Subnets {
				if netAddress.Gateway != nil {
					if util.IsIPv4(netAddress.Gateway) {
						return netAddress.Gateway.String()
					}
					// ipv6 address but keep looking since we prefer to use ipv4
					ip = netAddress.Gateway.String()
				}
			}
		}
	}
	if ip != "" {
		return ip
	}
	return util.GetLocalIPExcluding(opts.Exclude)
}

// GetHostContainersInternalIPExcluding returns the host.containers.internal ip
// Exclude are ips that should not be returned, this is useful to prevent returning the same ip as in the container.
// if netStatus is not nil then networkInterface also must be non nil otherwise this function panics.
func GetHostContainersInternalIPExcluding(conf *config.Config, netStatus map[string]types.StatusBlock, networkInterface types.ContainerNetwork, exclude []net.IP) string {
	return GetHostContainersInternalIP(HostContainersInternalOptions{
		Conf:             conf,
		NetStatus:        netStatus,
		NetworkInterface: networkInterface,
		Exclude:          exclude,
	})
}

// GetNetworkHostEntries returns HostEntries for all ips in the network status
// with the given hostnames.
func GetNetworkHostEntries(netStatus map[string]types.StatusBlock, names ...string) HostEntries {
	hostEntries := make(HostEntries, 0, len(netStatus))
	for _, status := range netStatus {
		for _, netInt := range status.Interfaces {
			for _, netAddress := range netInt.Subnets {
				e := HostEntry{IP: netAddress.IPNet.IP.String(), Names: names}
				hostEntries = append(hostEntries, e)
			}
		}
	}
	return hostEntries
}
