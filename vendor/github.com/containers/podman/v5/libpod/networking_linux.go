//go:build !remote

package libpod

import (
	"fmt"
	"net"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/netns"
)

// Create and configure a new network namespace for a container
func (r *Runtime) configureNetNS(ctr *Container, ctrNS string) (status map[string]types.StatusBlock, rerr error) {
	if err := r.exposeMachinePorts(ctr.config.PortMappings); err != nil {
		return nil, err
	}
	defer func() {
		// make sure to unexpose the gvproxy ports when an error happens
		if rerr != nil {
			if err := r.unexposeMachinePorts(ctr.config.PortMappings); err != nil {
				logrus.Errorf("failed to free gvproxy machine ports: %v", err)
			}
		}
	}()
	if ctr.config.NetMode.IsSlirp4netns() {
		return nil, r.setupSlirp4netns(ctr, ctrNS)
	}
	if ctr.config.NetMode.IsPasta() {
		return nil, r.setupPasta(ctr, ctrNS)
	}
	networks, err := ctr.networks()
	if err != nil {
		return nil, err
	}
	// All networks have been removed from the container.
	// This is effectively forcing net=none.
	if len(networks) == 0 {
		return nil, nil
	}

	netOpts := ctr.getNetworkOptions(networks)
	netStatus, err := r.setUpNetwork(ctrNS, netOpts)
	if err != nil {
		return nil, err
	}
	defer func() {
		// do not forget to tear down the netns when a later error happened.
		if rerr != nil {
			if err := r.teardownNetworkBackend(ctrNS, netOpts); err != nil {
				logrus.Warnf("failed to teardown network after failed setup: %v", err)
			}
		}
	}()

	// set up rootless port forwarder when rootless with ports and the network status is empty,
	// if this is called from network reload the network status will not be empty and we should
	// not set up port because they are still active
	if rootless.IsRootless() && len(ctr.config.PortMappings) > 0 && ctr.getNetworkStatus() == nil {
		// set up port forwarder for rootless netns
		// TODO: support slirp4netns port forwarder as well
		// make sure to fix this in container.handleRestartPolicy() as well
		// Important we have to call this after r.setUpNetwork() so that
		// we can use the proper netStatus
		err = r.setupRootlessPortMappingViaRLK(ctr, ctrNS, netStatus)
	}
	return netStatus, err
}

// Create and configure a new network namespace for a container
func (r *Runtime) createNetNS(ctr *Container) (n string, q map[string]types.StatusBlock, retErr error) {
	ctrNS, err := netns.NewNS()
	if err != nil {
		return "", nil, fmt.Errorf("creating network namespace for container %s: %w", ctr.ID(), err)
	}
	defer func() {
		if retErr != nil {
			if err := netns.UnmountNS(ctrNS.Path()); err != nil {
				logrus.Errorf("Unmounting partially created network namespace for container %s: %v", ctr.ID(), err)
			}
			if err := ctrNS.Close(); err != nil {
				logrus.Errorf("Closing partially created network namespace for container %s: %v", ctr.ID(), err)
			}
		}
	}()

	logrus.Debugf("Made network namespace at %s for container %s", ctrNS.Path(), ctr.ID())

	var networkStatus map[string]types.StatusBlock
	networkStatus, err = r.configureNetNS(ctr, ctrNS.Path())
	return ctrNS.Path(), networkStatus, err
}

// Configure the network namespace using the container process
func (r *Runtime) setupNetNS(ctr *Container) error {
	nsProcess := fmt.Sprintf("/proc/%d/ns/net", ctr.state.PID)
	nsPath, err := netns.NewNSFrom(nsProcess)
	if err != nil {
		return err
	}

	networkStatus, err := r.configureNetNS(ctr, nsPath)

	// Assign NetNS attributes to container
	ctr.state.NetNS = nsPath
	ctr.state.NetworkStatus = networkStatus
	return err
}

// Tear down a network namespace, undoing all state associated with it.
func (r *Runtime) teardownNetNS(ctr *Container) error {
	if err := r.unexposeMachinePorts(ctr.config.PortMappings); err != nil {
		// do not return an error otherwise we would prevent network cleanup
		logrus.Errorf("failed to free gvproxy machine ports: %v", err)
	}

	// Do not check the error here, we want to always umount the netns
	// This will ensure that the container interface will be deleted
	// even when there is a CNI or netavark bug.
	prevErr := r.teardownNetwork(ctr)

	// First unmount the namespace
	if err := netns.UnmountNS(ctr.state.NetNS); err != nil {
		if prevErr != nil {
			logrus.Error(prevErr)
		}
		return fmt.Errorf("unmounting network namespace for container %s: %w", ctr.ID(), err)
	}

	ctr.state.NetNS = ""

	return prevErr
}

func getContainerNetNS(ctr *Container) (string, *Container, error) {
	if ctr.state.NetNS != "" {
		return ctr.state.NetNS, nil, nil
	}
	if ctr.config.NetNsCtr != "" {
		c, err := ctr.runtime.GetContainer(ctr.config.NetNsCtr)
		if err != nil {
			return "", nil, err
		}
		if err = c.syncContainer(); err != nil {
			return "", c, err
		}
		netNs, c2, err := getContainerNetNS(c)
		if c2 != nil {
			c = c2
		}
		return netNs, c, err
	}
	return "", nil, nil
}

// Returns a map of interface name to statistics for that interface.
func getContainerNetIO(ctr *Container) (map[string]define.ContainerNetworkStats, error) {
	perNetworkStats := make(map[string]define.ContainerNetworkStats)

	netNSPath, _, netPathErr := getContainerNetNS(ctr)
	if netPathErr != nil {
		return nil, netPathErr
	}
	if netNSPath == "" {
		// If netNSPath is empty, it was set as none, and no netNS was set up
		// this is a valid state and thus return no error, nor any statistics
		return nil, nil
	}

	err := ns.WithNetNSPath(netNSPath, func(_ ns.NetNS) error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("retrieving all network interfaces: %w", err)
		}
		for _, link := range links {
			attributes := link.Attrs()
			if attributes.Flags&net.FlagLoopback != 0 {
				continue
			}

			if attributes.Statistics != nil {
				perNetworkStats[attributes.Name] = getNetStatsFromNetlinkStats(attributes.Statistics)
			}
		}
		return nil
	})
	return perNetworkStats, err
}

func getNetStatsFromNetlinkStats(stats *netlink.LinkStatistics) define.ContainerNetworkStats {
	return define.ContainerNetworkStats{
		RxBytes:   stats.RxBytes,
		RxDropped: stats.RxDropped,
		RxErrors:  stats.RxErrors,
		RxPackets: stats.RxPackets,
		TxBytes:   stats.TxBytes,
		TxDropped: stats.TxDropped,
		TxErrors:  stats.TxErrors,
		TxPackets: stats.TxPackets,
	}
}

// joinedNetworkNSPath returns netns path and bool if netns was set
func (c *Container) joinedNetworkNSPath() (string, bool) {
	for _, namespace := range c.config.Spec.Linux.Namespaces {
		if namespace.Type == specs.NetworkNamespace {
			return namespace.Path, true
		}
	}
	return "", false
}

func (c *Container) inspectJoinedNetworkNS(networkns string) (q types.StatusBlock, retErr error) {
	var result types.StatusBlock
	err := ns.WithNetNSPath(networkns, func(_ ns.NetNS) error {
		ifaces, err := net.Interfaces()
		if err != nil {
			return err
		}
		routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
		if err != nil {
			return err
		}
		var gateway net.IP
		for _, route := range routes {
			// add default gateway
			// Dst is set to 0.0.0.0/0 or ::/0 which is the default route
			if route.Dst != nil && route.Dst.IP.IsUnspecified() {
				ones, _ := route.Dst.Mask.Size()
				if ones == 0 {
					gateway = route.Gw
				}
			}
		}
		result.Interfaces = make(map[string]types.NetInterface)
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			if len(addrs) == 0 {
				continue
			}
			subnets := make([]types.NetAddress, 0, len(addrs))
			for _, address := range addrs {
				if ipnet, ok := address.(*net.IPNet); ok {
					if ipnet.IP.IsLinkLocalMulticast() || ipnet.IP.IsLinkLocalUnicast() {
						continue
					}
					subnet := types.NetAddress{
						IPNet: types.IPNet{
							IPNet: *ipnet,
						},
					}
					if ipnet.Contains(gateway) {
						subnet.Gateway = gateway
					}
					subnets = append(subnets, subnet)
				}
			}
			result.Interfaces[iface.Name] = types.NetInterface{
				Subnets:    subnets,
				MacAddress: types.HardwareAddr(iface.HardwareAddr),
			}
		}
		return nil
	})
	return result, err
}
