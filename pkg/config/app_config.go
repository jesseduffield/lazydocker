// Package config handles all the user-configuration. The fields here are
// all in PascalCase but in your actual config.yml they'll be in camelCase.
// You can view the default config with `lazydocker --config`.
// You can open your config file by going to the status panel (using left-arrow)
// and pressing 'o'.
// You can directly edit the file (e.g. in vim) by pressing 'e' instead.
// To see the final config after your user-specific options have been merged
// with the defaults, go to the 'about' tab in the status panel.
// Because of the way we merge your user config with the defaults you may need
// to be careful: if for example you set a `commandTemplates:` yaml key but then
// give it no child values, it will scrap all of the defaults and the app will
// probably crash.
package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OpenPeeDeeP/xdg"
	yaml "github.com/jesseduffield/yaml"
)

// UserConfig holds all of the user-configurable options
type UserConfig struct {
	// Gui is for configuring visual things like colors and whether we show or
	// hide things
	Gui GuiConfig `yaml:"gui,omitempty"`

	// Reporting determines whether events are reported such as errors (and maybe
	// application opens but I'm not decided on that yet because it sounds kinda
	// creepy but I also would love to know how many people are using this
	// program)
	Reporting string `yaml:"reporting,omitempty"`

	// ConfirmOnQuit when enabled prompts you to confirm you want to quit when you
	// hit esc or q when no confirmation panels are open
	ConfirmOnQuit bool `yaml:"confirmOnQuit,omitempty"`

	// CommandTemplates determines what commands actually get called when we run
	// certain commands
	CommandTemplates CommandTemplatesConfig `yaml:"commandTemplates,omitempty"`

	// CustomCommands determines what shows up in your custom commands menu when
	// you press 'c'. You can use go templates to access three items on the
	// struct: the DockerCompose command (defaulted to 'docker-compose'), the
	// Service if present, and the Container if present. The struct types for
	// those are found in the commands package
	CustomCommands CustomCommands `yaml:"customCommands,omitempty"`

	// BulkCommands are commands that apply to all items in a panel e.g.
	// killing all containers, stopping all services, or pruning all images
	BulkCommands CustomCommands `yaml:"bulkCommands,omitempty"`

	// OS determines what defaults are set for opening files and links
	OS OSConfig `yaml:"oS,omitempty"`

	// UpdateConfig determines what the default settings are for updating the ui
	Update UpdateConfig `yaml:"update,omitempty"`

	// Stats determines how long lazydocker will gather container stats for, and
	// what stat info to graph
	Stats StatsConfig `yaml:"stats,omitempty"`
}

// ThemeConfig is for setting the colors of panels and some text.
type ThemeConfig struct {
	ActiveBorderColor   []string `yaml:"activeBorderColor,omitempty"`
	InactiveBorderColor []string `yaml:"inactiveBorderColor,omitempty"`
	OptionsTextColor    []string `yaml:"optionsTextColor,omitempty"`
}

// GuiConfig is for configuring visual things like colors and whether we show or
// hide things
type GuiConfig struct {
	// ScrollHeight determines how many characters you scroll at a time when
	// scrolling the main panel
	ScrollHeight int `yaml:"scrollHeight,omitempty"`

	// ScrollPastBottom determines whether you can scroll past the bottom of the
	// main view
	ScrollPastBottom bool `yaml:"scrollPastBottom,omitempty"`

	// IgnoreMouseEvents is for when you do not want to use your mouse to interact
	// with anything
	IgnoreMouseEvents bool `yaml:"mouseEvents,omitempty"`

	// Theme determines what colors and color attributes your panel borders have.
	// I always set inactiveBorderColor to black because in my terminal it's more
	// of a grey, but that doesn't work in your average terminal. I highly
	// recommended finding a combination that works for you
	Theme ThemeConfig `yaml:"theme,omitempty"`

	// ShowAllContainers determines whether the Containers panel contains all the
	// containers returned by `docker ps -a`, or just those containers that aren't
	// directly linked to a service. It is probably desirable to enable this if
	// you have multiple containers per service, but otherwise it can cause a lot
	// of clutter
	ShowAllContainers bool `yaml:"showAllContainers,omitempty"`

	// ReturnImmediately determines whether you get the 'press enter to return to
	// lazydocker' message after a subprocess has completed. You would set this to
	// true if you often want to see the output of subprocesses before returning
	// to lazydocker. I would default this to false but then people who want it
	// set to true won't even know the config option exists.
	ReturnImmediately bool `yaml:"returnImmediately,omitempty"`

	// WrapMainPanel determines whether we use word wrap on the main panel
	WrapMainPanel bool `yaml:"wrapMainPanel,omitempty"`
}

// CommandTemplatesConfig determines what commands actually get called when we
// run certain commands
type CommandTemplatesConfig struct {
	// RestartService is for restarting a service. docker-compose restart {{
	// .Service.Name }} works but I prefer docker-compose up --force-recreate {{
	// .Service.Name }}
	RestartService string `yaml:"restartService,omitempty"`

	// DockerCompose is for your docker-compose command. You may want to combine a
	// few different docker-compose.yml files together, in which case you can set
	// this to "docker-compose -f foo/docker-compose.yml -f
	// bah/docker-compose.yml". The reason that the other docker-compose command
	// templates all start with {{ .DockerCompose }} is so that they can make use
	// of whatever you've set in this value rather than you having to copy and
	// paste it to all the other commands
	DockerCompose string `yaml:"dockerCompose,omitempty"`

	// StopService is the command for stopping a service
	StopService string `yaml:"stopService,omitempty"`

	// ServiceLogs get the logs for a service. This is actually not currently
	// used; we just get the logs of the corresponding container. But we should
	// probably support explicitly returning the logs of the service when you've
	// selected the service, given that a service may have multiple containers.
	ServiceLogs string `yaml:"serviceLogs,omitempty"`

	// ViewServiceLogs is for when you want to view the logs of a service as a
	// subprocess. This defaults to having no filter, unlike the in-app logs
	// commands which will usually filter down to the last hour for the sake of
	// performance.
	ViewServiceLogs string `yaml:"viewServiceLogs,omitempty"`

	// RebuildService is the command for rebuilding a service. Defaults to
	// something along the lines of `{{ .DockerCompose }} up --build {{
	// .Service.Name }}`
	RebuildService string `yaml:"rebuildService,omitempty"`

	// RecreateService is for force-recreating a service. I prefer this to
	// restarting a service because it will also restart any dependent services
	// and ensure they're running before trying to run the service at hand
	RecreateService string `yaml:"recreateService,omitempty"`

	// ViewContainerLogs is like ViewServiceLogs but for containers
	ViewContainerLogs string `yaml:"viewContainerLogs,omitempty"`

	// ContainerLogs shows the logs of a container. By default this restricts the
	// output to (as of right now) the last hour. This is for the sake of
	// performance, and you can feel free to change this
	ContainerLogs string `yaml:"containerLogs,omitempty"`

	// AllLogs is for showing what you get from doing `docker-compose logs`. It
	// combines all the logs together
	AllLogs string `yaml:"allLogs,omitempty"`

	// ViewAllLogs is to AllLogs what ViewContainerLogs is to ContainerLogs. It's
	// just the command we use when you want to see all logs in a subprocess with
	// no filtering
	ViewAllLogs string `yaml:"viewAlLogs,omitempty"`

	// DockerComposeConfig is the command for viewing the config of your docker
	// compose. It basically prints out the yaml from your docker-compose.yml
	// file(s)
	DockerComposeConfig string `yaml:"dockerComposeConfig,omitempty"`

	// CheckDockerComposeConfig is what we use to check whether we are in a
	// docker-compose context. If the command returns an error then we clearly
	// aren't in a docker-compose config and we then just hide the services panel
	// and only show containers
	CheckDockerComposeConfig string `yaml:"checkDockerComposeConfig,omitempty"`

	// ServiceTop is the command for viewing the processes under a given service
	ServiceTop string `yaml:"serviceTop,omitempty"`
}

// OSConfig contains config on the level of the os
type OSConfig struct {
	// OpenCommand is the command for opening a file
	OpenCommand string `yaml:"openCommand,omitempty"`

	// OpenCommand is the command for opening a link
	OpenLinkCommand string `yaml:"openLinkCommand,omitempty"`
}

// UpdateConfig determines what the default settings are for updating the ui
type UpdateConfig struct {
	// RefreshProjectTime determines the time betweens updates of all continues docker commands like docker ps, docker images, etc.
	// It expects a valid duration like: 100ms, 2s, 200ns
	// for docs see: https://golang.org/pkg/time/#ParseDuration
	DockerRefreshInterval time.Duration `yaml:"dockerRefreshInterval,omitempty"`
}

// GraphConfig specifies how to make a graph of recorded container stats
type GraphConfig struct {
	// Min sets the minimum value that you want to display. If you want to set
	// this, you should also set MinType to "static". The reason for this is that
	// if Min == 0, it's not clear if it has not been set (given that the
	// zero-value of an int is 0) or if it's intentionally been set to 0.
	Min float64 `yaml:"min,omitempty"`

	// Max sets the maximum value that you want to display. If you want to set
	// this, you should also set MaxType to "static". The reason for this is that
	// if Max == 0, it's not clear if it has not been set (given that the
	// zero-value of an int is 0) or if it's intentionally been set to 0.
	Max float64 `yaml:"max,omitempty"`

	// Height sets the height of the graph in ascii characters
	Height int `yaml:"height,omitempty"`

	// Caption sets the caption of the graph. If you want to show CPU Percentage
	// you could set this to "CPU (%)"
	Caption string `yaml:"caption,omitempty"`

	// This is the path to the stat that you want to display. It is based on the
	// RecordedStats struct in container_stats.go, so feel free to look there to
	// see all the options available. Alternatively if you go into lazydocker and
	// go to the stats tab, you'll see that same struct in JSON format, so you can
	// just PascalCase the path and you'll have a valid path. E.g.
	// ClientStats.blkio_stats -> "ClientStats.BlkioStats"
	StatPath string `yaml:"statPath,omitempty"`

	// This determines the color of the graph. This can be any color attribute,
	// e.g. 'blue', 'green'
	Color string `yaml:"color,omitempty"`

	// MinType and MaxType are each one of "", "static". blank means the min/max
	// of the data set will be used. "static" means the min/max specified will be
	// used
	MinType string `yaml:"minType,omitempty"`

	// MaxType is just like MinType but for the max value
	MaxType string `yaml:"maxType,omitempty"`
}

// StatsConfig contains the stuff relating to stats and graphs
type StatsConfig struct {
	// Graphs contains the configuration for the stats graphs we want to show in
	// the app
	Graphs []GraphConfig

	// MaxDuration tells us how long to collect stats for. Currently this defaults
	// to "5m" i.e. 5 minutes.
	MaxDuration time.Duration `yaml:"maxDuration,omitempty"`
}

// CustomCommands contains the custom commands that you might want to use on any
// given service or container
type CustomCommands struct {
	// Containers contains the custom commands for containers
	Containers []CustomCommand `yaml:"containers,omitempty"`

	// Services contains the custom commands for services
	Services []CustomCommand `yaml:"services,omitempty"`

	// Images contains the custom commands for images
	Images []CustomCommand `yaml:"images,omitempty"`

	// Volumes contains the custom commands for volumes
	Volumes []CustomCommand `yaml:"volumes,omitempty"`
}

// CustomCommand is a template for a command we want to run against a service or
// container
type CustomCommand struct {
	// Name is the name of the command, purely for visual display
	Name string `yaml:"name"`

	// Attach tells us whether to switch to a subprocess to interact with the
	// called program, or just read its output. If Attach is set to false, the
	// command will run in the background. I'm open to the idea of having a third
	// option where the output plays in the main panel.
	Attach bool `yaml:"attach"`

	// Command is the command we want to run. We can use the go templates here as
	// well. One example might be `{{ .DockerCompose }} exec {{ .Service.Name }}
	// /bin/sh`
	Command string `yaml:"command"`

	// ServiceNames is used to restrict this command to just one or more services.
	// An example might be 'rails migrate' for your rails api service(s). This
	// field has no effect on customcommands under the 'communications' part of
	// the customCommand config.
	ServiceNames []string `yaml:"serviceNames"`

	// InternalFunction is the name of a function inside lazydocker that we want to run, as opposed to a command-line command. This is only used internally and can't be configured by the user
	InternalFunction func() error `yaml:"-"`
}

// GetDefaultConfig returns the application default configuration NOTE (to
// contributors, not users): do not default a boolean to true, because false is
// the boolean zero value and this will be ignored when parsing the user's
// config
func GetDefaultConfig() UserConfig {
	duration, err := time.ParseDuration("3m")
	if err != nil {
		panic(err)
	}

	return UserConfig{
		Gui: GuiConfig{
			ScrollHeight:      2,
			ScrollPastBottom:  false,
			IgnoreMouseEvents: false,
			Theme: ThemeConfig{
				ActiveBorderColor:   []string{"green", "bold"},
				InactiveBorderColor: []string{"default"},
				OptionsTextColor:    []string{"blue"},
			},
			ShowAllContainers: false,
			ReturnImmediately: false,
			WrapMainPanel:     false,
		},
		Reporting:     "undetermined",
		ConfirmOnQuit: false,
		CommandTemplates: CommandTemplatesConfig{
			DockerCompose:             "docker-compose",
			RestartService:            "{{ .DockerCompose }} restart {{ .Service.Name }}",
			RebuildService:            "{{ .DockerCompose }} up -d --build {{ .Service.Name }}",
			RecreateService:           "{{ .DockerCompose }} up -d --force-recreate {{ .Service.Name }}",
			RecreateServiceDropVolumes:"{{ .DockerCompose }} up -d --force-recreate --renew-anon-volumes {{ .Service.Name }}",
			StopService:               "{{ .DockerCompose }} stop {{ .Service.Name }}",
			ServiceLogs:               "{{ .DockerCompose }} logs --since=60m --follow {{ .Service.Name }}",
			ViewServiceLogs:           "{{ .DockerCompose }} logs --follow {{ .Service.Name }}",
			AllLogs:                   "{{ .DockerCompose }} logs --tail=300 --follow",
			ViewAllLogs:               "{{ .DockerCompose }} logs",
			DockerComposeConfig:       "{{ .DockerCompose }} config",
			CheckDockerComposeConfig:  "{{ .DockerCompose }} config --quiet",
			ContainerLogs:             "docker logs --timestamps --follow --since=60m {{ .Container.ID }}",
			ViewContainerLogs:         "docker logs --timestamps --follow --since=60m {{ .Container.ID }}",
			ServiceTop:                "{{ .DockerCompose }} top {{ .Service.Name }}",
		},
		CustomCommands: CustomCommands{
			Containers: []CustomCommand{
				{
					Name:    "bash",
					Command: "docker exec -it {{ .Container.ID }} /bin/sh",
					Attach:  true,
				},
			},
			Services: []CustomCommand{},
			Images:   []CustomCommand{},
			Volumes:  []CustomCommand{},
		},
		BulkCommands: CustomCommands{
			Services: []CustomCommand{
				{
					Name:    "up",
					Command: "{{ .DockerCompose }} up -d",
				},
				{
					Name:    "up (attached)",
					Command: "{{ .DockerCompose }} up",
					Attach:  true,
				},
				{
					Name:    "stop",
					Command: "{{ .DockerCompose }} stop",
				},
				{
					Name:    "pull",
					Command: "{{ .DockerCompose }} pull",
					Attach:  true,
				},
				{
					Name:    "build",
					Command: "{{ .DockerCompose }} build --parallel --force-rm",
					Attach:  true,
				},
				{
					Name:    "down",
					Command: "{{ .DockerCompose }} down",
				},
				{
					Name:    "down with volumes",
					Command: "{{ .DockerCompose }} down --volumes",
				},
				{
					Name:    "down with images",
					Command: "{{ .DockerCompose }} down --rmi all",
				},
				{
					Name:    "down with volumes and images",
					Command: "{{ .DockerCompose }} down --volumes --rmi all",
				},
			},
			Containers: []CustomCommand{},
			Images:     []CustomCommand{},
			Volumes:    []CustomCommand{},
		},
		OS: GetPlatformDefaultConfig(),
		Update: UpdateConfig{
			DockerRefreshInterval: time.Millisecond * 100,
		},
		Stats: StatsConfig{
			MaxDuration: duration,
			Graphs: []GraphConfig{
				{
					Caption:  "CPU (%)",
					StatPath: "DerivedStats.CPUPercentage",
					Color:    "cyan",
				},
				{
					Caption:  "Memory (%)",
					StatPath: "DerivedStats.MemoryPercentage",
					Color:    "green",
				},
			},
		},
	}
}

// AppConfig contains the base configuration fields required for lazydocker.
type AppConfig struct {
	Debug       bool   `long:"debug" env:"DEBUG" default:"false"`
	Version     string `long:"version" env:"VERSION" default:"unversioned"`
	Commit      string `long:"commit" env:"COMMIT"`
	BuildDate   string `long:"build-date" env:"BUILD_DATE"`
	Name        string `long:"name" env:"NAME" default:"lazydocker"`
	BuildSource string `long:"build-source" env:"BUILD_SOURCE" default:""`
	UserConfig  *UserConfig
	ConfigDir   string
	ProjectDir  string
}

// NewAppConfig makes a new app config
func NewAppConfig(name, version, commit, date string, buildSource string, debuggingFlag bool, composeFiles []string, projectDir string) (*AppConfig, error) {
	configDir, err := findOrCreateConfigDir(name)
	if err != nil {
		return nil, err
	}

	userConfig, err := loadUserConfigWithDefaults(configDir)
	if err != nil {
		return nil, err
	}

	// Pass compose files as individual -f flags to docker-compose
	if len(composeFiles) > 0 {
		userConfig.CommandTemplates.DockerCompose += " -f " + strings.Join(composeFiles, " -f ")
	}

	appConfig := &AppConfig{
		Name:        name,
		Version:     version,
		Commit:      commit,
		BuildDate:   date,
		Debug:       debuggingFlag || os.Getenv("DEBUG") == "TRUE",
		BuildSource: buildSource,
		UserConfig:  userConfig,
		ConfigDir:   configDir,
		ProjectDir:  projectDir,
	}

	return appConfig, nil
}

func configDirForVendor(vendor string, projectName string) string {
	envConfigDir := os.Getenv("CONFIG_DIR")
	if envConfigDir != "" {
		return envConfigDir
	}
	configDirs := xdg.New(vendor, projectName)
	return configDirs.ConfigHome()
}

func configDir(projectName string) string {
	legacyConfigDirectory := configDirForVendor("jesseduffield", projectName)
	if _, err := os.Stat(legacyConfigDirectory); !os.IsNotExist(err) {
		return legacyConfigDirectory
	}
	configDirectory := configDirForVendor("", projectName)
	return configDirectory
}

func findOrCreateConfigDir(projectName string) (string, error) {
	folder := configDir(projectName)

	err := os.MkdirAll(folder, 0755)
	if err != nil {
		return "", err
	}

	return folder, nil
}

func loadUserConfigWithDefaults(configDir string) (*UserConfig, error) {
	config := GetDefaultConfig()

	return loadUserConfig(configDir, &config)
}

func loadUserConfig(configDir string, base *UserConfig) (*UserConfig, error) {
	fileName := filepath.Join(configDir, "config.yml")

	if _, err := os.Stat(fileName); err != nil {
		if os.IsNotExist(err) {
			file, err := os.Create(fileName)
			if err != nil {
				return nil, err
			}
			file.Close()
		} else {
			return nil, err
		}
	}

	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(content, base); err != nil {
		return nil, err
	}

	return base, nil
}

// WriteToUserConfig allows you to set a value on the user config to be saved
// note that if you set a zero-value, it may be ignored e.g. a false or 0 or
// empty string this is because we are using the omitempty yaml directive so
// that we don't write a heap of zero values to the user's config.yml
func (c *AppConfig) WriteToUserConfig(updateConfig func(*UserConfig) error) error {
	userConfig, err := loadUserConfig(c.ConfigDir, &UserConfig{})
	if err != nil {
		return err
	}

	if err := updateConfig(userConfig); err != nil {
		return err
	}

	file, err := os.OpenFile(c.ConfigFilename(), os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}

	return yaml.NewEncoder(file).Encode(userConfig)
}

// ConfigFilename returns the filename of the current config file
func (c *AppConfig) ConfigFilename() string {
	return filepath.Join(c.ConfigDir, "config.yml")
}
