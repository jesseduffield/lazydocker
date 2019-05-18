package commands

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"golang.org/x/xerrors"
)

// Container : A git Container
type Container struct {
	Name          string
	ServiceName   string
	ID            string
	Container     types.Container
	DisplayString string
	Client        *client.Client
}

// GetDisplayStrings returns the dispaly string of Container
func (c *Container) GetDisplayStrings(isFocused bool) []string {
	return []string{utils.ColoredString(c.Container.State, c.GetColor()), utils.ColoredString(c.Name, color.FgWhite)}
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

// Remove removes a container
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
