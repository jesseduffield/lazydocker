package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
)

// DockerCommand is our main git interface
type DockerCommand struct {
	Log       *logrus.Entry
	OSCommand *OSCommand
	Tr        *i18n.Localizer
	Config    config.AppConfigurer
	Client    *client.Client
}

// NewDockerCommand it runs git commands
func NewDockerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.Localizer, config config.AppConfigurer) (*DockerCommand, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}

	return &DockerCommand{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    config,
		Client:    cli,
	}, nil
}

// GetContainers returns a slice of docker containers
func (c *DockerCommand) GetContainers() ([]*Container, error) {
	containers, err := c.Client.ContainerList(context.Background(), types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	ownContainers := make([]*Container, len(containers))

	for i, container := range containers {
		c.Log.Warn(spew.Sdump(container))
		c.Log.Warn(fmt.Sprintf("%s %s\n", container.ID[:10], container.Image))
		serviceName, ok := container.Labels["com.docker.compose.service"]
		if !ok {
			serviceName = ""
			c.Log.Warn("Could not get service name from docker container")
		}
		ownContainers[i] = &Container{ID: container.ID, Name: strings.TrimLeft(container.Names[0], "/"), ServiceName: serviceName, Container: container}
	}

	return ownContainers, nil
}

type removeContainerConfig struct {
	// RemoveVolumes removes any volumes attached to the container
	RemoveVolumes bool

	// Force forces the container to be removed, even if it's running
	Force bool
}

type RemoveContainerOption func(c *removeContainerConfig)

// RemoveVolumes is an option to remove volumes attached to the container
func RemoveVolumes(c *removeContainerConfig) {
	c.RemoveVolumes = true
}

// Force is an option to remove volumes attached to the container
func Force(c *removeContainerConfig) {
	c.Force = true
}

// MustStopContainer tells us that we must stop the container before removing it
const MustStopContainer = iota

// RemoveContainer removes a container
func (c *DockerCommand) RemoveContainer(containerID string, options ...RemoveContainerOption) error {
	config := &removeContainerConfig{}
	for _, option := range options {
		option(config)
	}
	flags := ""
	if config.RemoveVolumes {
		flags += " --volumes "
	}
	if config.Force {
		flags += " --force "
	}

	err := c.OSCommand.RunCommand(fmt.Sprintf("docker rm %s %s", flags, containerID))
	if err != nil {
		if strings.Contains(err.Error(), "Stop the container before attempting removal or force remove") {
			return ComplexError{
				Code:    MustStopContainer,
				Message: err.Error(),
				frame:   xerrors.Caller(1),
			}
		}
		return err
	}
	return nil
}
