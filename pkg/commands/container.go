package commands

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/go-errors/errors"
	"github.com/christophe-duc/lazypodman/pkg/i18n"
	"github.com/christophe-duc/lazypodman/pkg/utils"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
)

// Container represents a Podman container
type Container struct {
	Name            string
	ServiceName     string
	ContainerNumber string // might make this an int in the future if need be

	// OneOff tells us if the container is just a job container or is actually bound to the service
	OneOff          bool
	ProjectName     string
	ID              string
	Summary         ContainerSummary
	Runtime         ContainerRuntime
	OSCommand       *OSCommand
	Log             *logrus.Entry
	StatHistory     []*RecordedStats
	Details         *ContainerDetails
	MonitoringStats bool
	PodmanCommand   LimitedPodmanCommand
	Tr              *i18n.TranslationSet

	StatsMutex deadlock.Mutex
}

// Remove removes the container
func (c *Container) Remove(force bool, removeVolumes bool) error {
	c.Log.Warn(fmt.Sprintf("removing container %s", c.Name))
	ctx := context.Background()
	if err := c.Runtime.RemoveContainer(ctx, c.ID, force, removeVolumes); err != nil {
		if strings.Contains(err.Error(), "Stop the container before attempting removal or force remove") ||
			strings.Contains(err.Error(), "container is running") {
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
	ctx := context.Background()
	return c.Runtime.StopContainer(ctx, c.ID, nil)
}

// Pause pauses the container
func (c *Container) Pause() error {
	c.Log.Warn(fmt.Sprintf("pausing container %s", c.Name))
	ctx := context.Background()
	return c.Runtime.PauseContainer(ctx, c.ID)
}

// Unpause unpauses the container
func (c *Container) Unpause() error {
	c.Log.Warn(fmt.Sprintf("unpausing container %s", c.Name))
	ctx := context.Background()
	return c.Runtime.UnpauseContainer(ctx, c.ID)
}

// Restart restarts the container
func (c *Container) Restart() error {
	c.Log.Warn(fmt.Sprintf("restarting container %s", c.Name))
	ctx := context.Background()
	return c.Runtime.RestartContainer(ctx, c.ID, nil)
}

// Attach attaches the container
func (c *Container) Attach() (*exec.Cmd, error) {
	if !c.DetailsLoaded() {
		return nil, errors.New(c.Tr.WaitingForContainerInfo)
	}

	// verify that we can in fact attach to this container
	if c.Details.Config != nil && !c.Details.Config.OpenStdin {
		return nil, errors.New(c.Tr.UnattachableContainerError)
	}

	if c.Summary.State == "exited" {
		return nil, errors.New(c.Tr.CannotAttachStoppedContainerError)
	}

	c.Log.Warn(fmt.Sprintf("attaching to container %s", c.Name))
	// Use podman attach command
	cmd := c.OSCommand.NewCmd("podman", "attach", "--sig-proxy=false", c.ID)
	return cmd, nil
}

// Top returns process information
func (c *Container) Top(ctx context.Context) (TopResponse, error) {
	details, err := c.Inspect()
	if err != nil {
		return TopResponse{}, err
	}

	// check container status
	if details.State == nil || !details.State.Running {
		return TopResponse{}, errors.New("container is not running")
	}

	titles, processes, err := c.Runtime.ContainerTop(ctx, c.ID)
	if err != nil {
		return TopResponse{}, err
	}

	return TopResponse{
		Titles:    titles,
		Processes: processes,
	}, nil
}

// Inspect returns details about the container
func (c *Container) Inspect() (*ContainerDetails, error) {
	ctx := context.Background()
	return c.Runtime.InspectContainer(ctx, c.ID)
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
	return c.Details != nil
}

// GetState returns the container state from the Summary
func (c *Container) GetState() string {
	return c.Summary.State
}
