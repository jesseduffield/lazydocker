package util

import (
	"fmt"
	"net"
	"slices"

	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/libnetwork/util"
	"go.podman.io/common/pkg/config"
)

func CreateBridge(n NetUtil, network *types.Network, usedNetworks []*net.IPNet, subnetPools []config.SubnetPool, checkBridgeConflict bool) error {
	if network.NetworkInterface != "" {
		if checkBridgeConflict {
			bridges := GetBridgeInterfaceNames(n)
			if slices.Contains(bridges, network.NetworkInterface) {
				return fmt.Errorf("bridge name %s already in use", network.NetworkInterface)
			}
		}
		if !types.NameRegex.MatchString(network.NetworkInterface) {
			return fmt.Errorf("bridge name %s invalid: %w", network.NetworkInterface, types.ErrInvalidName)
		}
	} else {
		var err error
		network.NetworkInterface, err = GetFreeDeviceName(n)
		if err != nil {
			return err
		}
	}

	ipamDriver := network.IPAMOptions[types.Driver]
	// also do this when the driver is unset
	if ipamDriver == "" || ipamDriver == types.HostLocalIPAMDriver {
		if len(network.Subnets) == 0 {
			freeSubnet, err := GetFreeIPv4NetworkSubnet(usedNetworks, subnetPools)
			if err != nil {
				return err
			}
			network.Subnets = append(network.Subnets, *freeSubnet)
		}
		// ipv6 enabled means dual stack, check if we already have
		// a ipv4 or ipv6 subnet and add one if not.
		if network.IPv6Enabled {
			ipv4 := false
			ipv6 := false
			for _, subnet := range network.Subnets {
				if util.IsIPv6(subnet.Subnet.IP) {
					ipv6 = true
				}
				if util.IsIPv4(subnet.Subnet.IP) {
					ipv4 = true
				}
			}
			if !ipv4 {
				freeSubnet, err := GetFreeIPv4NetworkSubnet(usedNetworks, subnetPools)
				if err != nil {
					return err
				}
				network.Subnets = append(network.Subnets, *freeSubnet)
			}
			if !ipv6 {
				freeSubnet, err := GetFreeIPv6NetworkSubnet(usedNetworks)
				if err != nil {
					return err
				}
				network.Subnets = append(network.Subnets, *freeSubnet)
			}
		}
		network.IPAMOptions[types.Driver] = types.HostLocalIPAMDriver
	}
	return nil
}
