package util

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode"

	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/libnetwork/util"
)

// ValidateSubnet will validate a given Subnet. It checks if the
// given gateway and lease range are part of this subnet. If the
// gateway is empty and addGateway is true it will get the first
// available ip in the subnet assigned.
func ValidateSubnet(s *types.Subnet, addGateway bool, usedNetworks []*net.IPNet) error {
	if s == nil {
		return errors.New("subnet is nil")
	}
	if s.Subnet.IP == nil {
		return errors.New("subnet ip is nil")
	}

	// Reparse to ensure subnet is valid.
	// Do not use types.ParseCIDR() because we want the ip to be
	// the network address and not a random ip in the subnet.
	_, n, err := net.ParseCIDR(s.Subnet.String())
	if err != nil {
		return fmt.Errorf("subnet invalid: %w", err)
	}

	// check that the new subnet does not conflict with existing ones
	if NetworkIntersectsWithNetworks(n, usedNetworks) {
		return fmt.Errorf("subnet %s is already used on the host or by another config", n.String())
	}

	s.Subnet = types.IPNet{IPNet: *n}
	if s.Gateway != nil {
		if !s.Subnet.Contains(s.Gateway) {
			return fmt.Errorf("gateway %s not in subnet %s", s.Gateway, &s.Subnet)
		}
		util.NormalizeIP(&s.Gateway)
	} else if addGateway {
		ip, err := util.FirstIPInSubnet(n)
		if err != nil {
			return err
		}
		s.Gateway = ip
	}

	if s.LeaseRange != nil {
		if s.LeaseRange.StartIP != nil {
			if !s.Subnet.Contains(s.LeaseRange.StartIP) {
				return fmt.Errorf("lease range start ip %s not in subnet %s", s.LeaseRange.StartIP, &s.Subnet)
			}
			util.NormalizeIP(&s.LeaseRange.StartIP)
		}
		if s.LeaseRange.EndIP != nil {
			if !s.Subnet.Contains(s.LeaseRange.EndIP) {
				return fmt.Errorf("lease range end ip %s not in subnet %s", s.LeaseRange.EndIP, &s.Subnet)
			}
			util.NormalizeIP(&s.LeaseRange.EndIP)
		}
	}
	return nil
}

// ValidateSubnets will validate the subnets for this network.
// It also sets the gateway if the gateway is empty and addGateway is set to true
// IPv6Enabled to true if at least one subnet is ipv6.
func ValidateSubnets(network *types.Network, addGateway bool, usedNetworks []*net.IPNet) error {
	for i := range network.Subnets {
		err := ValidateSubnet(&network.Subnets[i], addGateway, usedNetworks)
		if err != nil {
			return err
		}
		if util.IsIPv6(network.Subnets[i].Subnet.IP) {
			network.IPv6Enabled = true
		}
	}
	return nil
}

func ValidateRoutes(routes []types.Route) error {
	for _, route := range routes {
		err := ValidateRoute(route)
		if err != nil {
			return err
		}
	}
	return nil
}

func ValidateRoute(route types.Route) error {
	if route.Destination.IP == nil {
		return errors.New("route destination ip nil")
	}

	if route.Destination.Mask == nil {
		return errors.New("route destination mask nil")
	}

	if route.Gateway == nil {
		return errors.New("route gateway nil")
	}

	// Reparse to ensure destination is valid.
	ip, ipNet, err := net.ParseCIDR(route.Destination.String())
	if err != nil {
		return fmt.Errorf("route destination invalid: %w", err)
	}

	// check that destination is a network and not an address
	if !ip.Equal(ipNet.IP) {
		return errors.New("route destination invalid")
	}

	return nil
}

func ValidateSetupOptions(n NetUtil, namespacePath string, options types.SetupOptions) error {
	if namespacePath == "" {
		return errors.New("namespacePath is empty")
	}
	if options.ContainerID == "" {
		return errors.New("ContainerID is empty")
	}
	if len(options.Networks) == 0 {
		return errors.New("must specify at least one network")
	}
	for name, netOpts := range options.Networks {
		network, err := n.Network(name)
		if err != nil {
			return err
		}
		err = validatePerNetworkOpts(network, &netOpts)
		if err != nil {
			return err
		}
	}
	return nil
}

// validatePerNetworkOpts checks that all given static ips are in a subnet on this network.
func validatePerNetworkOpts(network *types.Network, netOpts *types.PerNetworkOptions) error {
	if netOpts.InterfaceName == "" {
		return fmt.Errorf("interface name on network %s is empty", network.Name)
	}
	if network.IPAMOptions[types.Driver] == types.HostLocalIPAMDriver {
	outer:
		for _, ip := range netOpts.StaticIPs {
			for _, s := range network.Subnets {
				if s.Subnet.Contains(ip) {
					continue outer
				}
			}
			return fmt.Errorf("requested static ip %s not in any subnet on network %s", ip.String(), network.Name)
		}
	}
	return nil
}

// ValidateInterfaceName validates the interface name based on the following rules:
// 1. The name must be less than MaxInterfaceNameLength characters
// 2. The name must not be "." or ".."
// 3. The name must not contain / or : or any whitespace characters
// ref to https://github.com/torvalds/linux/blob/81e4f8d68c66da301bb881862735bd74c6241a19/include/uapi/linux/if.h#L33C18-L33C20
func ValidateInterfaceName(ifName string) error {
	if len(ifName) > types.MaxInterfaceNameLength {
		return fmt.Errorf("interface name is too long: interface names must be %d characters or less: %w", types.MaxInterfaceNameLength, types.ErrInvalidArg)
	}
	if ifName == "." || ifName == ".." {
		return fmt.Errorf("interface name is . or ..: %w", types.ErrInvalidArg)
	}
	if strings.ContainsFunc(ifName, func(r rune) bool {
		return r == '/' || r == ':' || unicode.IsSpace(r)
	}) {
		return fmt.Errorf("interface name contains / or : or whitespace characters: %w", types.ErrInvalidArg)
	}
	return nil
}
