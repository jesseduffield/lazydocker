package app

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/heroku/rollrus"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/gui"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/updates"
	"github.com/shibukawa/configdir"
	"github.com/sirupsen/logrus"
)

// App struct
type App struct {
	closers []io.Closer

	Config        config.AppConfigurer
	Log           *logrus.Entry
	OSCommand     *commands.OSCommand
	DockerCommand *commands.DockerCommand
	Gui           *gui.Gui
	Tr            *i18n.Localizer
	Updater       *updates.Updater // may only need this on the Gui
}

func newProductionLogger(config config.AppConfigurer) *logrus.Logger {
	log := logrus.New()
	log.Out = ioutil.Discard
	log.SetLevel(logrus.ErrorLevel)
	return log
}

func globalConfigDir() string {
	configDirs := configdir.New("jesseduffield", "lazydocker")
	configDir := configDirs.QueryFolders(configdir.Global)[0]
	return configDir.Path
}

func getLogLevel() logrus.Level {
	strLevel := os.Getenv("LOG_LEVEL")
	level, err := logrus.ParseLevel(strLevel)
	if err != nil {
		return logrus.DebugLevel
	}
	return level
}

func newDevelopmentLogger(config config.AppConfigurer) *logrus.Logger {
	log := logrus.New()
	log.SetLevel(getLogLevel())
	file, err := os.OpenFile(filepath.Join(globalConfigDir(), "development.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic("unable to log to file") // TODO: don't panic (also, remove this call to the `panic` function)
	}
	log.SetOutput(file)
	return log
}

func newLogger(config config.AppConfigurer) *logrus.Entry {
	var log *logrus.Logger
	environment := "production"
	if config.GetDebug() || os.Getenv("DEBUG") == "TRUE" {
		environment = "development"
		log = newDevelopmentLogger(config)
	} else {
		log = newProductionLogger(config)
	}

	// highly recommended: tail -f development.log | humanlog
	// https://github.com/aybabtme/humanlog
	log.Formatter = &logrus.JSONFormatter{}

	if config.GetUserConfig().GetString("reporting") == "on" {
		// this isn't really a secret token: it only has permission to push new rollbar items
		hook := rollrus.NewHook("23432119147a4367abf7c0de2aa99a2d", environment) // using the same key that lazygit uses for now
		log.Hooks.Add(hook)
	}
	return log.WithFields(logrus.Fields{
		"debug":     config.GetDebug(),
		"version":   config.GetVersion(),
		"commit":    config.GetCommit(),
		"buildDate": config.GetBuildDate(),
	})
}

// NewApp bootstrap a new application
func NewApp(config config.AppConfigurer) (*App, error) {
	app := &App{
		closers: []io.Closer{},
		Config:  config,
	}
	var err error
	app.Log = newLogger(config)
	app.Tr = i18n.NewLocalizer(app.Log)
	app.OSCommand = commands.NewOSCommand(app.Log, config)

	app.Updater, err = updates.NewUpdater(app.Log, config, app.OSCommand, app.Tr)
	if err != nil {
		return app, err
	}

	// here is the place to make use of the docker-compose.yml file in the current directory

	app.DockerCommand, err = commands.NewDockerCommand(app.Log, app.OSCommand, app.Tr, app.Config)
	if err != nil {
		return app, err
	}
	app.Gui, err = gui.NewGui(app.Log, app.DockerCommand, app.OSCommand, app.Tr, config, app.Updater)
	if err != nil {
		return app, err
	}
	return app, nil
}

func (app *App) Run() error {
	return app.Gui.RunWithSubprocesses()
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
