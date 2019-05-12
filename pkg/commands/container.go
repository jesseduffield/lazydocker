package commands

import (
	"github.com/docker/docker/api/types"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// Container : A git Container
type Container struct {
	Name          string
	ID            string
	State         string
	Container     types.Container
	DisplayString string
}

// GetDisplayStrings returns the dispaly string of Container
func (b *Container) GetDisplayStrings(isFocused bool) []string {
	displayName := utils.ColoredString(b.Name, b.GetColor())

	return []string{displayName}
}

// GetColor Container color
func (b *Container) GetColor() color.Attribute {
	return color.FgWhite

	// todo: change color based on state.

	switch b.State {
	case "feature":
		return color.FgGreen
	case "bugfix":
		return color.FgYellow
	case "hotfix":
		return color.FgRed
	default:
		return color.FgWhite
	}
}
