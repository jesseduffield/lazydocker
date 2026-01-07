//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// A service consists of one or more pods.  The service container is started
// before all pods and is stopped when the last pod stops. The service
// container allows for tracking and managing the entire life cycle of service
// which may be started via `podman-play-kube`.
type Service struct {
	// Pods running as part of the service.
	Pods []string `json:"servicePods"`
}

// Indicates whether the pod is associated with a service container.
// The pod is expected to be updated and locked.
func (p *Pod) hasServiceContainer() bool {
	return p.config.ServiceContainerID != ""
}

// Returns the pod's service container.
// The pod is expected to be updated and locked.
func (p *Pod) serviceContainer() (*Container, error) {
	id := p.config.ServiceContainerID
	if id == "" {
		return nil, fmt.Errorf("pod has no service container: %w", define.ErrNoSuchCtr)
	}
	return p.runtime.state.Container(id)
}

// ServiceContainer returns the service container.
func (p *Pod) ServiceContainer() (*Container, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if err := p.updatePod(); err != nil {
		return nil, err
	}
	return p.serviceContainer()
}

func (c *Container) addServicePodLocked(id string) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if err := c.syncContainer(); err != nil {
		return err
	}
	c.state.Service.Pods = append(c.state.Service.Pods, id)
	return c.save()
}

// IsService returns true when the container is a "service container".
func (c *Container) IsService() bool {
	return c.config.IsService
}

// serviceContainerReport bundles information when checking whether a service
// container can be stopped.
type serviceContainerReport struct {
	// Indicates whether the service container can be stopped or not.
	canBeStopped bool
	// Number of all known containers below the service container.
	numContainers int
	// Number of containers below the service containers that exited
	// non-zero.
	failedContainers int
}

// canStopServiceContainerLocked returns true if all pods of the service are stopped.
// Note that the method acquires the container lock.
func (c *Container) canStopServiceContainerLocked() (*serviceContainerReport, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if err := c.syncContainer(); err != nil {
		return nil, err
	}

	if !c.IsService() {
		return nil, fmt.Errorf("internal error: checking service: container %s is not a service container", c.ID())
	}

	return c.canStopServiceContainer()
}

// canStopServiceContainer returns true if all pods of the service are stopped.
// Note that the method expects the container to be locked.
func (c *Container) canStopServiceContainer() (*serviceContainerReport, error) {
	report := serviceContainerReport{canBeStopped: true}
	for _, id := range c.state.Service.Pods {
		pod, err := c.runtime.LookupPod(id)
		if err != nil {
			if errors.Is(err, define.ErrNoSuchPod) {
				continue
			}
			return nil, err
		}

		status, err := pod.GetPodStatus()
		if err != nil {
			if errors.Is(err, define.ErrNoSuchPod) {
				continue
			}
			return nil, err
		}

		switch status {
		case define.PodStateStopped, define.PodStateExited, define.PodStateErrored:
			podCtrs, err := c.runtime.state.PodContainers(pod)
			if err != nil {
				return nil, err
			}
			for _, pc := range podCtrs {
				if pc.IsInfra() {
					continue // ignore infra containers
				}
				exitCode, err := c.runtime.state.GetContainerExitCode(pc.ID())
				if err != nil {
					return nil, err
				}
				if exitCode != 0 {
					report.failedContainers++
				}
				report.numContainers++
			}
		default:
			// Service container cannot be stopped, so we can
			// return early.
			report.canBeStopped = false
			return &report, nil
		}
	}

	return &report, nil
}

// Checks whether the service container can be stopped and does so.
func (p *Pod) maybeStopServiceContainer() error {
	if !p.hasServiceContainer() {
		return nil
	}

	serviceCtr, err := p.serviceContainer()
	if err != nil {
		if errors.Is(err, define.ErrNoSuchCtr) {
			return nil
		}
		return fmt.Errorf("getting pod's service container: %w", err)
	}
	// Checking whether the service can be stopped must be done in
	// the runtime's work queue to resolve ABBA dead locks in the
	// pod->container->servicePods hierarchy.
	p.runtime.queueWork(func() {
		logrus.Debugf("Pod %s has a service %s: checking if it can be stopped", p.ID(), serviceCtr.ID())
		report, err := serviceCtr.canStopServiceContainerLocked()
		if err != nil {
			logrus.Errorf("Checking whether service of container %s can be stopped: %v", serviceCtr.ID(), err)
			return
		}
		if !report.canBeStopped {
			return
		}

		// Now either kill or stop the service container, depending on the configured exit policy.
		stop := func() {
			// Note that the service container runs catatonit which
			// will exit gracefully on SIGINT.
			logrus.Debugf("Stopping service container %s", serviceCtr.ID())
			if err := serviceCtr.Kill(uint(unix.SIGINT)); err != nil && !errors.Is(err, define.ErrCtrStateInvalid) {
				logrus.Debugf("Error stopping service container %s: %v", serviceCtr.ID(), err)
			}
		}

		kill := func() {
			logrus.Debugf("Killing service container %s", serviceCtr.ID())
			if err := serviceCtr.Kill(uint(unix.SIGKILL)); err != nil && !errors.Is(err, define.ErrCtrStateInvalid) {
				logrus.Debugf("Error killing service container %s: %v", serviceCtr.ID(), err)
			}
		}

		switch serviceCtr.config.KubeExitCodePropagation {
		case define.KubeExitCodePropagationNone:
			stop()
		case define.KubeExitCodePropagationAny:
			if report.failedContainers > 0 {
				kill()
			} else {
				stop()
			}
		case define.KubeExitCodePropagationAll:
			if report.failedContainers == report.numContainers {
				kill()
			} else {
				stop()
			}
		default:
			logrus.Errorf("Internal error: cannot stop service container %s: unknown exit policy %q", serviceCtr.ID(), serviceCtr.config.KubeExitCodePropagation.String())
		}
	})
	return nil
}

// Starts the pod's service container if it's not already running.
func (p *Pod) maybeStartServiceContainer(ctx context.Context) error {
	if !p.hasServiceContainer() {
		return nil
	}

	serviceCtr, err := p.serviceContainer()
	if err != nil {
		return fmt.Errorf("getting pod's service container: %w", err)
	}

	serviceCtr.lock.Lock()
	defer serviceCtr.lock.Unlock()

	if err := serviceCtr.syncContainer(); err != nil {
		return err
	}

	if serviceCtr.state.State == define.ContainerStateRunning {
		return nil
	}

	// Restart will reinit among other things.
	return serviceCtr.restartWithTimeout(ctx, 0)
}

// canRemoveServiceContainer returns true if all pods of the service are removed.
// Note that the method acquires the container lock.
func (c *Container) canRemoveServiceContainer() (bool, error) {
	if !c.IsService() {
		return false, fmt.Errorf("internal error: checking service: container %s is not a service container", c.ID())
	}

	for _, id := range c.state.Service.Pods {
		if _, err := c.runtime.LookupPod(id); err != nil {
			if errors.Is(err, define.ErrNoSuchPod) {
				continue
			}
			return false, err
		}
		return false, nil
	}

	return true, nil
}

// Checks whether the service container can be removed and does so.
// It also unlinks the pod from the service container.
func (p *Pod) maybeRemoveServiceContainer() error {
	if !p.hasServiceContainer() {
		return nil
	}

	serviceCtr, err := p.serviceContainer()
	if err != nil {
		if errors.Is(err, define.ErrNoSuchCtr) {
			return nil
		}
		return fmt.Errorf("getting pod's service container: %w", err)
	}
	// Checking whether the service can be stopped must be done in
	// the runtime's work queue to resolve ABBA dead locks in the
	// pod->container->servicePods hierarchy.
	p.runtime.queueWork(func() {
		logrus.Debugf("Pod %s has a service %s: checking if it can be removed", p.ID(), serviceCtr.ID())
		canRemove, err := func() (bool, error) { // Anonymous func for easy locking
			serviceCtr.lock.Lock()
			defer serviceCtr.lock.Unlock()
			if err := serviceCtr.syncContainer(); err != nil {
				return false, err
			}

			// Unlink the pod from the service container.
			servicePods := make([]string, 0, len(serviceCtr.state.Service.Pods)-1)
			for _, id := range serviceCtr.state.Service.Pods {
				if id != p.ID() {
					servicePods = append(servicePods, id)
				}
			}
			serviceCtr.state.Service.Pods = servicePods
			if err := serviceCtr.save(); err != nil {
				return false, err
			}

			return serviceCtr.canRemoveServiceContainer()
		}()
		if err != nil {
			if !errors.Is(err, define.ErrNoSuchCtr) {
				logrus.Errorf("Checking whether service container %s can be removed: %v", serviceCtr.ID(), err)
			}
			return
		}
		if !canRemove {
			return
		}
		logrus.Debugf("Removing service container %s", serviceCtr.ID())
		if err := p.runtime.RemoveContainer(context.Background(), serviceCtr, true, false, nil); err != nil {
			if !errors.Is(err, define.ErrNoSuchCtr) {
				logrus.Errorf("Removing service container %s: %v", serviceCtr.ID(), err)
			}
		}
	})
	return nil
}
