package cni

import (
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

func setupLoopback(namespacePath string) error {
	// set the loopback adapter up in the container netns
	return ns.WithNetNSPath(namespacePath, func(_ ns.NetNS) error {
		link, err := netlink.LinkByName("lo")
		if err == nil {
			err = netlink.LinkSetUp(link)
		}
		return err
	})
}
