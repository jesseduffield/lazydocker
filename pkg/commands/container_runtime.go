package commands

import (
	"fmt"
	"io"
	"os/exec"
)

// ContainerRuntime defines the interface that all container runtimes must implement
// This allows lazydocker to work with different container systems (Docker, Apple Container, etc.)
type ContainerRuntime interface {
	// Container operations
	GetContainers() ([]*Container, error)
	RefreshContainersAndServices(currentServices []*Service, currentContainers []*Container) ([]*Container, []*Service, error)
	RefreshContainerDetails(containers []*Container) error
	PruneContainers() error

	// Image operations
	RefreshImages() ([]*Image, error)
	PruneImages() error

	// Volume operations
	RefreshVolumes() ([]*Volume, error)
	PruneVolumes() error

	// Network operations
	RefreshNetworks() ([]*Network, error)
	PruneNetworks() error

	// Volume create (optional capability)
	CreateVolume(name string, opts map[string]string) error

	// Service operations (for docker-compose/equivalent)
	GetServices() ([]*Service, error)
	InDockerComposeProject() bool

	// System operations
	ViewAllLogs() (cmd Cmd, err error)
	DockerComposeConfig() string
	SystemStatus() (map[string]interface{}, error)

	// Runtime information
	GetRuntimeName() string
	GetRuntimeVersion() string

	// Close resources
	io.Closer
}

// Cmd represents a command that can be executed
type Cmd interface {
	Start() error
	Wait() error
	Kill() error
}

// ContainerRuntimeAdapter provides a unified interface for accessing container operations
// It wraps either a DockerCommand or AppleContainerCommand and exposes them through
// the ContainerRuntime interface
type ContainerRuntimeAdapter struct {
	dockerCommand         *DockerCommand
	appleContainerCommand *AppleContainerCommand
	runtimeType           string
}

// operationNotSupported returns an error indicating the operation is not supported by the current runtime
func (c *ContainerRuntimeAdapter) operationNotSupported(operation string) error {
	return fmt.Errorf("%s not supported by %s runtime", operation, c.runtimeType)
}

// commandNotAvailable returns an error indicating the runtime command is not available
func (c *ContainerRuntimeAdapter) commandNotAvailable() error {
	return fmt.Errorf("%s command not available", c.runtimeType)
}

// unsupportedRuntime returns an error indicating the runtime type is not supported
func (c *ContainerRuntimeAdapter) unsupportedRuntime() error {
	return fmt.Errorf("unsupported runtime '%s'", c.runtimeType)
}

// NewContainerRuntimeAdapter creates a new adapter based on the provided commands
func NewContainerRuntimeAdapter(docker *DockerCommand, apple *AppleContainerCommand, runtimeType string) *ContainerRuntimeAdapter {
	return &ContainerRuntimeAdapter{
		dockerCommand:         docker,
		appleContainerCommand: apple,
		runtimeType:           runtimeType,
	}
}

// Supports returns whether a given feature is supported by the active runtime
func (c *ContainerRuntimeAdapter) Supports(f Feature) bool {
	switch c.runtimeType {
	case "docker":
		// Docker runtime supports all features used by LazyDocker
		return true
	case "apple":
		if c.appleContainerCommand == nil {
			return false
		}
		return c.appleContainerCommand.Supports(f)
	default:
		return false
	}
}

// GetContainers returns containers from the active runtime
func (c *ContainerRuntimeAdapter) GetContainers() ([]*Container, error) {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		return c.dockerCommand.GetContainers(nil)
	case "apple":
		if c.appleContainerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		return c.appleContainerCommand.GetContainers()
	default:
		return nil, c.unsupportedRuntime()
	}
}

// RefreshContainersAndServices refreshes both containers and services
func (c *ContainerRuntimeAdapter) RefreshContainersAndServices(currentServices []*Service, currentContainers []*Container) ([]*Container, []*Service, error) {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return nil, nil, c.commandNotAvailable()
		}
		return c.dockerCommand.RefreshContainersAndServices(currentServices, currentContainers)
	case "apple":
		if c.appleContainerCommand == nil {
			return nil, nil, c.commandNotAvailable()
		}
		// Apple Container doesn't have services concept, so we just return containers
		containers, err := c.appleContainerCommand.GetContainers()
		return containers, []*Service{}, err
	default:
		return nil, nil, c.unsupportedRuntime()
	}
}

// RefreshContainerDetails updates container details
func (c *ContainerRuntimeAdapter) RefreshContainerDetails(containers []*Container) error {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return c.commandNotAvailable()
		}
		return c.dockerCommand.RefreshContainerDetails(containers)
	case "apple":
		// Apple Container doesn't need explicit refresh - details are fetched with containers
		return nil
	default:
		return c.unsupportedRuntime()
	}
}

// PruneContainers removes unused containers
func (c *ContainerRuntimeAdapter) PruneContainers() error {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return c.commandNotAvailable()
		}
		return c.dockerCommand.PruneContainers()
	case "apple":
		// Apple Container might not have prune functionality
		return c.operationNotSupported("container pruning")
	default:
		return c.unsupportedRuntime()
	}
}

// RefreshImages returns images from the active runtime
func (c *ContainerRuntimeAdapter) RefreshImages() ([]*Image, error) {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		return c.dockerCommand.RefreshImages()
	case "apple":
		if c.appleContainerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		return c.appleContainerCommand.GetImages()
	default:
		return nil, c.unsupportedRuntime()
	}
}

// PruneImages removes unused images
func (c *ContainerRuntimeAdapter) PruneImages() error {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return c.commandNotAvailable()
		}
		return c.dockerCommand.PruneImages()
	case "apple":
		return c.operationNotSupported("image pruning")
	default:
		return c.unsupportedRuntime()
	}
}

// RefreshVolumes returns volumes from the active runtime
func (c *ContainerRuntimeAdapter) RefreshVolumes() ([]*Volume, error) {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		return c.dockerCommand.RefreshVolumes()
	case "apple":
		if c.appleContainerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		if c.appleContainerCommand.OSCommand == nil { // avoid invoking external CLI in tests
			return []*Volume{}, nil
		}
		return c.appleContainerCommand.RefreshVolumes()
	default:
		return nil, c.unsupportedRuntime()
	}
}

// PruneVolumes removes unused volumes
func (c *ContainerRuntimeAdapter) PruneVolumes() error {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return c.commandNotAvailable()
		}
		return c.dockerCommand.PruneVolumes()
	case "apple":
		return c.operationNotSupported("volume pruning")
	default:
		return c.unsupportedRuntime()
	}
}

// RefreshNetworks returns networks from the active runtime
func (c *ContainerRuntimeAdapter) RefreshNetworks() ([]*Network, error) {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		return c.dockerCommand.RefreshNetworks()
	case "apple":
		if c.appleContainerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		if c.appleContainerCommand.OSCommand == nil { // avoid invoking external CLI in tests
			return []*Network{}, nil
		}
		return c.appleContainerCommand.GetNetworks()
	default:
		return nil, c.unsupportedRuntime()
	}
}

// PruneNetworks removes unused networks
func (c *ContainerRuntimeAdapter) PruneNetworks() error {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return c.commandNotAvailable()
		}
		return c.dockerCommand.PruneNetworks()
	case "apple":
		return c.operationNotSupported("network pruning")
	default:
		return c.unsupportedRuntime()
	}
}

// GetServices returns services from the active runtime
func (c *ContainerRuntimeAdapter) GetServices() ([]*Service, error) {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		return c.dockerCommand.GetServices()
	case "apple":
		// Apple Container doesn't have services concept
		return []*Service{}, nil
	default:
		return nil, c.unsupportedRuntime()
	}
}

// InDockerComposeProject returns whether we're in a docker-compose project
func (c *ContainerRuntimeAdapter) InDockerComposeProject() bool {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return false
		}
		return c.dockerCommand.InDockerComposeProject
	case "apple":
		// Apple Container doesn't have compose concept
		return false
	default:
		return false
	}
}

// ViewAllLogs returns a command to view all logs
func (c *ContainerRuntimeAdapter) ViewAllLogs() (cmd Cmd, err error) {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		execCmd, err := c.dockerCommand.ViewAllLogs()
		if err != nil {
			return nil, err
		}
		return &cmdWrapper{cmd: execCmd}, nil
	case "apple":
		return nil, c.operationNotSupported("viewing all logs")
	default:
		return nil, c.unsupportedRuntime()
	}
}

// DockerComposeConfig returns the docker-compose config
func (c *ContainerRuntimeAdapter) DockerComposeConfig() string {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand == nil {
			return ""
		}
		return c.dockerCommand.DockerComposeConfig()
	case "apple":
		return ""
	default:
		return ""
	}
}

// CreateVolume creates a volume (runtime-dependent)
func (c *ContainerRuntimeAdapter) CreateVolume(name string, opts map[string]string) error {
	switch c.runtimeType {
	case "docker":
		return c.operationNotSupported("volume create")
	case "apple":
		if c.appleContainerCommand == nil {
			return c.commandNotAvailable()
		}
		if !c.appleContainerCommand.Supports(FeatureVolumeCreate) {
			return c.operationNotSupported("volume create")
		}
		return c.appleContainerCommand.CreateVolume(name, opts)
	default:
		return c.unsupportedRuntime()
	}
}

// SystemStatus returns a runtime-specific system status map
func (c *ContainerRuntimeAdapter) SystemStatus() (map[string]interface{}, error) {
	switch c.runtimeType {
	case "docker":
		return map[string]interface{}{"runtime": "docker"}, nil
	case "apple":
		if c.appleContainerCommand == nil {
			return nil, c.commandNotAvailable()
		}
		if c.appleContainerCommand.OSCommand == nil {
			return map[string]interface{}{"runtime": "apple"}, nil
		}
		return c.appleContainerCommand.SystemStatus()
	default:
		return nil, c.unsupportedRuntime()
	}
}

// GetRuntimeName returns the name of the runtime
func (c *ContainerRuntimeAdapter) GetRuntimeName() string {
	return c.runtimeType
}

// GetRuntimeVersion returns the version of the runtime
func (c *ContainerRuntimeAdapter) GetRuntimeVersion() string {
	switch c.runtimeType {
	case "docker":
		return "Docker Runtime"
	case "apple":
		return "Apple Container Runtime"
	default:
		return "Unknown Runtime"
	}
}

// Close closes the underlying runtime resources
func (c *ContainerRuntimeAdapter) Close() error {
	switch c.runtimeType {
	case "docker":
		if c.dockerCommand != nil {
			return c.dockerCommand.Close()
		}
	case "apple":
		// Apple Container command doesn't implement io.Closer currently
		// This is fine since it doesn't hold persistent connections
	}
	return nil
}

// cmdWrapper wraps an exec.Cmd to implement our Cmd interface
type cmdWrapper struct {
	cmd *exec.Cmd
}

func (c *cmdWrapper) Start() error {
	return c.cmd.Start()
}

func (c *cmdWrapper) Wait() error {
	return c.cmd.Wait()
}

func (c *cmdWrapper) Kill() error {
	if c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}
