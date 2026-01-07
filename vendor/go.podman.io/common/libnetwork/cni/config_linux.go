//go:build (linux || freebsd) && cni

package cni

import (
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

func deleteLink(name string) {
	link, err := netlink.LinkByName(name)
	if err == nil {
		err = netlink.LinkDel(link)
		// only log the error, it is not fatal
		if err != nil {
			logrus.Infof("Failed to remove network interface %s: %v", name, err)
		}
	}
}
