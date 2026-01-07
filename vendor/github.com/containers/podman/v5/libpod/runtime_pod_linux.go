//go:build !remote

package libpod

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/rootless"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/common/pkg/config"
)

func (r *Runtime) platformMakePod(pod *Pod, resourceLimits *spec.LinuxResources) (string, error) {
	cgroupParent := ""
	// Check Cgroup parent sanity, and set it if it was not set
	if r.config.Cgroups() != "disabled" {
		switch r.config.Engine.CgroupManager {
		case config.CgroupfsCgroupsManager:
			canUseCgroup := !rootless.IsRootless() || isRootlessCgroupSet(pod.config.CgroupParent)
			if canUseCgroup {
				// need to actually create parent here
				if pod.config.CgroupParent == "" {
					pod.config.CgroupParent = CgroupfsDefaultCgroupParent
				} else if strings.HasSuffix(path.Base(pod.config.CgroupParent), ".slice") {
					return "", fmt.Errorf("systemd slice received as cgroup parent when using cgroupfs: %w", define.ErrInvalidArg)
				}
				// If we are set to use pod cgroups, set the cgroup parent that
				// all containers in the pod will share
				if pod.config.UsePodCgroup {
					pod.state.CgroupPath = filepath.Join(pod.config.CgroupParent, pod.ID())
					cgroupParent = pod.state.CgroupPath
					// cgroupfs + rootless = permission denied when creating the cgroup.
					if !rootless.IsRootless() {
						res, err := GetLimits(resourceLimits)
						if err != nil {
							return "", err
						}
						res.SkipDevices = true
						// Need to both create and update the cgroup
						// rather than create a new path in c/common for pod cgroup creation
						// just create as if it is a ctr and then update figures out that we need to
						// populate the resource limits on the pod level
						cgc, err := cgroups.New(pod.state.CgroupPath, &res)
						if err != nil {
							return "", err
						}
						err = cgc.Update(&res)
						if err != nil {
							return "", err
						}
					}
				}
			}
		case config.SystemdCgroupsManager:
			if pod.config.CgroupParent == "" {
				if rootless.IsRootless() {
					pod.config.CgroupParent = SystemdDefaultRootlessCgroupParent
				} else {
					pod.config.CgroupParent = SystemdDefaultCgroupParent
				}
			} else if len(pod.config.CgroupParent) < 6 || !strings.HasSuffix(path.Base(pod.config.CgroupParent), ".slice") {
				return "", fmt.Errorf("did not receive systemd slice as cgroup parent when using systemd to manage cgroups: %w", define.ErrInvalidArg)
			}
			// If we are set to use pod cgroups, set the cgroup parent that
			// all containers in the pod will share
			if pod.config.UsePodCgroup {
				cgroupPath, err := systemdSliceFromPath(pod.config.CgroupParent, fmt.Sprintf("libpod_pod_%s", pod.ID()), resourceLimits)
				if err != nil {
					return "", fmt.Errorf("unable to create pod cgroup for pod %s: %w", pod.ID(), err)
				}
				pod.state.CgroupPath = cgroupPath
				cgroupParent = pod.state.CgroupPath
			}
		default:
			return "", fmt.Errorf("unsupported Cgroup manager: %s - cannot validate cgroup parent: %w", r.config.Engine.CgroupManager, define.ErrInvalidArg)
		}
	}

	if pod.config.UsePodCgroup {
		logrus.Debugf("Got pod cgroup as %s", pod.state.CgroupPath)
	}

	return cgroupParent, nil
}

func (p *Pod) removePodCgroup() error {
	// Remove pod cgroup, if present
	if p.state.CgroupPath == "" {
		return nil
	}
	logrus.Debugf("Removing pod cgroup %s", p.state.CgroupPath)

	cgroup, err := cgroups.GetOwnCgroup()
	if err != nil {
		return err
	}

	// if we are trying to delete a cgroup that is our ancestor, we need to move the
	// current process out of it before the cgroup is destroyed.
	if isSubDir(cgroup, string(filepath.Separator)+p.state.CgroupPath) {
		parent := path.Dir(p.state.CgroupPath)
		if err := cgroups.MoveUnderCgroup(parent, "cleanup", nil); err != nil {
			return err
		}
	}

	switch p.runtime.config.Engine.CgroupManager {
	case config.SystemdCgroupsManager:
		if err := deleteSystemdCgroup(p.state.CgroupPath, p.ResourceLim()); err != nil {
			return fmt.Errorf("removing pod %s cgroup: %w", p.ID(), err)
		}
	case config.CgroupfsCgroupsManager:
		// Delete the cgroupfs cgroup
		// Make sure the conmon cgroup is deleted first
		// Since the pod is almost gone, don't bother failing
		// hard - instead, just log errors.
		conmonCgroupPath := filepath.Join(p.state.CgroupPath, "conmon")
		conmonCgroup, err := cgroups.Load(conmonCgroupPath)
		if err != nil && err != cgroups.ErrCgroupDeleted && err != cgroups.ErrCgroupV1Rootless {
			return fmt.Errorf("retrieving pod %s conmon cgroup: %w", p.ID(), err)
		}
		if err == nil {
			if err = conmonCgroup.Delete(); err != nil {
				return fmt.Errorf("removing pod %s conmon cgroup: %w", p.ID(), err)
			}
		}
		cgroup, err := cgroups.Load(p.state.CgroupPath)
		if err != nil && err != cgroups.ErrCgroupDeleted && err != cgroups.ErrCgroupV1Rootless {
			return fmt.Errorf("retrieving pod %s cgroup: %w", p.ID(), err)
		}
		if err == nil {
			if err := cgroup.Delete(); err != nil {
				return fmt.Errorf("removing pod %s cgroup: %w", p.ID(), err)
			}
		}
	default:
		// This should be caught much earlier, but let's still
		// keep going so we make sure to evict the pod before
		// ending up with an inconsistent state.
		return fmt.Errorf("unrecognized cgroup manager %s when removing pod %s cgroups: %w", p.runtime.config.Engine.CgroupManager, p.ID(), define.ErrInternal)
	}
	return nil
}
