//go:build !remote

package libpod

import (
	"fmt"
	"path/filepath"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
)

func (p *Pod) platformRefresh() error {
	// We need to recreate the pod's cgroup
	if p.config.UsePodCgroup {
		switch p.runtime.config.Engine.CgroupManager {
		case config.SystemdCgroupsManager:
			cgroupPath, err := systemdSliceFromPath(p.config.CgroupParent, fmt.Sprintf("libpod_pod_%s", p.ID()), p.ResourceLim())
			if err != nil {
				logrus.Errorf("Creating Cgroup for pod %s: %v", p.ID(), err)
			}
			p.state.CgroupPath = cgroupPath
		case config.CgroupfsCgroupsManager:
			if !rootless.IsRootless() || isRootlessCgroupSet(p.config.CgroupParent) {
				p.state.CgroupPath = filepath.Join(p.config.CgroupParent, p.ID())

				logrus.Debugf("setting pod cgroup to %s", p.state.CgroupPath)
			}
		default:
			return fmt.Errorf("unknown cgroups manager %s specified: %w", p.runtime.config.Engine.CgroupManager, define.ErrInvalidArg)
		}
	}
	return nil
}
