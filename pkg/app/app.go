package app

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/log"
	"github.com/sirupsen/logrus"
)

// App struct
type App struct {
	closers []io.Closer

	Config        *config.AppConfig
	Log           *logrus.Entry
	OSCommand     *commands.OSCommand
	DockerCommand *commands.DockerCommand
	Gui           *gui.Gui
	Tr            *i18n.TranslationSet
	ErrorChan     chan error
}

// NewApp bootstrap a new application
func NewApp(config *config.AppConfig) (*App, error) {
	app := &App{
		closers:   []io.Closer{},
		Config:    config,
		ErrorChan: make(chan error),
	}
	var err error
	app.Log = log.NewLogger(config, "23432119147a4367abf7c0de2aa99a2d")
	app.Tr = i18n.NewTranslationSet(app.Log)
	app.OSCommand = commands.NewOSCommand(app.Log, config)

	// here is the place to make use of the docker-compose.yml file in the current directory

	app.DockerCommand, err = commands.NewDockerCommand(app.Log, app.OSCommand, app.Tr, app.Config, app.ErrorChan)
	if err != nil {
		return app, err
	}
	app.Gui, err = gui.NewGui(app.Log, app.DockerCommand, app.OSCommand, app.Tr, config, app.ErrorChan)
	if err != nil {
		return app, err
	}
	return app, nil
}

func (app *App) Run() error {
	err := waitForTerminalSpace()
	if err != nil {
		return err
	}
	err = app.Gui.RunWithSubprocesses()
	return err
}

func waitForTerminalSpace() error {
	// before we do anything, we need to check that we have some window space available
	width, height, err := terminal.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	if width > 0 && height > 0 {
		return nil
	}
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	select {
	case <-winch:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("there is no available terminal space")
	}
}

// Close closes any resources
func (app *App) Close() error {
	for _, closer := range app.closers {
		err := closer.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type errorMapping struct {
	originalError string
	newError      string
}

// KnownError takes an error and tells us whether it's an error that we know about where we can print a nicely formatted version of it rather than panicking with a stack trace
func (app *App) KnownError(err error) (string, bool) {
	errorMessage := err.Error()

	mappings := []errorMapping{
		{
			originalError: "Got permission denied while trying to connect to the Docker daemon socket",
			newError:      app.Tr.CannotAccessDockerSocketError,
		},
	}

	for _, mapping := range mappings {
		if strings.Contains(errorMessage, mapping.originalError) {
			return mapping.newError, true
		}
	}

	return "", false
}
