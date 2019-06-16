package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/docker/docker/client"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/lazydocker/pkg/app"
	"github.com/jesseduffield/lazydocker/pkg/config"
)

var (
	commit      string
	version     = "unversioned"
	date        string
	buildSource = "unknown"

	configFlag    = flag.Bool("config", false, "Print the current default config")
	debuggingFlag = flag.Bool("debug", false, "a boolean")
	versionFlag   = flag.Bool("v", false, "Print the current version")
)

func main() {
	flag.Parse()
	if *versionFlag {
		fmt.Printf("commit=%s, build date=%s, build source=%s, version=%s, os=%s, arch=%s\n", commit, date, buildSource, version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	if *configFlag {
		fmt.Printf("%s\n", config.GetDefaultConfig())
		os.Exit(0)
	}

	// for now we're always in debug mode so we're not passing *debuggingFlag
	*debuggingFlag = true

	appConfig, err := config.NewAppConfig("lazydocker", version, commit, date, buildSource, *debuggingFlag)
	if err != nil {
		log.Fatal(err.Error())
	}

	app, err := app.NewApp(appConfig)

	if err == nil {
		err = app.Run()
	}

	if err != nil {
		if client.IsErrConnectionFailed(err) {
			log.Println(app.Tr.ConnectionFailed)
			os.Exit(0)
		}

		newErr := errors.Wrap(err, 0)
		stackTrace := newErr.ErrorStack()
		app.Log.Error(stackTrace)

		log.Fatal(fmt.Sprintf("%s\n\n%s", app.Tr.ErrorOccurred, stackTrace))
	}
}
