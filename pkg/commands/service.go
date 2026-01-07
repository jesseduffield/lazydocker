package commands

import (
	"context"
	"os/exec"

	"github.com/christophe-duc/lazypodman/pkg/utils"
	"github.com/sirupsen/logrus"
)

// Service represents a compose service
type Service struct {
	Name          string
	ID            string
	OSCommand     *OSCommand
	Log           *logrus.Entry
	Container     *Container
	PodmanCommand LimitedPodmanCommand
}

// Remove removes the service's containers
func (s *Service) Remove(force bool, removeVolumes bool) error {
	return s.Container.Remove(force, removeVolumes)
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

// Start starts the service
func (s *Service) Start() error {
	return s.runCommand(s.OSCommand.Config.UserConfig.CommandTemplates.StartService)
}

func (s *Service) runCommand(templateCmdStr string) error {
	command := utils.ApplyTemplate(
		templateCmdStr,
		s.PodmanCommand.NewCommandObject(CommandObject{Service: s}),
	)
	return s.OSCommand.RunCommand(command)
}

// Attach attaches to the service
func (s *Service) Attach() (*exec.Cmd, error) {
	return s.Container.Attach()
}

// ViewLogs attaches to a subprocess viewing the service's logs
func (s *Service) ViewLogs() (*exec.Cmd, error) {
	templateString := s.OSCommand.Config.UserConfig.CommandTemplates.ViewServiceLogs
	command := utils.ApplyTemplate(
		templateString,
		s.PodmanCommand.NewCommandObject(CommandObject{Service: s}),
	)

	cmd := s.OSCommand.ExecutableFromString(command)
	s.OSCommand.PrepareForChildren(cmd)

	return cmd, nil
}

// RenderTop renders the process list of the service
func (s *Service) RenderTop(ctx context.Context) (string, error) {
	templateString := s.OSCommand.Config.UserConfig.CommandTemplates.ServiceTop
	command := utils.ApplyTemplate(
		templateString,
		s.PodmanCommand.NewCommandObject(CommandObject{Service: s}),
	)

	return s.OSCommand.RunCommandWithOutputContext(ctx, command)
}
