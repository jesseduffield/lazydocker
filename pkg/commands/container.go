package commands

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/sasha-s/go-deadlock"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
)

// Container : A docker Container
type Container struct {
	Name            string
	ServiceName     string
	ContainerNumber string // might make this an int in the future if need be

	// OneOff tells us if the container is just a job container or is actually bound to the service
	OneOff          bool
	ProjectName     string
	ID              string
	Container       dockerTypes.Container
	Client          *client.Client
	OSCommand       *OSCommand
	Log             *logrus.Entry
	StatHistory     []*RecordedStats
	Details         dockerTypes.ContainerJSON
	MonitoringStats bool
	DockerCommand   LimitedDockerCommand
	Tr              *i18n.TranslationSet

	StatsMutex deadlock.Mutex
}

// Remove removes the container
func (c *Container) Remove(options container.RemoveOptions) error {
	c.Log.Warn(fmt.Sprintf("removing container %s", c.Name))
	if err := c.Client.ContainerRemove(context.Background(), c.ID, options); err != nil {
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

// Stop stops the container
func (c *Container) Stop() error {
	c.Log.Warn(fmt.Sprintf("stopping container %s", c.Name))
	return c.Client.ContainerStop(context.Background(), c.ID, container.StopOptions{})
}

// Pause pauses the container
func (c *Container) Pause() error {
	c.Log.Warn(fmt.Sprintf("pausing container %s", c.Name))
	return c.Client.ContainerPause(context.Background(), c.ID)
}

// Unpause unpauses the container
func (c *Container) Unpause() error {
	c.Log.Warn(fmt.Sprintf("unpausing container %s", c.Name))
	return c.Client.ContainerUnpause(context.Background(), c.ID)
}

// Restart restarts the container
func (c *Container) Restart() error {
	c.Log.Warn(fmt.Sprintf("restarting container %s", c.Name))
	return c.Client.ContainerRestart(context.Background(), c.ID, container.StopOptions{})
}

// Attach attaches the container
func (c *Container) Attach() (*exec.Cmd, error) {
	if !c.DetailsLoaded() {
		return nil, errors.New(c.Tr.WaitingForContainerInfo)
	}

	// verify that we can in fact attach to this container
	if !c.Details.Config.OpenStdin {
		return nil, errors.New(c.Tr.UnattachableContainerError)
	}

	if c.Container.State == "exited" {
		return nil, errors.New(c.Tr.CannotAttachStoppedContainerError)
	}

	c.Log.Warn(fmt.Sprintf("attaching to container %s", c.Name))
	// TODO: use SDK
	cmd := c.OSCommand.NewCmd("docker", "attach", "--sig-proxy=false", c.ID)
	return cmd, nil
}

// Top returns process information
func (c *Container) Top(ctx context.Context) (container.ContainerTopOKBody, error) {
	detail, err := c.Inspect()
	if err != nil {
		return container.ContainerTopOKBody{}, err
	}

	// check container status
	if !detail.State.Running {
		return container.ContainerTopOKBody{}, errors.New("container is not running")
	}

	return c.Client.ContainerTop(ctx, c.ID, []string{})
}

// PruneContainers prunes containers
func (c *DockerCommand) PruneContainers() error {
	_, err := c.Client.ContainersPrune(context.Background(), filters.Args{})
	return err
}

// Inspect returns details about the container
func (c *Container) Inspect() (dockerTypes.ContainerJSON, error) {
	return c.Client.ContainerInspect(context.Background(), c.ID)
}

// RenderTop returns details about the container
func (c *Container) RenderTop(ctx context.Context) (string, error) {
	result, err := c.Top(ctx)
	if err != nil {
		return "", err
	}

	return utils.RenderTable(append([][]string{result.Titles}, result.Processes...))
}

// DetailsLoaded tells us whether we have yet loaded the details for a container.
// Sometimes it takes some time for a container to have its details loaded
// after it starts.
func (c *Container) DetailsLoaded() bool {
	return c.Details.ContainerJSONBase != nil
}
