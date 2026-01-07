//go:build !remote && (linux || freebsd)

package libpod

import (
	"context"
	"errors"
	"fmt"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

// NewPod makes a new, empty pod
func (r *Runtime) NewPod(_ context.Context, p specgen.PodSpecGenerator, options ...PodCreateOption) (_ *Pod, deferredErr error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	pod := newPod(r)

	// Set default namespace to runtime's namespace
	// Do so before options run so they can override it
	if r.config.Engine.Namespace != "" {
		pod.config.Namespace = r.config.Engine.Namespace
	}

	for _, option := range options {
		if err := option(pod); err != nil {
			return nil, fmt.Errorf("running pod create option: %w", err)
		}
	}

	// Allocate a lock for the pod
	lock, err := r.lockManager.AllocateLock()
	if err != nil {
		return nil, fmt.Errorf("allocating lock for new pod: %w", err)
	}
	pod.lock = lock
	pod.config.LockID = pod.lock.ID()

	defer func() {
		if deferredErr != nil {
			if err := pod.lock.Free(); err != nil {
				logrus.Errorf("Freeing pod lock after failed creation: %v", err)
			}
		}
	}()

	pod.valid = true

	parentCgroup, err := r.platformMakePod(pod, p.ResourceLimits)
	if err != nil {
		return nil, err
	}
	if p.InfraContainerSpec != nil {
		p.InfraContainerSpec.CgroupParent = parentCgroup
	}

	if !pod.HasInfraContainer() && pod.SharesNamespaces() {
		return nil, errors.New("Pods must have an infra container to share namespaces")
	}
	if pod.HasInfraContainer() && !pod.SharesNamespaces() {
		logrus.Infof("Pod has an infra container, but shares no namespaces")
	}

	// Unless the user has specified a name, use a randomly generated one.
	// Note that name conflicts may occur (see #11735), so we need to loop.
	generateName := pod.config.Name == ""
	var addPodErr error
	for {
		if generateName {
			name, err := r.generateName()
			if err != nil {
				return nil, err
			}
			pod.config.Name = name
		}

		if p.InfraContainerSpec != nil && p.InfraContainerSpec.Hostname == "" {
			p.InfraContainerSpec.Hostname = pod.config.Name
		}
		if addPodErr = r.state.AddPod(pod); addPodErr == nil {
			return pod, nil
		}
		if !generateName || (!errors.Is(addPodErr, define.ErrPodExists) && !errors.Is(addPodErr, define.ErrCtrExists)) {
			break
		}
	}
	return nil, fmt.Errorf("adding pod to state: %w", addPodErr)
}

// AddInfra adds the created infra container to the pod state
func (r *Runtime) AddInfra(_ context.Context, pod *Pod, infraCtr *Container) (*Pod, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}
	pod.state.InfraContainerID = infraCtr.ID()
	if err := pod.save(); err != nil {
		return nil, err
	}
	pod.newPodEvent(events.Create)
	return pod, nil
}

// SavePod is a helper function to save the pod state from outside of libpod
func (r *Runtime) SavePod(pod *Pod) error {
	if !r.valid {
		return define.ErrRuntimeStopped
	}
	if err := pod.save(); err != nil {
		return err
	}
	pod.newPodEvent(events.Create)
	return nil
}

// DO NOT USE THIS FUNCTION DIRECTLY. Use removePod(), below. It will call
// removeMalformedPod() if necessary.
func (r *Runtime) removeMalformedPod(ctx context.Context, p *Pod, ctrs []*Container, force bool, timeout *uint, ctrNamedVolumes map[string]*ContainerNamedVolume) (map[string]error, error) {
	removedCtrs := make(map[string]error)
	errored := false
	for _, ctr := range ctrs {
		err := func() error {
			ctrLock := ctr.lock
			ctrLock.Lock()
			defer func() {
				ctrLock.Unlock()
			}()

			if err := ctr.syncContainer(); err != nil {
				return err
			}

			for _, vol := range ctr.config.NamedVolumes {
				ctrNamedVolumes[vol.Name] = vol
			}

			opts := ctrRmOpts{
				Force:      force,
				RemovePod:  true,
				IgnoreDeps: true,
				Timeout:    timeout,
			}
			_, _, err := r.removeContainer(ctx, ctr, opts)
			return err
		}()
		removedCtrs[ctr.ID()] = err
		if err != nil {
			errored = true
		}
	}

	// So, technically, no containers have been *removed*.
	// They're still in the DB.
	// So just return nil for removed containers. Squash all the errors into
	// a multierror so we don't lose them.
	if errored {
		var allErrors error
		for ctr, err := range removedCtrs {
			if err != nil {
				allErrors = multierror.Append(allErrors, fmt.Errorf("removing container %s: %w", ctr, err))
			}
		}
		return nil, fmt.Errorf("no containers were removed due to the following errors: %w", allErrors)
	}

	// Clear infra container ID before we remove the infra container.
	// There is a potential issue if we don't do that, and removal is
	// interrupted between RemoveAllContainers() below and the pod's removal
	// later - we end up with a reference to a nonexistent infra container.
	p.state.InfraContainerID = ""
	if err := p.save(); err != nil {
		return nil, err
	}

	// Remove all containers in the pod from the state.
	if err := r.state.RemovePodContainers(p); err != nil {
		// If this fails, there isn't much more we can do.
		// The containers in the pod are unusable, but they still exist,
		// so pod removal will fail.
		return nil, err
	}

	return removedCtrs, nil
}

func (r *Runtime) removePod(ctx context.Context, p *Pod, removeCtrs, force bool, timeout *uint) (map[string]error, error) {
	removedCtrs := make(map[string]error)

	if err := p.updatePod(); err != nil {
		return nil, err
	}

	ctrs, err := r.state.PodContainers(p)
	if err != nil {
		return nil, err
	}
	numCtrs := len(ctrs)

	// If the only running container in the pod is the pause container, remove the pod and container unconditionally.
	pauseCtrID := p.state.InfraContainerID
	if numCtrs == 1 && ctrs[0].ID() == pauseCtrID {
		removeCtrs = true
		force = true
	}
	if !removeCtrs && numCtrs > 0 {
		return nil, fmt.Errorf("pod %s contains containers and cannot be removed: %w", p.ID(), define.ErrCtrExists)
	}

	var (
		removalErr      error
		ctrNamedVolumes map[string]*ContainerNamedVolume
	)

	// Build a graph of all containers in the pod.
	graph, err := BuildContainerGraph(ctrs)
	if err != nil {
		// We have to allow the pod to be removed.
		// But let's only do it if force is set.
		if !force {
			return nil, fmt.Errorf("cannot create container graph for pod %s: %w", p.ID(), err)
		}

		removalErr = fmt.Errorf("creating container graph for pod %s failed, fell back to loop removal: %w", p.ID(), err)

		removedCtrs, err = r.removeMalformedPod(ctx, p, ctrs, force, timeout, ctrNamedVolumes)
		if err != nil {
			logrus.Errorf("Error creating container graph for pod %s: %v. Falling back to loop removal.", p.ID(), err)
			return removedCtrs, err
		}
	} else {
		var (
			ctrErrors   map[string]error
			ctrsVisited map[string]bool
		)

		ctrNamedVolumes, ctrsVisited, ctrErrors, err = removeContainerGraph(ctx, graph, p, timeout, force)
		if err != nil {
			return nil, err
		}

		// Finalize the removed containers list
		for ctr := range ctrsVisited {
			removedCtrs[ctr] = ctrErrors[ctr]
		}

		if len(ctrErrors) > 0 {
			return removedCtrs, fmt.Errorf("not all containers could be removed from pod %s: %w", p.ID(), define.ErrRemovingCtrs)
		}
	}

	for volName := range ctrNamedVolumes {
		volume, err := r.state.Volume(volName)
		if err != nil && !errors.Is(err, define.ErrNoSuchVolume) {
			logrus.Errorf("Retrieving volume %s: %v", volName, err)
			continue
		}
		if !volume.Anonymous() {
			continue
		}
		if err := r.removeVolume(ctx, volume, false, timeout, false); err != nil {
			// If the anonymous volume is still being used that means it was likely transferred
			// to another container via --volumes-from so no need to log this as real error.
			if errors.Is(err, define.ErrNoSuchVolume) || errors.Is(err, define.ErrVolumeRemoved) || errors.Is(err, define.ErrVolumeBeingUsed) {
				continue
			}
			logrus.Errorf("Removing volume %s: %v", volName, err)
		}
	}

	// Remove pod cgroup
	if err := p.removePodCgroup(); err != nil {
		if removalErr == nil {
			removalErr = fmt.Errorf("removing pod %s cgroup: %w", p.ID(), err)
		} else {
			logrus.Errorf("Deleting pod %s cgroup %s: %v", p.ID(), p.state.CgroupPath, err)
		}
	}

	if err := p.maybeRemoveServiceContainer(); err != nil {
		return removedCtrs, err
	}

	// Remove pod from state
	if err := r.state.RemovePod(p); err != nil {
		if removalErr != nil {
			logrus.Errorf("%v", removalErr)
		}
		return removedCtrs, err
	}

	// Mark pod invalid
	p.valid = false
	p.newPodEvent(events.Remove)

	// Deallocate the pod lock
	if err := p.lock.Free(); err != nil {
		if removalErr == nil {
			removalErr = fmt.Errorf("freeing pod %s lock: %w", p.ID(), err)
		} else {
			logrus.Errorf("Freeing pod %s lock: %v", p.ID(), err)
		}
	}

	return removedCtrs, removalErr
}
