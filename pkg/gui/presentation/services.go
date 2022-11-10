package presentation

import (
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

func GetServiceDisplayStrings(service *commands.Service) []string {
	if service.Container == nil {
		return []string{
			utils.ColoredString("none", color.FgBlue),
			"",
			service.Name,
			"",
			"",
			"",
		}
	}

	container := service.Container
	return []string{
		getContainerDisplayStatus(container),
		getContainerDisplaySubstatus(container),
		service.Name,
		getDisplayCPUPerc(container),
		utils.ColoredString(displayPorts(container), color.FgYellow),
		utils.ColoredString(displayContainerImage(container), color.FgMagenta),
	}
}
