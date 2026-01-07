package define

import (
	"fmt"
	"time"
)

// ContainerStatus represents the current state of a container
type ContainerStatus int

const (
	// ContainerStateUnknown indicates that the container is in an error
	// state where information about it cannot be retrieved
	ContainerStateUnknown ContainerStatus = iota
	// ContainerStateConfigured indicates that the container has had its
	// storage configured but it has not been created in the OCI runtime
	ContainerStateConfigured ContainerStatus = iota
	// ContainerStateCreated indicates the container has been created in
	// the OCI runtime but not started
	ContainerStateCreated ContainerStatus = iota
	// ContainerStateRunning indicates the container is currently executing
	ContainerStateRunning ContainerStatus = iota
	// ContainerStateStopped indicates that the container was running but has
	// exited
	ContainerStateStopped ContainerStatus = iota
	// ContainerStatePaused indicates that the container has been paused
	ContainerStatePaused ContainerStatus = iota
	// ContainerStateExited indicates the container has stopped and been
	// cleaned up
	ContainerStateExited ContainerStatus = iota
	// ContainerStateRemoving indicates the container is in the process of
	// being removed.
	ContainerStateRemoving ContainerStatus = iota
	// ContainerStateStopping indicates the container is in the process of
	// being stopped.
	ContainerStateStopping ContainerStatus = iota
)

// ContainerStatus returns a string representation for users of a container
// state. All results should match Docker's versions (from `docker ps`) as
// closely as possible, given the different set of states we support.
func (t ContainerStatus) String() string {
	switch t {
	case ContainerStateUnknown:
		return "unknown"
	case ContainerStateConfigured:
		// The naming here is confusing, but it's necessary for Docker
		// compatibility - their Created state is our Configured state.
		return "created"
	case ContainerStateCreated:
		// Docker does not have an equivalent to this state, so give it
		// a clear name. Most of the time this is a purely transitory
		// state between Configured and Running so we don't expect to
		// see it much anyways.
		return "initialized"
	case ContainerStateRunning:
		return "running"
	case ContainerStateStopped:
		return "stopped"
	case ContainerStatePaused:
		return "paused"
	case ContainerStateExited:
		return "exited"
	case ContainerStateRemoving:
		return "removing"
	case ContainerStateStopping:
		return "stopping"
	}
	return "bad state"
}

// StringToContainerStatus converts a string representation of a container's
// status into an actual container status type
func StringToContainerStatus(status string) (ContainerStatus, error) {
	switch status {
	case ContainerStateUnknown.String():
		return ContainerStateUnknown, nil
	case ContainerStateConfigured.String():
		return ContainerStateConfigured, nil
	case ContainerStateCreated.String():
		return ContainerStateCreated, nil
	case ContainerStateRunning.String():
		return ContainerStateRunning, nil
	case ContainerStateStopped.String():
		return ContainerStateStopped, nil
	case ContainerStatePaused.String():
		return ContainerStatePaused, nil
	case ContainerStateExited.String():
		return ContainerStateExited, nil
	case ContainerStateRemoving.String():
		return ContainerStateRemoving, nil
	default:
		return ContainerStateUnknown, fmt.Errorf("unknown container state: %s: %w", status, ErrInvalidArg)
	}
}

// ContainerExecStatus is the status of an exec session within a container.
type ContainerExecStatus int

const (
	// ExecStateUnknown indicates that the state of the exec session is not
	// known.
	ExecStateUnknown ContainerExecStatus = iota
	// ExecStateCreated indicates that the exec session has been created but
	// not yet started
	ExecStateCreated ContainerExecStatus = iota
	// ExecStateRunning indicates that the exec session has been started but
	// has not yet exited.
	ExecStateRunning ContainerExecStatus = iota
	// ExecStateStopped indicates that the exec session has stopped and is
	// no longer running.
	ExecStateStopped ContainerExecStatus = iota
)

// String returns a string representation of a given exec state.
func (s ContainerExecStatus) String() string {
	switch s {
	case ExecStateUnknown:
		return "unknown"
	case ExecStateCreated:
		return "created"
	case ExecStateRunning:
		return "running"
	case ExecStateStopped:
		return "stopped"
	default:
		return "bad state"
	}
}

// ContainerStats contains the statistics information for a running container
type ContainerStats struct {
	AvgCPU        float64
	ContainerID   string
	Name          string
	PerCPU        []uint64
	CPU           float64
	CPUNano       uint64
	CPUSystemNano uint64
	SystemNano    uint64
	MemUsage      uint64
	MemLimit      uint64
	MemPerc       float64
	// Map of interface name to network statistics for that interface.
	Network     map[string]ContainerNetworkStats
	BlockInput  uint64
	BlockOutput uint64
	PIDs        uint64
	UpTime      time.Duration
	Duration    uint64
}

// Statistics for an individual container network interface
type ContainerNetworkStats struct {
	RxBytes   uint64
	RxDropped uint64
	RxErrors  uint64
	RxPackets uint64
	TxBytes   uint64
	TxDropped uint64
	TxErrors  uint64
	TxPackets uint64
}
