package commands

import (
	"io"

	"github.com/christophe-duc/lazypodman/pkg/config"
	"github.com/christophe-duc/lazypodman/pkg/i18n"
	"github.com/sirupsen/logrus"
)

// This file exports dummy constructors for use by tests in other packages

// NewDummyOSCommand creates a new dummy OSCommand for testing
func NewDummyOSCommand() *OSCommand {
	return NewOSCommand(NewDummyLog(), NewDummyAppConfig())
}

// NewDummyAppConfig creates a new dummy AppConfig for testing
func NewDummyAppConfig() *config.AppConfig {
	appConfig := &config.AppConfig{
		Name:        "lazypodman",
		Version:     "unversioned",
		Commit:      "",
		BuildDate:   "",
		Debug:       false,
		BuildSource: "",
		UserConfig: &config.UserConfig{
			Gui:              config.GuiConfig{Language: "en"},
			CommandTemplates: config.CommandTemplatesConfig{PodmanCompose: "podman-compose"},
		},
	}
	return appConfig
}

// NewDummyLog creates a new dummy Log for testing
func NewDummyLog() *logrus.Entry {
	log := logrus.New()
	log.Out = io.Discard
	return log.WithField("test", "test")
}

// NewDummyPodmanCommand creates a new dummy PodmanCommand for testing
func NewDummyPodmanCommand() *PodmanCommand {
	return NewDummyPodmanCommandWithOSCommand(NewDummyOSCommand())
}

// NewDummyPodmanCommandWithOSCommand creates a new dummy PodmanCommand for testing
func NewDummyPodmanCommandWithOSCommand(osCommand *OSCommand) *PodmanCommand {
	newAppConfig := NewDummyAppConfig()
	return &PodmanCommand{
		Log:       NewDummyLog(),
		OSCommand: osCommand,
		Tr:        i18n.NewTranslationSet(NewDummyLog(), newAppConfig.UserConfig.Gui.Language),
		Config:    newAppConfig,
	}
}
