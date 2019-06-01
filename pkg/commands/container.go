package commands

import (
	"context"
	"os/exec"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
)

// Container : A docker Container
type Container struct {
	Name            string
	ServiceName     string
	ContainerNumber string // might make this an int in the future if need be
	ProjectName     string
	ID              string
	Container       types.Container
	DisplayString   string
	Client          *client.Client
	OSCommand       *OSCommand
	Log             *logrus.Entry
	Stats           ContainerCliStat
}

// ContainerCliStat is a stat object returned by the CLI docker stat command
type ContainerCliStat struct {
	BlockIO   string `json:"BlockIO"`
	CPUPerc   string `json:"CPUPerc"`
	Container string `json:"Container"`
	ID        string `json:"ID"`
	MemPerc   string `json:"MemPerc"`
	MemUsage  string `json:"MemUsage"`
	Name      string `json:"Name"`
	NetIO     string `json:"NetIO"`
	PIDs      string `json:"PIDs"`
}

// GetDisplayStrings returns the dispaly string of Container
func (c *Container) GetDisplayStrings(isFocused bool) []string {
	return []string{utils.ColoredString(c.Container.State, c.GetColor()), utils.ColoredString(c.Name, color.FgWhite), c.Stats.CPUPerc}
}

// GetColor Container color
func (c *Container) GetColor() color.Attribute {
	switch c.Container.State {
	case "exited":
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
	default:
		return color.FgWhite
	}
}

// MustStopContainer tells us that we must stop the container before removing it
const MustStopContainer = iota

// Remove removes the container
func (c *Container) Remove(options types.ContainerRemoveOptions) error {
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
	return c.Client.ContainerStop(context.Background(), c.ID, nil)
}

// Restart restarts the container
func (c *Container) Restart() error {
	return c.Client.ContainerRestart(context.Background(), c.ID, nil)
}

// RestartService restarts the container
func (c *Container) RestartService() error {
	templateString := c.OSCommand.Config.GetUserConfig().GetString("commandTemplates.restartService")
	command := utils.ApplyTemplate(templateString, c)
	return c.OSCommand.RunCommand(command)
}

// Attach attaches the container
func (c *Container) Attach() *exec.Cmd {
	cmd := c.OSCommand.PrepareSubProcess("docker", "attach", "--sig-proxy=false", c.ID)
	return cmd
}

// Top returns process information
func (c *Container) Top() (types.ContainerProcessList, error) {
	return c.Client.ContainerTop(context.Background(), c.ID, []string{})
}
