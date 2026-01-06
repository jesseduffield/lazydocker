package presentation

import "github.com/christophe-duc/lazypodman/pkg/commands"

func GetProjectDisplayStrings(project *commands.Project) []string {
	return []string{project.Name}
}
