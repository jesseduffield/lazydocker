//go:build !remote

package libpod

import (
	"fmt"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"go.podman.io/storage/pkg/stringid"
)

// Creates a new, empty pod
func newPod(runtime *Runtime) *Pod {
	pod := new(Pod)
	pod.config = new(PodConfig)
	pod.config.ID = stringid.GenerateRandomID()
	pod.config.Labels = make(map[string]string)
	pod.config.CreatedTime = time.Now()
	//	pod.config.InfraContainer = new(ContainerConfig)
	pod.state = new(podState)
	pod.runtime = runtime

	return pod
}

// Update pod state from database
func (p *Pod) updatePod() error {
	if err := p.runtime.state.UpdatePod(p); err != nil {
		return err
	}

	return nil
}

// Save pod state to database
func (p *Pod) save() error {
	if err := p.runtime.state.SavePod(p); err != nil {
		return fmt.Errorf("saving pod %s state: %w", p.ID(), err)
	}

	return nil
}

// Refresh a pod's state after restart
// This cannot lock any other pod, but may lock individual containers, as those
// will have refreshed by the time pod refresh runs.
func (p *Pod) refresh() error {
	// Need to do an update from the DB to pull potentially-missing state
	if err := p.runtime.state.UpdatePod(p); err != nil {
		return err
	}

	if !p.valid {
		return define.ErrPodRemoved
	}

	// Retrieve the pod's lock
	lock, err := p.runtime.lockManager.AllocateAndRetrieveLock(p.config.LockID)
	if err != nil {
		return fmt.Errorf("retrieving lock %d for pod %s: %w", p.config.LockID, p.ID(), err)
	}
	p.lock = lock

	if err := p.platformRefresh(); err != nil {
		return err
	}

	// Save changes
	return p.save()
}

// resetPodState resets state fields to default values.
// It is performed before a refresh and clears the state after a reboot.
// It does not save the results - assumes the database will do that for us.
func resetPodState(state *podState) {
	state.CgroupPath = ""
}
