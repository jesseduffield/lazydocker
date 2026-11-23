package commands

import (
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/sirupsen/logrus"
)

// newTestDockerCommand assembles a DockerCommand suitable for unit-testing the
// profile-discovery and command-templating logic in isolation. It populates
// just the fields those code paths touch: Config (with default user templates),
// Log, and the runOutput injection seam used in place of the real OSCommand
// shellout.
func newTestDockerCommand(runOutput func(string) (string, error)) *DockerCommand {
	userCfg := config.GetDefaultConfig()
	return &DockerCommand{
		Log:                    logrus.NewEntry(logrus.New()),
		Config:                 &config.AppConfig{UserConfig: &userCfg},
		InDockerComposeProject: true,
		runOutput:              runOutput,
	}
}
