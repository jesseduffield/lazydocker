//go:build !remote

package libpod

import "github.com/containers/podman/v5/libpod/define"

// GetPodStatus determines the status of the pod based on the
// statuses of the containers in the pod.
// Returns a string representation of the pod status
func (p *Pod) GetPodStatus() (string, error) {
	ctrStatuses, err := p.Status()
	if err != nil {
		return define.PodStateErrored, err
	}
	return createPodStatusResults(ctrStatuses)
}

func createPodStatusResults(ctrStatuses map[string]define.ContainerStatus) (string, error) {
	ctrNum := len(ctrStatuses)
	if ctrNum == 0 {
		return define.PodStateCreated, nil
	}
	statuses := map[string]int{
		define.PodStateStopped: 0,
		define.PodStateRunning: 0,
		define.PodStatePaused:  0,
		define.PodStateCreated: 0,
		define.PodStateErrored: 0,
	}
	for _, ctrStatus := range ctrStatuses {
		switch ctrStatus {
		case define.ContainerStateExited:
			fallthrough
		case define.ContainerStateStopped:
			statuses[define.PodStateStopped]++
		case define.ContainerStateRunning:
			statuses[define.PodStateRunning]++
		case define.ContainerStatePaused:
			statuses[define.PodStatePaused]++
		case define.ContainerStateCreated, define.ContainerStateConfigured:
			statuses[define.PodStateCreated]++
		default:
			statuses[define.PodStateErrored]++
		}
	}

	switch {
	case statuses[define.PodStateRunning] == ctrNum:
		return define.PodStateRunning, nil
	case statuses[define.PodStateRunning] > 0:
		return define.PodStateDegraded, nil
	case statuses[define.PodStatePaused] == ctrNum:
		return define.PodStatePaused, nil
	case statuses[define.PodStateStopped] == ctrNum:
		return define.PodStateExited, nil
	case statuses[define.PodStateStopped] > 0:
		return define.PodStateStopped, nil
	case statuses[define.PodStateErrored] > 0:
		return define.PodStateErrored, nil
	default:
		return define.PodStateCreated, nil
	}
}
