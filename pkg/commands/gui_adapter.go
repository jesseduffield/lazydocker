package commands

import (
	"fmt"
	"os/exec"

	"github.com/jesseduffield/lazydocker/pkg/config"
)

// GuiContainerCommand provides an adapter that makes ContainerRuntime work with the GUI
// It implements the subset of DockerCommand methods that the GUI actually uses
type GuiContainerCommand struct {
	runtime       *ContainerRuntimeAdapter
	dockerCommand *DockerCommand // For docker-specific operations
	config        *config.AppConfig
}

// NewGuiContainerCommand creates a new GUI adapter for the container runtime
func NewGuiContainerCommand(runtime *ContainerRuntimeAdapter, dockerCommand *DockerCommand, config *config.AppConfig) *GuiContainerCommand {
	return &GuiContainerCommand{
		runtime:       runtime,
		dockerCommand: dockerCommand,
		config:        config,
	}
}

// Supports exposes runtime capability checks to the GUI layer
func (g *GuiContainerCommand) Supports(f Feature) bool {
	if g.runtime == nil {
		return false
	}
	return g.runtime.Supports(f)
}

// GetContainers returns containers
func (g *GuiContainerCommand) GetContainers(existingContainers []*Container) ([]*Container, error) {
	return g.runtime.GetContainers()
}

// RefreshContainersAndServices refreshes both containers and services
func (g *GuiContainerCommand) RefreshContainersAndServices(currentServices []*Service, currentContainers []*Container) ([]*Container, []*Service, error) {
	return g.runtime.RefreshContainersAndServices(currentServices, currentContainers)
}

// RefreshContainer updates details for a specific container
func (g *GuiContainerCommand) RefreshContainer(container *Container) error {
	containers := []*Container{container}
	return g.runtime.RefreshContainerDetails(containers)
}

// RefreshImages returns the list of images
func (g *GuiContainerCommand) RefreshImages() ([]*Image, error) {
	return g.runtime.RefreshImages()
}

// RefreshVolumes returns the list of volumes
func (g *GuiContainerCommand) RefreshVolumes() ([]*Volume, error) {
	return g.runtime.RefreshVolumes()
}

// RefreshNetworks returns the list of networks
func (g *GuiContainerCommand) RefreshNetworks() ([]*Network, error) {
	return g.runtime.RefreshNetworks()
}

// GetServices returns the list of services
func (g *GuiContainerCommand) GetServices() ([]*Service, error) {
	return g.runtime.GetServices()
}

// InDockerComposeProject checks if we're in a docker-compose project
func (g *GuiContainerCommand) InDockerComposeProject() bool {
	return g.runtime.InDockerComposeProject()
}

// DockerComposeConfig returns the docker-compose configuration
func (g *GuiContainerCommand) DockerComposeConfig() string {
	return g.runtime.DockerComposeConfig()
}

// GetRuntimeName returns the name of the active runtime
func (g *GuiContainerCommand) GetRuntimeName() string {
	return g.runtime.GetRuntimeName()
}

// GetRuntimeVersion returns a friendly runtime version/info string
func (g *GuiContainerCommand) GetRuntimeVersion() string {
	if g.runtime == nil {
		return ""
	}
	return g.runtime.GetRuntimeVersion()
}

// CreateVolume creates a volume via the active runtime
func (g *GuiContainerCommand) CreateVolume(name string, opts map[string]string) error {
	return g.runtime.CreateVolume(name, opts)
}

// SystemStatus returns a runtime-specific system status map
func (g *GuiContainerCommand) SystemStatus() (map[string]interface{}, error) {
	return g.runtime.SystemStatus()
}

// ViewAllLogs returns a command to view all logs
func (g *GuiContainerCommand) ViewAllLogs() (*exec.Cmd, error) {
	cmd, err := g.runtime.ViewAllLogs()
	if err != nil {
		return nil, err
	}
	// Convert from our Cmd interface to exec.Cmd
	// This is a bit of a hack but maintains compatibility
	if cmdWrapper, ok := cmd.(*cmdWrapper); ok {
		return cmdWrapper.cmd, nil
	}
	return nil, fmt.Errorf("viewing all logs not supported by %s runtime", g.runtime.GetRuntimeName())
}

// PruneContainers removes unused containers
func (g *GuiContainerCommand) PruneContainers() error {
	return g.runtime.PruneContainers()
}

// PruneImages removes unused images
func (g *GuiContainerCommand) PruneImages() error {
	return g.runtime.PruneImages()
}

// PruneVolumes removes unused volumes
func (g *GuiContainerCommand) PruneVolumes() error {
	return g.runtime.PruneVolumes()
}

// PruneNetworks removes unused networks
func (g *GuiContainerCommand) PruneNetworks() error {
	return g.runtime.PruneNetworks()
}

// NewCommandObject creates a new command object with defaults
func (g *GuiContainerCommand) NewCommandObject(obj CommandObject) CommandObject {
	if g.dockerCommand != nil {
		return g.dockerCommand.NewCommandObject(obj)
	}
	// For Apple Container, just return the object as-is since it doesn't use docker-compose
	return obj
}

// GetClient returns the Docker client if available (only for Docker runtime)
func (g *GuiContainerCommand) GetClient() interface{} {
	if g.dockerCommand != nil {
		return g.dockerCommand.Client
	}
	return nil
}

// CreateClientStatMonitor creates a stat monitor for a container (Docker-specific)
func (g *GuiContainerCommand) CreateClientStatMonitor(container *Container) {
	if g.dockerCommand != nil {
		g.dockerCommand.CreateClientStatMonitor(container)
	}
}
