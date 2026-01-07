package slirp4netns

const (
	ipv6ConfDefaultAcceptDadSysctl = "/proc/sys/net/ipv6/conf/default/accept_dad"

	// defaultMTU the default MTU override.
	defaultMTU = 65520

	// default slirp4ns subnet.
	defaultSubnet = "10.0.2.0/24"
)
