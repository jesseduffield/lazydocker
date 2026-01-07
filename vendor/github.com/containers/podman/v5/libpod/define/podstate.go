package define

const (
	// PodStateCreated indicates the pod is created but has not been started
	PodStateCreated = "Created"
	// PodStateErrored indicates the pod is in an errored state where
	// information about it can no longer be retrieved
	PodStateErrored = "Error"
	// PodStateExited indicates the pod ran but has been stopped
	PodStateExited = "Exited"
	// PodStatePaused indicates the pod has been paused
	PodStatePaused = "Paused"
	// PodStateRunning indicates that all of the containers in the pod are
	// running.
	PodStateRunning = "Running"
	// PodStateDegraded indicates that at least one, but not all, of the
	// containers in the pod are running.
	PodStateDegraded = "Degraded"
	// PodStateStopped indicates all of the containers belonging to the pod
	// are stopped.
	PodStateStopped = "Stopped"
)
