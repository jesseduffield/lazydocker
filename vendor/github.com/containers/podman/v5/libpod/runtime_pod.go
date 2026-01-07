//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/containers/podman/v5/libpod/define"
)

// Contains the public Runtime API for pods

// A PodCreateOption is a functional option which alters the Pod created by
// NewPod
type PodCreateOption func(*Pod) error

// PodFilter is a function to determine whether a pod is included in command
// output. Pods to be outputted are tested using the function. A true return
// will include the pod, a false return will exclude it.
type PodFilter func(*Pod) bool

// RemovePod removes a pod
// If removeCtrs is specified, containers will be removed
// Otherwise, a pod that is not empty will return an error and not be removed
// If force is specified with removeCtrs, all containers will be stopped before
// being removed
// Otherwise, the pod will not be removed if any containers are running
func (r *Runtime) RemovePod(ctx context.Context, p *Pod, removeCtrs, force bool, timeout *uint) (map[string]error, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	if !p.valid {
		if ok, _ := r.state.HasPod(p.ID()); !ok {
			// Pod probably already removed
			// Or was never in the runtime to begin with
			return make(map[string]error), nil
		}
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	return r.removePod(ctx, p, removeCtrs, force, timeout)
}

// GetPod retrieves a pod by its ID
func (r *Runtime) GetPod(id string) (*Pod, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	return r.state.Pod(id)
}

// HasPod checks to see if a pod with the given ID exists
func (r *Runtime) HasPod(id string) (bool, error) {
	if !r.valid {
		return false, define.ErrRuntimeStopped
	}

	return r.state.HasPod(id)
}

// LookupPod retrieves a pod by its name or a partial ID
// If a partial ID is not unique, an error will be returned
func (r *Runtime) LookupPod(idOrName string) (*Pod, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	return r.state.LookupPod(idOrName)
}

// Pods retrieves all pods
// Filters can be provided which will determine which pods are included in the
// output. Multiple filters are handled by ANDing their output, so only pods
// matching all filters are returned
func (r *Runtime) Pods(filters ...PodFilter) ([]*Pod, error) {
	pods, err := r.GetAllPods()
	if err != nil {
		return nil, err
	}
	podsFiltered := make([]*Pod, 0, len(pods))
	for _, pod := range pods {
		include := true
		for _, filter := range filters {
			include = include && filter(pod)
		}

		if include {
			podsFiltered = append(podsFiltered, pod)
		}
	}

	return podsFiltered, nil
}

// GetAllPods retrieves all pods
func (r *Runtime) GetAllPods() ([]*Pod, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	return r.state.AllPods()
}

// GetLatestPod returns a pod object of the latest created pod.
func (r *Runtime) GetLatestPod() (*Pod, error) {
	lastCreatedIndex := -1
	var lastCreatedTime time.Time
	pods, err := r.GetAllPods()
	if err != nil {
		return nil, fmt.Errorf("unable to get all pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, define.ErrNoSuchPod
	}
	for podIndex, pod := range pods {
		createdTime := pod.config.CreatedTime
		if createdTime.After(lastCreatedTime) {
			lastCreatedTime = createdTime
			lastCreatedIndex = podIndex
		}
	}
	return pods[lastCreatedIndex], nil
}

// GetRunningPods returns an array of running pods
func (r *Runtime) GetRunningPods() ([]*Pod, error) {
	var (
		pods        []string
		runningPods []*Pod
	)
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}
	containers, err := r.GetRunningContainers()
	if err != nil {
		return nil, err
	}
	// Assemble running pods
	for _, c := range containers {
		if !slices.Contains(pods, c.PodID()) {
			pods = append(pods, c.PodID())
			pod, err := r.GetPod(c.PodID())
			if err != nil {
				if errors.Is(err, define.ErrPodRemoved) || errors.Is(err, define.ErrNoSuchPod) {
					continue
				}
				return nil, err
			}
			runningPods = append(runningPods, pod)
		}
	}
	return runningPods, nil
}

// PrunePods removes unused pods and their containers from local storage.
func (r *Runtime) PrunePods(_ context.Context) (map[string]error, error) {
	response := make(map[string]error)
	states := []string{define.PodStateStopped, define.PodStateExited}
	filterFunc := func(p *Pod) bool {
		state, _ := p.GetPodStatus()
		return slices.Contains(states, state)
	}
	pods, err := r.Pods(filterFunc)
	if err != nil {
		return nil, err
	}
	if len(pods) < 1 {
		return response, nil
	}
	for _, pod := range pods {
		var timeout *uint
		_, err := r.removePod(context.TODO(), pod, true, false, timeout)
		response[pod.ID()] = err
	}
	return response, nil
}
