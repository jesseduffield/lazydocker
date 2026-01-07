package slirp4netns

import "net"

const (
	BinaryName = "slirp4netns"
)

// SetupResult return type from Setup().
type SetupResult struct {
	// Pid of the created slirp4netns process
	Pid int
	// Subnet which is used by slirp4netns
	Subnet *net.IPNet
	// IPv6 whenever Ipv6 is enabled in slirp4netns
	IPv6 bool
}
