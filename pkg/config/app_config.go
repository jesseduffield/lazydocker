package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	yaml "github.com/jesseduffield/yaml"
	"github.com/shibukawa/configdir"
)

// AppConfig contains the base configuration fields required for lazygit.
type AppConfig struct {
	Debug       bool   `long:"debug" env:"DEBUG" default:"false"`
	Version     string `long:"version" env:"VERSION" default:"unversioned"`
	Commit      string `long:"commit" env:"COMMIT"`
	BuildDate   string `long:"build-date" env:"BUILD_DATE"`
	Name        string `long:"name" env:"NAME" default:"lazygit"`
	BuildSource string `long:"build-source" env:"BUILD_SOURCE" default:""`
	UserConfig  *UserConfig
	ConfigDir   string
}

// UserConfig holds all of the user-configurable options. The fields here are all in PascalCase but in your actual config.yml they'll be in camelCase. You can view the default config with `lazydocker --config` and you can open the config file with 'o' when the status panel is focused, or use 'e' to edit it in your chosen editor. Be careful: if for example you set a `commandTemplates:` yaml key but then give it no child values, it will scrap all of the defaults and the app will probably crash
type UserConfig struct {
	// Gui is for configuring visual things like colors and whether we show or hide things
	Gui GuiConfig `yaml:"gui,omitempty"`

	// Reporting determines whether events are reported such as errors (and maybe application opens but I'm not decided on that yet because it sounds kinda creepy but I also would love to know how many people are using this program)
	Reporting string `yaml:"reporting,omitempty"`

	// ConfirmOnQuit when enabled prompts you to confirm you want to quit when you hit esc or q when no confirmation panels are open
	ConfirmOnQuit bool `yaml:"confirmOnQuit,omitempty"`

	// CommandTemplates determines what commands actually get called when we run certain commands
	CommandTemplates CommandTemplatesConfig `yaml:"commandTemplates,omitempty"`

	// CustomCommands determines what shows up in your custom commands menu when you press 'c'. You can use go templates to access three items on the struct: the DockerCompose command (defaulted to 'docker-compose'), the Service if present, and the Container if present. The struct types for those are found in the commands package
	CustomCommands CustomCommands `yaml:"customCommands,omitempty"`

	// OS determines what defaults are set for opening files and links
	OS OSConfig `yaml:"oS,omitempty"`

	// Update is currently not being used, but like lazygit, it may be used down the line to help you update automatically.
	Update UpdateConfig `yaml:"update,omitempty"`

	// Stats determines how long lazydocker will gather container stats for, and what stat info to graph
	Stats StatsConfig `yaml:"stats,omitempty"`
}

// ThemeConfig is for setting the colors of panels and some text.
type ThemeConfig struct {
	ActiveBorderColor   []string `yaml:"activeBorderColor,omitempty"`
	InactiveBorderColor []string `yaml:"inactiveBorderColor,omitempty"`
	OptionsTextColor    []string `yaml:"optionsTextColor,omitempty"`
}

// GuiConfig is for configuring visual things like colors and whether we show or hide things
type GuiConfig struct {
	// ScrollHeight determines how many characters you scroll at a time when scrolling the main panel
	ScrollHeight int `yaml:"scrollHeight,omitempty"`

	// ScrollPastBottom determines whether you can scroll past the bottom of the main view
	ScrollPastBottom bool `yaml:"scrollPastBottom,omitempty"`

	// MouseEvents is experimental at this point. It's purpose is to allow you to focus panels by clicking on them, and allow for clicking on links
	MouseEvents bool `yaml:"mouseEvents,omitempty"`

	// Theme determines what colors and color attributes your panel borders have. I always set inactiveBorderColor to black because in my terminal it's more of a grey, but that doesn't work in your average terminal. I highly recommended finding a combination that works for you
	Theme ThemeConfig `yaml:"theme,omitempty"`

	// ShowAllContainers determines whether the Containers panel contains all the containers returned by `docker ps -a`, or just those containers that aren't directly linked to a service. It is probably desirable to enable this if you have multiple containers per service, but otherwise it can cause a lot of clutter
	ShowAllContainers bool `yaml:"showAllContainers,omitempty"`
}

// CommandTemplatesConfig determines what commands actually get called when we run certain commands
type CommandTemplatesConfig struct {
	// RestartService is for restarting a service. docker-compose restart {{ .Service.Name }} works but I prefer docker-compose up --force-recreate {{ .Service.Name }}
	RestartService string `yaml:"restartService,omitempty"`

	// DockerCompose is for your docker-compose command. You may want to combine a few different docker-compose.yml files together, in which case you can set this to "docker-compose -f foo/docker-compose.yml -f bah/docker-compose.yml". The reason that the other docker-compose command templates all start with {{ .DockerCompose }} is so that they can make use of whatever you've set in this value rather than you having to copy and paste it to all the other commands
	DockerCompose string `yaml:"dockerCompose,omitempty"`

	// StopService is the command for stopping a service
	StopService string `yaml:"stopService,omitempty"`

	// ServiceLogs get the logs for a service. This is actually not currently used; we just get the logs of the corresponding container. But we should probably support explicitly returning the logs of the service when you've selected the service, given that a service may have multiple containers.
	ServiceLogs string `yaml:"serviceLogs,omitempty"`

	// ViewServiceLogs is for when you want to view the logs of a service as a subprocess. This defaults to having no filter, unlike the in-app logs commands which will usually filter down to the last hour for the sake of performance.
	ViewServiceLogs string `yaml:"viewServiceLogs,omitempty"`

	// RebuildService is the command for rebuilding a service. Defaults to something along the lines of `{{ .DockerCompose }} up --build {{ .Service.Name }}`
	RebuildService string `yaml:"rebuildService,omitempty"`

	// RecreateService is for force-recreating a service. I prefer this to restarting a service because it will also restart any dependent services and ensure they're running before trying to run the service at hand
	RecreateService string `yaml:"recreateService,omitempty"`

	// ViewContainerLogs is like ViewServiceLogs but for containers
	ViewContainerLogs string `yaml:"viewContainerLogs,omitempty"`

	// ContainerLogs shows the logs of a container. By default this restricts the output to (as of right now) the last hour. This is for the sake of performance, and you can feel free to change this
	ContainerLogs string `yaml:"containerLogs,omitempty"`

	// AllLogs is for showing what you get from doing `docker-compose logs`. It combines all the logs together
	AllLogs string `yaml:"allLogs,omitempty"`

	// ViewAllLogs is to AllLogs what ViewContainerLogs is to ContainerLogs. It's just the command we use when you want to see all logs in a subprocess with no filtering
	ViewAllLogs string `yaml:"viewAlLogs,omitempty"`

	// DockerComposeConfig is the command for viewing the config of your docker compose. It basically prints out the yaml from your docker-compose.yml file(s)
	DockerComposeConfig string `yaml:"dockerComposeConfig,omitempty"`

	// CheckDockerComposeConfig is what we use to check whether we are in a docker-compose context. If the command returns an error then we clearly aren't in a docker-compose config and we then just hide the services panel and only show containers
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

// UpdateConfig is currently not being used, but may be used down the line to allow for automatic updates
type UpdateConfig struct {
	Method string `yaml:"method,omitempty"`
}

// GraphConfig specifies how to make a graph of recorded container stats
type GraphConfig struct {
	// Min sets the minimum value that you want to display. If you want to set this, you should also set MinType to "static". The reason for this is that if Min == 0, it's not clear if it has not been set (given that the zero-value of an int is 0) or if it's intentionally been set to 0.
	Min float64 `yaml:"min,omitempty"`

	// Max sets the maximum value that you want to display. If you want to set this, you should also set MaxType to "static". The reason for this is that if Max == 0, it's not clear if it has not been set (given that the zero-value of an int is 0) or if it's intentionally been set to 0.
	Max float64 `yaml:"max,omitempty"`

	// Height sets the height of the graph in ascii characters
	Height int `yaml:"height,omitempty"`

	// Caption sets the caption of the graph. If you want to show CPU Percentage you could set this to "CPU (%)"
	Caption string `yaml:"caption,omitempty"`

	// This is the path to the stat that you want to display. It is based on the RecordedStats struct in container_stats.go, so feel free to look there to see all the options available. Alternatively if you go into lazydocker and go to the stats tab, you'll see that same struct in JSON format, so you can just PascalCase the path and you'll have a valid path. E.g. ClientStats.blkio_stats -> "ClientStats.BlkioStats"
	StatPath string `yaml:"statPath,omitempty"`

	// This determines the color of the graph. This can be any color attribute, e.g. 'blue', 'green'
	Color string `yaml:"color,omitempty"`

	// MinType and MaxType are each one of "", "static". blank means the min/max of the data set will be used. "static" means the min/max specified will be used
	MinType string `yaml:"minType,omitempty"`

	// MaxType is just like MinType but for the max value
	MaxType string `yaml:"maxType,omitempty"`
}

// StatsConfig contains the stuff relating to stats and graphs
type StatsConfig struct {
	// Graphs contains the configuration for the stats graphs we want to show in the app
	Graphs []GraphConfig

	// MaxDuration tells us how long to collect stats for. Currently this defaults to "5m" i.e. 5 minutes.
	MaxDuration time.Duration `yaml:"maxDuration,omitempty"`
}

// CustomCommands contains the custom commands that you might want to use on any given service or container
type CustomCommands struct {
	// Containers contains the custom commands for containers
	Containers []CustomCommand `yaml:"containers,omitempty"`

	// Services contains the custom commands for services
	Services []CustomCommand `yaml:"services,omitempty"`
}

// CustomCommand is a template for a command we want to run against a service or container
type CustomCommand struct {
	// Attach tells us whether to switch to a subprocess to interact with the called program, or just read its output. If Attach is set to false, the command will run in the background. I'm open to the idea of having a third option where the output plays in the main panel.
	Attach bool

	// Command is the command we want to run. We can use the go templates here as well. One example might be `{{ .DockerCompose }} exec {{ .Service.Name }} /bin/sh`
	Command string
}

// GetDefaultConfig returns the application default configuration
// NOTE (to contributors, not users): do not default a boolean to true, because false is the boolean zero value and this will be ignored when parsing the user's config
func GetDefaultConfig() UserConfig {
	return UserConfig{
		Gui: GuiConfig{
			ScrollHeight:     2,
			ScrollPastBottom: false,
			MouseEvents:      false,
			Theme: ThemeConfig{
				ActiveBorderColor:   []string{"green", "bold"},
				InactiveBorderColor: []string{"white"},
				OptionsTextColor:    []string{"blue"},
			},
			ShowAllContainers: false,
		},
		Reporting:     "undetermined",
		ConfirmOnQuit: false,
		CommandTemplates: CommandTemplatesConfig{
			DockerCompose:            "docker-compose",
			RestartService:           "{{ .DockerCompose }} restart {{ .Service.Name }}",
			RebuildService:           "{{ .DockerCompose }} up -d --build {{ .Service.Name }}",
			RecreateService:          "{{ .DockerCompose }} up -d --force-recreate {{ .Service.Name }}",
			StopService:              "{{ .DockerCompose }} stop {{ .Service.Name }}",
			ServiceLogs:              "{{ .DockerCompose }} logs --since=60m --follow {{ .Service.Name }}",
			ViewServiceLogs:          "{{ .DockerCompose }} logs --follow {{ .Service.Name }}",
			AllLogs:                  "{{ .DockerCompose }} logs --tail=300 --follow",
			ViewAllLogs:              "{{ .DockerCompose }} logs",
			DockerComposeConfig:      "{{ .DockerCompose }} config",
			CheckDockerComposeConfig: "{{ .DockerCompose }} config --quiet",
			ContainerLogs:            "docker logs --timestamps --follow --since=60m {{ .Container.ID }}",
			ViewContainerLogs:        "docker logs --timestamps --follow --since=60m {{ .Container.ID }}",
			ServiceTop:               "{{ .DockerCompose }} top {{ .Service.Name }}",
		},
		CustomCommands: CustomCommands{
			Containers: []CustomCommand{
				{
					Attach:  true,
					Command: "docker exec -it {{ .Container.ID }} /bin/sh",
				},
			},
			Services: []CustomCommand{},
		},
		OS: GetPlatformDefaultConfig(),
		Update: UpdateConfig{
			Method: "never",
		},
		Stats: StatsConfig{
			Graphs: []GraphConfig{
				{
					Caption:  "CPU (%)",
					StatPath: "DerivedStats.CPUPercentage",
					Color:    "blue",
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

// NewAppConfig makes a new app config
func NewAppConfig(name, version, commit, date string, buildSource string, debuggingFlag bool) (*AppConfig, error) {
	configDir, err := findOrCreateConfigDir(name)
	if err != nil {
		return nil, err
	}

	userConfig, err := loadUserConfigWithDefaults(configDir)
	if err != nil {
		return nil, err
	}

	appConfig := &AppConfig{
		Name:        name,
		Version:     version,
		Commit:      commit,
		BuildDate:   date,
		Debug:       true, // TODO: restore os.Getenv("DEBUG") == "TRUE"
		BuildSource: buildSource,
		UserConfig:  userConfig,
		ConfigDir:   configDir,
	}

	return appConfig, nil
}

func findOrCreateConfigDir(projectName string) (string, error) {
	configDirs := configdir.New("jesseduffield", projectName)
	folders := configDirs.QueryFolders(configdir.Global)

	if err := folders[0].CreateParentDir("foo"); err != nil {
		return "", err
	}

	return folders[0].Path, nil
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
// note that if you set a zero-value, it may be ignored e.g. a false or 0 or empty string
// this is because we are using the omitempty yaml directive so that we don't write a heap
// of zero values to the user's config.yml
func (c *AppConfig) WriteToUserConfig(updateConfig func(*UserConfig) error) error {
	userConfig, err := loadUserConfig(c.ConfigDir, &UserConfig{})
	if err != nil {
		return err
	}

	if err := updateConfig(userConfig); err != nil {
		return err
	}

	out, err := yaml.Marshal(userConfig)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(c.ConfigFilename(), out, 0666)
}

// ConfigFilename returns the filename of the current config file
func (c *AppConfig) ConfigFilename() string {
	return filepath.Join(c.ConfigDir, "config.yml")
}
