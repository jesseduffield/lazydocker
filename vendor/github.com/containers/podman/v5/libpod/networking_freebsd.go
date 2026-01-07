//go:build !remote

package libpod

import (
	"crypto/rand"
	jdec "encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"

	"github.com/containers/buildah/pkg/jail"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
)

type Netstat struct {
	Statistics NetstatInterface `json:"statistics"`
}

type NetstatInterface struct {
	Interface []NetstatAddress `json:"interface"`
}

type NetstatAddress struct {
	Name    string `json:"name"`
	Flags   string `json:"flags"`
	Mtu     int    `json:"mtu"`
	Network string `json:"network"`
	Address string `json:"address"`

	ReceivedPackets uint64 `json:"received-packets"`
	ReceivedBytes   uint64 `json:"received-bytes"`
	ReceivedErrors  uint64 `json:"received-errors"`

	SentPackets uint64 `json:"sent-packets"`
	SentBytes   uint64 `json:"sent-bytes"`
	SentErrors  uint64 `json:"send-errors"`

	DroppedPackets uint64 `json:"dropped-packets"`

	Collisions uint64 `json:"collisions"`
}

func getSlirp4netnsIP(_ *net.IPNet) (*net.IP, error) {
	return nil, errors.New("not implemented GetSlirp4netnsIP")
}

// This is called after the container's jail is created but before its
// started. We can use this to initialise the container's vnet when we don't
// have a separate vnet jail (which is the case in FreeBSD 13.3 and later).
func (r *Runtime) setupNetNS(ctr *Container) error {
	networkStatus, err := r.configureNetNS(ctr, ctr.ID())
	ctr.state.NetNS = ctr.ID()
	ctr.state.NetworkStatus = networkStatus
	return err
}

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

	return netStatus, err
}

// Create and configure a new network namespace for a container
func (r *Runtime) createNetNS(ctr *Container) (n string, q map[string]types.StatusBlock, retErr error) {
	b := make([]byte, 16)
	_, err := rand.Reader.Read(b)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate random vnet name: %v", err)
	}
	netns := fmt.Sprintf("vnet-%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])

	jconf := jail.NewConfig()
	jconf.Set("name", netns)
	jconf.Set("vnet", jail.NEW)
	jconf.Set("children.max", 1)
	jconf.Set("persist", true)
	jconf.Set("enforce_statfs", 0)
	jconf.Set("devfs_ruleset", 4)
	jconf.Set("allow.raw_sockets", true)
	jconf.Set("allow.chflags", true)
	jconf.Set("securelevel", -1)
	j, err := jail.Create(jconf)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create vnet jail %s for container %s: %w", netns, ctr.ID(), err)
	}

	logrus.Debugf("Created vnet jail %s for container %s", netns, ctr.ID())

	var networkStatus map[string]types.StatusBlock
	networkStatus, err = r.configureNetNS(ctr, netns)
	if err != nil {
		jconf := jail.NewConfig()
		jconf.Set("persist", false)
		if err := j.Set(jconf); err != nil {
			// Log this error and return the error from configureNetNS
			logrus.Errorf("failed to destroy vnet jail %s: %v", netns, err)
		}
	}
	return netns, networkStatus, err
}

// Tear down a network namespace, undoing all state associated with it.
func (r *Runtime) teardownNetNS(ctr *Container) error {
	if err := r.unexposeMachinePorts(ctr.config.PortMappings); err != nil {
		// do not return an error otherwise we would prevent network cleanup
		logrus.Errorf("failed to free gvproxy machine ports: %v", err)
	}
	if err := r.teardownNetwork(ctr); err != nil {
		return err
	}

	if ctr.state.NetNS != "" {
		// If PostConfigureNetNS is false, then we are running with a
		// separate vnet jail so we need to clean that up now.
		if !ctr.config.PostConfigureNetNS {
			// Rather than destroying the jail immediately, reset the
			// persist flag so that it will live until the container is
			// done.
			netjail, err := jail.FindByName(ctr.state.NetNS)
			if err != nil {
				return fmt.Errorf("finding network jail %s: %w", ctr.state.NetNS, err)
			}
			jconf := jail.NewConfig()
			jconf.Set("persist", false)
			if err := netjail.Set(jconf); err != nil {
				return fmt.Errorf("releasing network jail %s: %w", ctr.state.NetNS, err)
			}
		}
		ctr.state.NetNS = ""
	}
	return nil
}

// TODO (5.0): return the statistics per network interface
// This would allow better compat with docker.
func getContainerNetIO(ctr *Container) (map[string]define.ContainerNetworkStats, error) {
	if ctr.state.NetNS == "" {
		// If NetNS is nil, it was set as none, and no netNS
		// was set up this is a valid state and thus return no
		// error, nor any statistics
		return nil, nil
	}

	// First try running 'netstat -j' - this lets us retrieve stats from
	// containers which don't have a separate vnet jail.
	cmd := exec.Command("netstat", "-j", ctr.state.NetNS, "-bi", "--libxo", "json")
	out, err := cmd.Output()
	if err != nil {
		// Fall back to using jexec so that this still works on 13.2
		// which does not have the -j flag.
		cmd := exec.Command("jexec", ctr.state.NetNS, "netstat", "-bi", "--libxo", "json")
		out, err = cmd.Output()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read network stats: %v", err)
	}
	stats := Netstat{}
	if err := jdec.Unmarshal(out, &stats); err != nil {
		return nil, err
	}

	res := make(map[string]define.ContainerNetworkStats)

	// Sum all the interface stats - in practice only Tx/TxBytes are needed
	for _, ifaddr := range stats.Statistics.Interface {
		// Each interface has two records, one for link-layer which has
		// an MTU field and one for IP which doesn't. We only want the
		// link-layer stats.
		//
		// It's not clear if we should include loopback stats here but
		// if we move to per-interface stats in future, this can be
		// reported separately.
		if ifaddr.Mtu > 0 {
			linkStats := define.ContainerNetworkStats{
				RxPackets: ifaddr.ReceivedPackets,
				TxPackets: ifaddr.SentPackets,
				RxBytes:   ifaddr.ReceivedBytes,
				TxBytes:   ifaddr.SentBytes,
				RxErrors:  ifaddr.ReceivedErrors,
				TxErrors:  ifaddr.SentErrors,
				RxDropped: ifaddr.DroppedPackets,
			}
			res[ifaddr.Name] = linkStats
		}
	}

	return res, nil
}

func (c *Container) joinedNetworkNSPath() (string, bool) {
	return c.state.NetNS, false
}

func (c *Container) inspectJoinedNetworkNS(_ string) (q types.StatusBlock, retErr error) {
	// TODO: extract interface information from the vnet jail
	return types.StatusBlock{}, nil
}

func (c *Container) reloadRootlessRLKPortMapping() error {
	return errors.New("unsupported (*Container).reloadRootlessRLKPortMapping")
}
