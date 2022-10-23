package commands

import (
	"os/exec"

	"github.com/docker/docker/api/types/container"

	"github.com/docker/docker/api/types"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
)

// Service : A docker Service
type Service struct {
	Name          string
	ID            string
	OSCommand     *OSCommand
	Log           *logrus.Entry
	Container     *Container
	DockerCommand LimitedDockerCommand
}

// Remove removes the service's containers
func (s *Service) Remove(options types.ContainerRemoveOptions) error {
	return s.Container.Remove(options)
}

// Stop stops the service's containers
func (s *Service) Stop() error {
	return s.runCommand(s.OSCommand.Config.UserConfig.CommandTemplates.StopService)
}

// Up up's the service
func (s *Service) Up() error {
	return s.runCommand(s.OSCommand.Config.UserConfig.CommandTemplates.UpService)
}

// Restart restarts the service
func (s *Service) Restart() error {
	return s.runCommand(s.OSCommand.Config.UserConfig.CommandTemplates.RestartService)
}

// Restart starts the service
func (s *Service) Start() error {
	return s.runCommand(s.OSCommand.Config.UserConfig.CommandTemplates.StartService)
}

func (s *Service) runCommand(templateCmdStr string) error {
	command := utils.ApplyTemplate(
		templateCmdStr,
		s.DockerCommand.NewCommandObject(CommandObject{Service: s}),
	)
	return s.OSCommand.RunCommand(command)
}

// Attach attaches to the service
func (s *Service) Attach() (*exec.Cmd, error) {
	return s.Container.Attach()
}

// Top returns process information
func (s *Service) Top() (container.ContainerTopOKBody, error) {
	return s.Container.Top()
}

// ViewLogs attaches to a subprocess viewing the service's logs
func (s *Service) ViewLogs() (*exec.Cmd, error) {
	templateString := s.OSCommand.Config.UserConfig.CommandTemplates.ViewServiceLogs
	command := utils.ApplyTemplate(
		templateString,
		s.DockerCommand.NewCommandObject(CommandObject{Service: s}),
	)

	cmd := s.OSCommand.ExecutableFromString(command)
	s.OSCommand.PrepareForChildren(cmd)

	return cmd, nil
}

// RenderTop renders the process list of the service
func (s *Service) RenderTop() (string, error) {
	templateString := s.OSCommand.Config.UserConfig.CommandTemplates.ServiceTop
	command := utils.ApplyTemplate(
		templateString,
		s.DockerCommand.NewCommandObject(CommandObject{Service: s}),
	)

	return s.OSCommand.RunCommandWithOutput(command)
}
