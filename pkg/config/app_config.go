package config

import (
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/shibukawa/configdir"
	yaml "gopkg.in/yaml.v2"
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
	content, err := ioutil.ReadFile(filepath.Join(configDir, "config.yml"))
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(content, base); err != nil {
		return nil, err
	}

	return base, nil
}

type UserConfig struct {
	Gui              GuiConfig              `yaml:"gui,omitempty"`
	Reporting        string                 `yaml:"reporting,omitempty"`
	ConfirmOnQuit    bool                   `yaml:"confirmOnQuit,omitempty"`
	CommandTemplates CommandTemplatesConfig `yaml:"commandTemplates,omitempty"`
	CustomCommands   CustomCommands         `yaml:"customCommands,omitempty"`
	OS               OSConfig               `yaml:"oS,omitempty"`
	Update           UpdateConfig           `yaml:"update,omitempty"`
	Stats            StatsConfig            `yaml:"stats,omitempty"`
}

type ThemeConfig struct {
	ActiveBorderColor   []string `yaml:"activeBorderColor,omitempty"`
	InactiveBorderColor []string `yaml:"inactiveBorderColor,omitempty"`
	OptionsTextColor    []string `yaml:"optionsTextColor,omitempty"`
}

type GuiConfig struct {
	ScrollHeight      int         `yaml:"scrollHeight,omitempty"`
	ScrollPastBottom  bool        `yaml:"scrollPastBottom,omitempty"`
	MouseEvents       bool        `yaml:"mouseEvents,omitempty"`
	Theme             ThemeConfig `yaml:"theme,omitempty"`
	ShowAllContainers bool        `yaml:"showAllContainers,omitempty"`
}

type CommandTemplatesConfig struct {
	RestartService  string `yaml:"restartService,omitempty"`
	DockerCompose   string `yaml:"dockerCompose,omitempty"`
	StopService     string `yaml:"stopService,omitempty"`
	ServiceLogs     string `yaml:"serviceLogs,omitempty"`
	ViewServiceLogs string `yaml:"viewServiceLogs,omitempty"`
	RebuildService  string `yaml:"rebuildService,omitempty"`

	// ViewContainerLogs is for viewing the container logs in a subprocess. We have this as a separate command in case you want to show all the logs rather than just tail them for the sake of reducing CPU load when in the lazydocker GUI
	ViewContainerLogs        string `yaml:"viewContainerLogs,omitempty"`
	ContainerLogs            string `yaml:"containerLogs,omitempty"`
	ContainerTTYLogs         string `yaml:"containerTTYLogs,omitempty"`
	AllLogs                  string `yaml:"allLogs,omitempty"`
	ViewAllLogs              string `yaml:"viewAlLogs,omitempty"`
	DockerComposeConfig      string `yaml:"dockerComposeConfig,omitempty"`
	CheckDockerComposeConfig string `yaml:"checkDockerComposeConfig,omitempty"`
	ServiceTop               string `yaml:"serviceTop,omitempty"`
}

type OSConfig struct {
	OpenCommand     string `yaml:"openCommand,omitempty"`
	OpenLinkCommand string `yaml:"openLinkCommand,omitempty"`
}

type UpdateConfig struct {
	Method string `yaml:"method,omitempty"`
}

// GraphConfig specifies how to make a graph of recorded container stats
type GraphConfig struct {
	Min      float64 `yaml:"min,omitempty"`
	Max      float64 `yaml:"max,omitempty"`
	Height   int     `yaml:"height,omitempty"`
	Caption  string  `yaml:"caption,omitempty"`
	StatPath string  `yaml:"statPath,omitempty"`
	Color    string  `yaml:"color,omitempty"`
	// MinType and MaxType are each one of "", "static". blank means the min/max of the data set will be used. "static" means the min/max specified will be used
	MinType string `yaml:"minType,omitempty"`
	MaxType string `yaml:"maxType,omitempty"`
}

type StatsConfig struct {
	Graphs      []GraphConfig
	MaxDuration time.Duration `yaml:"maxDuration,omitempty"`
}

type CustomCommands struct {
	Containers []CustomCommand `yaml:"containers,omitempty"`
	Services   []CustomCommand `yaml:"services,omitempty"`
}

type CustomCommand struct {
	Attach  bool
	Command string
}

// GetDefaultConfig returns the application default configuration
// NOTE: do not default a boolean to true, because false is the boolean zero value and this will be ignored when parsing the user's config
func GetDefaultConfig() UserConfig {
	return UserConfig{
		Gui: GuiConfig{
			ScrollHeight:     2,
			ScrollPastBottom: false,
			MouseEvents:      false,
			Theme: ThemeConfig{
				ActiveBorderColor:   []string{"white", "bold"},
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
			StopService:              "{{ .DockerCompose }} stop {{ .Service.Name }}",
			ServiceLogs:              "{{ .DockerCompose }} logs --since=60m --follow {{ .Service.Name }}",
			ViewServiceLogs:          "{{ .DockerCompose }} logs --follow {{ .Service.Name }}",
			AllLogs:                  "{{ .DockerCompose }} logs --since=60m --follow",
			ViewAllLogs:              "{{ .DockerCompose }} logs",
			DockerComposeConfig:      "{{ .DockerCompose }} config",
			CheckDockerComposeConfig: "{{ .DockerCompose }} config --quiet",
			ContainerLogs:            "docker logs --timestamps --follow --since=60m {{ .Container.ID }}",
			ViewContainerLogs:        "docker logs --timestamps --follow --since=60m {{ .Container.ID }}",
			ContainerTTYLogs:         "docker logs --follow --since=60m {{ .Container.ID }}",
			ServiceTop:               "{{ .DockerCompose }} top {{ .Service.Name }}",
		},
		CustomCommands: CustomCommands{
			Containers: []CustomCommand{
				{
					Attach:  true,
					Command: "docker exec -it {{ .Container.ID }} /bin/bash",
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
					Min:      0,
					Max:      100,
					Height:   10,
					Caption:  "CPU (%)",
					StatPath: "DerivedStats.CPUPercentage",
					Color:    "blue",
				},
				{
					Min:      0,
					Max:      100,
					Height:   10,
					Caption:  "Memory (%)",
					StatPath: "DerivedStats.MemoryPercentage",
					Color:    "green",
				},
			},
		},
	}
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

func (c *AppConfig) ConfigFilename() string {
	return filepath.Join(c.ConfigDir, "config.yml")
}
