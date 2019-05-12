package commands

import (
	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// Container : A git Container
type Container struct {
	Name          string
	ServiceName   string
	ID            string
	Container     types.Container
	DisplayString string
}

// GetDisplayStrings returns the dispaly string of Container
func (c *Container) GetDisplayStrings(isFocused bool) []string {
	displayName := utils.ColoredString(c.Name, c.GetColor())

	return []string{c.ServiceName, displayName}
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
