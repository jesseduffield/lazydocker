package commands

import (
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
)

type Project struct {
	Name          string
	IsProfile     bool
	OSCommand     *OSCommand
	Config        *config.AppConfig
	DockerCommand LimitedDockerCommand
}

// Up ups the project
func (p *Project) Up() error {
	commandTemplates := p.Config.UserConfig.CommandTemplates
	templateCmdStr := commandTemplates.Up
	if p.IsProfile {
		templateCmdStr = commandTemplates.UpProfile
	}
	return p.runCommand(templateCmdStr)
}

// Down downs the project
func (p *Project) Down() error {
	defer func() {
		if r := recover(); r != nil {
			p.OSCommand.Log.Error(r)
		}
	}()
	commandTemplates := p.Config.UserConfig.CommandTemplates
	templateCmdStr := commandTemplates.Down
	if p.IsProfile {
		templateCmdStr = commandTemplates.DownProfile
	}
	return p.runCommand(templateCmdStr)
}

// Restart restarts the project
func (p *Project) Restart() error {
	commandTemplates := p.Config.UserConfig.CommandTemplates
	templateCmdStr := commandTemplates.Restart
	if p.IsProfile {
		templateCmdStr = commandTemplates.RestartProfile
	}
	return p.runCommand(templateCmdStr)
}

// Run custom command on the project
func (p *Project) runCommand(templateCmdStr string) error {
	cmdObj := CommandObject{}
	if p.IsProfile {
		cmdObj.Profile = p.Name
	}

	command := utils.ApplyTemplate(
		templateCmdStr,
		p.DockerCommand.NewCommandObject(cmdObj),
	)
	// log command
	return p.OSCommand.RunCommand(command)
}
