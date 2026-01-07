package util

import "net"

// getLiveNetworkSubnets returns a slice of subnets representing what the system
// has defined as network interfaces.
func getLiveNetworkSubnets() ([]*net.IPNet, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	nets := make([]*net.IPNet, 0, len(addrs))
	for _, address := range addrs {
		_, n, err := net.ParseCIDR(address.String())
		if err != nil {
			return nil, err
		}
		nets = append(nets, n)
	}
	return nets, nil
}

// GetLiveNetworkNames returns a list of network interface names on the system.
func GetLiveNetworkNames() ([]string, error) {
	liveInterfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	interfaceNames := make([]string, 0, len(liveInterfaces))
	for _, i := range liveInterfaces {
		interfaceNames = append(interfaceNames, i.Name)
	}
	return interfaceNames, nil
}
