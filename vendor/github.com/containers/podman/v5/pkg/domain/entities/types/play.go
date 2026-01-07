package types

type PlayKubePod struct {
	// ID - ID of the pod created as a result of play kube.
	ID string
	// Containers - the IDs of the containers running in the created pod.
	Containers []string
	// InitContainers - the IDs of the init containers to be run in the created pod.
	InitContainers []string
	// Logs - non-fatal errors and log messages while processing.
	Logs []string
	// ContainerErrors - any errors that occurred while starting containers
	// in the pod.
	ContainerErrors []string
}

type PlayKubeVolume struct {
	// Name - Name of the volume created by play kube.
	Name string
}

type PlayKubeReport struct {
	// Pods - pods created by play kube.
	Pods []PlayKubePod
	// Volumes - volumes created by play kube.
	Volumes []PlayKubeVolume
	PlayKubeTeardown
	// Secrets - secrets created by play kube
	Secrets []PlaySecret
	// ServiceContainerID - ID of the service container if one is created
	ServiceContainerID string
	// If set, exit with the specified exit code.
	ExitCode *int32
}

type KubePlayReport = PlayKubeReport

// PlayKubeDownReport contains the results of tearing down play kube
type PlayKubeTeardown struct {
	StopReport     []*PodStopReport
	RmReport       []*PodRmReport
	VolumeRmReport []*VolumeRmReport
	SecretRmReport []*SecretRmReport
}

type PlaySecret struct {
	CreateReport *SecretCreateReport
}
