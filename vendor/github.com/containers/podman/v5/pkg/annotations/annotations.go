package annotations

const (
	// SandboxID is the sandbox ID annotation.
	SandboxID = "io.kubernetes.cri-o.SandboxID"

	// ContainerManager is the annotation key for indicating the creator and
	// manager of the container.
	ContainerManager = "io.container.manager"
)

// ContainerType values
const (
	// ContainerTypeSandbox represents a pod sandbox container.
	ContainerTypeSandbox = "sandbox"

	// ContainerTypeContainer represents a container running within a pod.
	ContainerTypeContainer = "container"
)

// ContainerManagerLibpod indicates that libpod created and manages the
// container.
const ContainerManagerLibpod = "libpod"
