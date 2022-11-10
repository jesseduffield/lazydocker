package presentation

import "github.com/jesseduffield/lazydocker/pkg/commands"

func GetProjectDisplayStrings(project *commands.Project) []string {
	return []string{project.Name}
}
