package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/docker/docker/client"
	"github.com/go-errors/errors"
	"github.com/integrii/flaggy"
	"github.com/jesseduffield/lazydocker/pkg/app"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/jesseduffield/yaml"
	"github.com/samber/lo"
)

const DEFAULT_VERSION = "unversioned"

var (
	commit      string
	version     = DEFAULT_VERSION
	date        string
	buildSource = "unknown"

	configFlag    = false
	debuggingFlag = false
	composeFiles  []string
)

func main() {
	updateBuildInfo()

	info := fmt.Sprintf(
		"%s\nDate: %s\nBuildSource: %s\nCommit: %s\nOS: %s\nArch: %s",
		version,
		date,
		buildSource,
		commit,
		runtime.GOOS,
		runtime.GOARCH,
	)

	flaggy.SetName("lazydocker")
	flaggy.SetDescription("The lazier way to manage everything docker")
	flaggy.DefaultParser.AdditionalHelpPrepend = "https://github.com/jesseduffield/lazydocker"

	flaggy.Bool(&configFlag, "c", "config", "Print the current default config")
	flaggy.Bool(&debuggingFlag, "d", "debug", "a boolean")
	flaggy.StringSlice(&composeFiles, "f", "file", "Specify alternate compose files")
	flaggy.SetVersion(info)

	flaggy.Parse()

	if configFlag {
		var buf bytes.Buffer
		encoder := yaml.NewEncoder(&buf)
		err := encoder.Encode(config.GetDefaultConfig())
		if err != nil {
			log.Fatal(err.Error())
		}
		fmt.Printf("%v\n", buf.String())
		os.Exit(0)
	}

	projectDir, err := os.Getwd()
	if err != nil {
		log.Fatal(err.Error())
	}

	appConfig, err := config.NewAppConfig("lazydocker", version, commit, date, buildSource, debuggingFlag, composeFiles, projectDir)
	if err != nil {
		log.Fatal(err.Error())
	}

	app, err := app.NewApp(appConfig)
	if err == nil {
		err = app.Run()
	}
	app.Close()

	if err != nil {
		if errMessage, known := app.KnownError(err); known {
			log.Println(errMessage)
			os.Exit(0)
		}

		if client.IsErrConnectionFailed(err) {
			log.Println(app.Tr.ConnectionFailed)
			os.Exit(0)
		}

		newErr := errors.Wrap(err, 0)
		stackTrace := newErr.ErrorStack()
		app.Log.Error(stackTrace)

		log.Fatalf("%s\n\n%s", app.Tr.ErrorOccurred, stackTrace)
	}
}

func updateBuildInfo() {
	if version == DEFAULT_VERSION {
		if buildInfo, ok := debug.ReadBuildInfo(); ok {
			revision, ok := lo.Find(buildInfo.Settings, func(setting debug.BuildSetting) bool {
				return setting.Key == "vcs.revision"
			})
			if ok {
				commit = revision.Value
				// if lazydocker was built from source we'll show the version as the
				// abbreviated commit hash
				version = utils.SafeTruncate(revision.Value, 7)
			}

			// if version hasn't been set we assume that neither has the date
			time, ok := lo.Find(buildInfo.Settings, func(setting debug.BuildSetting) bool {
				return setting.Key == "vcs.time"
			})
			if ok {
				date = time.Value
			}
		}
	}
}
