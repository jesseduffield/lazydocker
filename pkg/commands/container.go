package commands

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/lazydocker/pkg/config"
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
	Container       types.Container
	DisplayString   string
	Client          *client.Client
	OSCommand       *OSCommand
	Config          *config.AppConfig
	Log             *logrus.Entry
	StatHistory     []*RecordedStats
	Details         types.ContainerJSON
	MonitoringStats bool
	DockerCommand   LimitedDockerCommand
	Tr              *i18n.TranslationSet

	StatsMutex sync.Mutex
}

// GetDisplayStrings returns the dispaly string of Container
func (c *Container) GetDisplayStrings(isFocused bool) []string {
	image := strings.TrimPrefix(c.Container.Image, "sha256:")

	return []string{c.GetDisplayStatus(), c.GetDisplaySubstatus(), c.Name, c.GetDisplayCPUPerc(), utils.ColoredString(image, color.FgMagenta)}
}

// GetDisplayStatus returns the colored status of the container
func (c *Container) GetDisplayStatus() string {
	return utils.ColoredString(c.Container.State, c.GetColor())
}

// GetDisplayStatus returns the exit code if the container has exited, and the health status if the container is running (and has a health check)
func (c *Container) GetDisplaySubstatus() string {
	if !c.DetailsLoaded() {
		return ""
	}

	switch c.Container.State {
	case "exited":
		return utils.ColoredString(
			fmt.Sprintf("(%s)", strconv.Itoa(c.Details.State.ExitCode)), c.GetColor(),
		)
	case "running":
		return c.getHealthStatus()
	default:
		return ""
	}
}

func (c *Container) getHealthStatus() string {
	if !c.DetailsLoaded() {
		return ""
	}

	healthStatusColorMap := map[string]color.Attribute{
		"healthy":   color.FgGreen,
		"unhealthy": color.FgRed,
		"starting":  color.FgYellow,
	}

	if c.Details.State.Health == nil {
		return ""
	}
	healthStatus := c.Details.State.Health.Status
	if healthStatusColor, ok := healthStatusColorMap[healthStatus]; ok {
		return utils.ColoredString(fmt.Sprintf("(%s)", healthStatus), healthStatusColor)
	}
	return ""
}

// GetDisplayCPUPerc colors the cpu percentage based on how extreme it is
func (c *Container) GetDisplayCPUPerc() string {
	stats, ok := c.getLastStats()
	if !ok {
		return ""
	}

	percentage := stats.DerivedStats.CPUPercentage
	formattedPercentage := fmt.Sprintf("%.2f%%", stats.DerivedStats.CPUPercentage)

	var clr color.Attribute
	if percentage > 90 {
		clr = color.FgRed
	} else if percentage > 50 {
		clr = color.FgYellow
	} else {
		clr = color.FgWhite
	}

	return utils.ColoredString(formattedPercentage, clr)
}

// ProducingLogs tells us whether we should bother checking a container's logs
func (c *Container) ProducingLogs() bool {
	return c.Container.State == "running" && c.DetailsLoaded() && c.Details.HostConfig.LogConfig.Type != "none"
}

// GetColor Container color
func (c *Container) GetColor() color.Attribute {
	switch c.Container.State {
	case "exited":
		// This means the colour may be briefly yellow and then switch to red upon starting
		// Not sure what a better alternative is.
		if !c.DetailsLoaded() || c.Details.State.ExitCode == 0 {
			return color.FgYellow
		}
		return color.FgRed
	case "created":
		return color.FgCyan
	case "running":
		return color.FgGreen
	case "paused":
		return color.FgYellow
	case "dead":
		return color.FgRed
	case "restarting":
		return color.FgBlue
	case "removing":
		return color.FgMagenta
	default:
		return color.FgWhite
	}
}

// Remove removes the container
func (c *Container) Remove(options types.ContainerRemoveOptions) error {
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
	return c.Client.ContainerStop(context.Background(), c.ID, nil)
}

// Restart restarts the container
func (c *Container) Restart() error {
	c.Log.Warn(fmt.Sprintf("restarting container %s", c.Name))
	return c.Client.ContainerRestart(context.Background(), c.ID, nil)
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
	cmd := c.OSCommand.PrepareSubProcess("docker", "attach", "--sig-proxy=false", c.ID)
	return cmd, nil
}

// Top returns process information
func (c *Container) Top() (container.ContainerTopOKBody, error) {
	detail, err := c.Inspect()
	if err != nil {
		return container.ContainerTopOKBody{}, err
	}

	// check container status
	if !detail.State.Running {
		return container.ContainerTopOKBody{}, errors.New("container is not running")
	}

	return c.Client.ContainerTop(context.Background(), c.ID, []string{})
}

// PruneContainers prunes containers
func (c *DockerCommand) PruneContainers() error {
	_, err := c.Client.ContainersPrune(context.Background(), filters.Args{})
	return err
}

// Inspect returns details about the container
func (c *Container) Inspect() (types.ContainerJSON, error) {
	return c.Client.ContainerInspect(context.Background(), c.ID)
}

// RenderTop returns details about the container
func (c *Container) RenderTop() (string, error) {
	result, err := c.Top()
	if err != nil {
		return "", err
	}

	return utils.RenderTable(append([][]string{result.Titles}, result.Processes...))
}

// DetailsLoaded tells us whether we have yet loaded the details for a container. Because this is an asynchronous operation, sometimes we have the container before we have its details.
func (c *Container) DetailsLoaded() bool {
	return c.Details.ContainerJSONBase != nil
}
