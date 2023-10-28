package presentation

import "github.com/jesseduffield/lazydocker/pkg/commands"

func GetProjectDisplayStrings(project *commands.Project) []string {
	// TODO show status up down stop
	return []string{project.Compose.Name}
}
