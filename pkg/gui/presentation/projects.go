package presentation

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func GetProjectDisplayStrings(project *commands.Project) []string {
	if project.IsProfile {
		return []string{utils.ColoredString("profile ", color.FgCyan) + project.Name}
	}
	return []string{project.Name}
}
