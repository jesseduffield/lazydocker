package util

import (
	"errors"
	"fmt"
	"net"
	"slices"

	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
)

// GetBridgeInterfaceNames returns all bridge interface names
// already used by network configs.
func GetBridgeInterfaceNames(n NetUtil) []string {
	names := make([]string, 0, n.Len())
	n.ForEach(func(net types.Network) {
		if net.Driver == types.BridgeNetworkDriver {
			names = append(names, net.NetworkInterface)
		}
	})
	return names
}

// GetUsedNetworkNames returns all network names already used
// by network configs.
func GetUsedNetworkNames(n NetUtil) []string {
	names := make([]string, 0, n.Len())
	n.ForEach(func(net types.Network) {
		names = append(names, net.Name)
	})
	return names
}

// GetFreeDeviceName returns a free device name which can
// be used for new configs as name and bridge interface name.
// The base name is suffixed by a number.
func GetFreeDeviceName(n NetUtil) (string, error) {
	bridgeNames := GetBridgeInterfaceNames(n)
	netNames := GetUsedNetworkNames(n)
	liveInterfaces, err := GetLiveNetworkNames()
	if err != nil {
		return "", nil
	}
	names := make([]string, 0, len(bridgeNames)+len(netNames)+len(liveInterfaces))
	names = append(names, bridgeNames...)
	names = append(names, netNames...)
	names = append(names, liveInterfaces...)
	// FIXME: Is a limit fine?
	// Start by 1, 0 is reserved for the default network
	for i := 1; i < 1000000; i++ {
		deviceName := fmt.Sprintf("%s%d", n.DefaultInterfaceName(), i)
		if !slices.Contains(names, deviceName) {
			logrus.Debugf("found free device name %s", deviceName)
			return deviceName, nil
		}
	}
	return "", errors.New("could not find free device name, to many iterations")
}

// GetUsedSubnets returns a list of all used subnets by network
// configs and interfaces on the host.
func GetUsedSubnets(n NetUtil) ([]*net.IPNet, error) {
	// first, load all used subnets from network configs
	subnets := make([]*net.IPNet, 0, n.Len())
	n.ForEach(func(n types.Network) {
		for i := range n.Subnets {
			subnets = append(subnets, &n.Subnets[i].Subnet.IPNet)
		}
	})
	// second, load networks from the current system
	liveSubnets, err := getLiveNetworkSubnets()
	if err != nil {
		return nil, err
	}
	return append(subnets, liveSubnets...), nil
}

// GetFreeIPv4NetworkSubnet returns a unused ipv4 subnet.
func GetFreeIPv4NetworkSubnet(usedNetworks []*net.IPNet, subnetPools []config.SubnetPool) (*types.Subnet, error) {
	var err error
	for _, pool := range subnetPools {
		// make sure to copy the netip to prevent overwriting the subnet pool
		netIP := make(net.IP, net.IPv4len)
		copy(netIP, pool.Base.IP)
		network := &net.IPNet{
			IP:   netIP,
			Mask: net.CIDRMask(pool.Size, 32),
		}
		for pool.Base.Contains(network.IP) {
			if !NetworkIntersectsWithNetworks(network, usedNetworks) {
				logrus.Debugf("found free ipv4 network subnet %s", network.String())
				return &types.Subnet{
					Subnet: types.IPNet{IPNet: *network},
				}, nil
			}
			network, err = NextSubnet(network)
			if err != nil {
				// when error go to next pool, we return the error only when all pools are done
				break
			}
		}
	}

	if err != nil {
		return nil, err
	}
	return nil, errors.New("could not find free subnet from subnet pools")
}

// GetFreeIPv6NetworkSubnet returns a unused ipv6 subnet.
func GetFreeIPv6NetworkSubnet(usedNetworks []*net.IPNet) (*types.Subnet, error) {
	// FIXME: Is 10000 fine as limit? We should prevent an endless loop.
	for range 10000 {
		// RFC4193: Choose the ipv6 subnet random and NOT sequentially.
		network, err := getRandomIPv6Subnet()
		if err != nil {
			return nil, err
		}
		if intersectsConfig := NetworkIntersectsWithNetworks(&network, usedNetworks); !intersectsConfig {
			logrus.Debugf("found free ipv6 network subnet %s", network.String())
			return &types.Subnet{
				Subnet: types.IPNet{IPNet: network},
			}, nil
		}
	}
	return nil, errors.New("failed to get random ipv6 subnet")
}

// MapDockerBridgeDriverOptions docker driver network options to podman network options.
func MapDockerBridgeDriverOptions(n *types.Network) {
	// validate the given options
	for key, value := range n.Options {
		switch key {
		case "com.docker.network.driver.mtu":
			n.Options[types.MTUOption] = value
			delete(n.Options, "com.docker.network.driver.mtu")

		case "com.docker.network.bridge.name":
			n.NetworkInterface = value
			delete(n.Options, "com.docker.network.bridge.name")
		}
	}
}
