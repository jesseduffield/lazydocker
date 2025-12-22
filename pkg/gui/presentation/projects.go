package presentation

import "github.com/peauc/lazydocker-ng/pkg/commands"

func GetProjectDisplayStrings(project *commands.Project) []string {
	return []string{project.Name}
}
