package app

import (
	"io"
	"strings"

	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/jesseduffield/lazycontainer/pkg/config"
	"github.com/jesseduffield/lazycontainer/pkg/gui"
	"github.com/jesseduffield/lazycontainer/pkg/i18n"
	"github.com/jesseduffield/lazycontainer/pkg/log"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
	"github.com/sirupsen/logrus"
)

type App struct {
	closers []io.Closer

	Config        *config.AppConfig
	Log           *logrus.Entry
	OSCommand     *commands.OSCommand
	ContainerCmd  *commands.ContainerCommand
	Gui           *gui.Gui
	Tr            *i18n.TranslationSet
	ErrorChan     chan error
}

func NewApp(config *config.AppConfig) (*App, error) {
	app := &App{
		closers:   []io.Closer{},
		Config:    config,
		ErrorChan: make(chan error),
	}
	var err error
	app.Log = log.NewLogger(config, "23432119147a4367abf7c0de2aa99a2d")
	app.Tr, err = i18n.NewTranslationSetFromConfig(app.Log, config.UserConfig.Gui.Language)
	if err != nil {
		return app, err
	}
	app.OSCommand = commands.NewOSCommand(app.Log, config)

	app.ContainerCmd, err = commands.NewContainerCommand(app.Log, app.OSCommand, app.Tr, app.Config, app.ErrorChan)
	if err != nil {
		return app, err
	}
	app.closers = append(app.closers, app.ContainerCmd)
	app.Gui, err = gui.NewGui(app.Log, app.ContainerCmd, app.OSCommand, app.Tr, config, app.ErrorChan)
	if err != nil {
		return app, err
	}
	return app, nil
}

func (app *App) Run() error {
	return app.Gui.Run()
}

func (app *App) Close() error {
	return utils.CloseMany(app.closers)
}

type errorMapping struct {
	originalError string
	newError      string
}

func (app *App) KnownError(err error) (string, bool) {
	errorMessage := err.Error()

	mappings := []errorMapping{
		{
			originalError: "container: command not found",
			newError:      "Apple Container is not installed. Please install it and try again.",
		},
		{
			originalError: "container system is not running",
			newError:      "Apple Container system is not running. Run 'container system start' first.",
		},
	}

	for _, mapping := range mappings {
		if strings.Contains(errorMessage, mapping.originalError) {
			return mapping.newError, true
		}
	}

	return "", false
}
