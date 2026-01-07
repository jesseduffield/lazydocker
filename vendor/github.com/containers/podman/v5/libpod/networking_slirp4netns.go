//go:build !remote && linux

package libpod

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/containers/podman/v5/pkg/errorhandling"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/slirp4netns"
	"go.podman.io/common/libnetwork/types"
)

// setupSlirp4netns can be called in rootful as well as in rootless
func (r *Runtime) setupSlirp4netns(ctr *Container, netns string) error {
	ports := ctr.convertPortMappings()

	if !ctr.config.PostConfigureNetNS {
		var err error
		ctr.rootlessSlirpSyncR, ctr.rootlessSlirpSyncW, err = os.Pipe()
		if err != nil {
			return fmt.Errorf("failed to create rootless network sync pipe: %w", err)
		}
		if len(ports) > 0 {
			ctr.rootlessPortSyncR, ctr.rootlessPortSyncW, err = os.Pipe()
			if err != nil {
				return fmt.Errorf("failed to create rootless port sync pipe: %w", err)
			}
		}
	}
	defer errorhandling.CloseQuiet(ctr.rootlessSlirpSyncR)
	if ctr.rootlessPortSyncR != nil {
		defer errorhandling.CloseQuiet(ctr.rootlessPortSyncR)
	}

	res, err := slirp4netns.Setup(&slirp4netns.SetupOptions{
		Config:                r.config,
		ContainerID:           ctr.ID(),
		Netns:                 netns,
		Ports:                 ports,
		ExtraOptions:          ctr.config.NetworkOptions[slirp4netns.BinaryName],
		Slirp4netnsExitPipeR:  ctr.rootlessSlirpSyncR,
		RootlessPortExitPipeR: ctr.rootlessPortSyncR,
	})
	if err != nil {
		return err
	}
	ctr.slirp4netnsSubnet = res.Subnet
	return nil
}

func (r *Runtime) setupRootlessPortMappingViaRLK(ctr *Container, netnsPath string, netStatus map[string]types.StatusBlock) error {
	var err error
	if !ctr.config.PostConfigureNetNS {
		ctr.rootlessPortSyncR, ctr.rootlessPortSyncW, err = os.Pipe()
		if err != nil {
			return fmt.Errorf("failed to create rootless port sync pipe: %w", err)
		}
	}
	defer errorhandling.CloseQuiet(ctr.rootlessPortSyncR)
	return slirp4netns.SetupRootlessPortMappingViaRLK(&slirp4netns.SetupOptions{
		Config:                r.config,
		ContainerID:           ctr.ID(),
		Netns:                 netnsPath,
		Ports:                 ctr.convertPortMappings(),
		RootlessPortExitPipeR: ctr.rootlessPortSyncR,
	}, nil, netStatus)
}

// reloadRootlessRLKPortMapping will trigger a reload for the port mappings in the rootlessport process.
// This should only be called by network connect/disconnect and only as rootless.
func (c *Container) reloadRootlessRLKPortMapping() error {
	if len(c.config.PortMappings) == 0 {
		return nil
	}
	childIP := slirp4netns.GetRootlessPortChildIP(nil, c.state.NetworkStatus)
	logrus.Debugf("reloading rootless ports for container %s, childIP is %s", c.config.ID, childIP)

	conn, err := openUnixSocket(filepath.Join(c.runtime.config.Engine.TmpDir, "rp", c.config.ID))
	if err != nil {
		return fmt.Errorf("could not reload rootless port mappings, port forwarding may no longer work correctly: %w", err)
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	err = enc.Encode(childIP)
	if err != nil {
		return fmt.Errorf("port reloading failed: %w", err)
	}
	b, err := io.ReadAll(conn)
	if err != nil {
		return fmt.Errorf("port reloading failed: %w", err)
	}
	data := string(b)
	if data != "OK" {
		return fmt.Errorf("port reloading failed: %s", data)
	}
	return nil
}

func getSlirp4netnsIP(subnet *net.IPNet) (*net.IP, error) {
	return slirp4netns.GetIP(subnet)
}
