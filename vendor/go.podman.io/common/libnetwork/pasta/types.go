package pasta

import "net"

const BinaryName = "pasta"

type SetupResult struct {
	// IpAddresses configured by pasta
	IPAddresses []net.IP
	// DNSForwardIP is the ip used in --dns-forward, it should be added as first
	// entry to resolv.conf in the container.
	DNSForwardIPs []string
	// MapGuestIps are the ips used for the --map-guest-addr option which
	// we can use for the host.containers.internal entry.
	MapGuestAddrIPs []string
	// IPv6 says whenever pasta run with ipv6 support
	IPv6 bool
}
