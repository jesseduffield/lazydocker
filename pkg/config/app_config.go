package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/OpenPeeDeeP/xdg"
	"github.com/jesseduffield/yaml"
)

type UserConfig struct {
	Gui            GuiConfig      `yaml:"gui,omitempty"`
	ConfirmOnQuit  bool           `yaml:"confirmOnQuit,omitempty"`
	Logs           LogsConfig     `yaml:"logs,omitempty"`
	CustomCommands CustomCommands `yaml:"customCommands,omitempty"`
	BulkCommands   CustomCommands `yaml:"bulkCommands,omitempty"`
	OS             OSConfig       `yaml:"oS,omitempty"`
	Stats          StatsConfig    `yaml:"stats,omitempty"`
	Replacements   Replacements   `yaml:"replacements,omitempty"`
	Ignore         []string       `yaml:"ignore,omitempty"`
}

type ThemeConfig struct {
	ActiveBorderColor   []string `yaml:"activeBorderColor,omitempty"`
	InactiveBorderColor []string `yaml:"inactiveBorderColor,omitempty"`
	SelectedLineBgColor []string `yaml:"selectedLineBgColor,omitempty"`
	OptionsTextColor    []string `yaml:"optionsTextColor,omitempty"`
}

type GuiConfig struct {
	ScrollHeight               int         `yaml:"scrollHeight,omitempty"`
	Language                   string      `yaml:"language,omitempty"`
	ScrollPastBottom           bool        `yaml:"scrollPastBottom,omitempty"`
	IgnoreMouseEvents          bool        `yaml:"mouseEvents,omitempty"`
	Theme                      ThemeConfig `yaml:"theme,omitempty"`
	ShowAllContainers          bool        `yaml:"showAllContainers,omitempty"`
	ReturnImmediately          bool        `yaml:"returnImmediately,omitempty"`
	WrapMainPanel              bool        `yaml:"wrapMainPanel,omitempty"`
	LegacySortContainers       bool        `yaml:"legacySortContainers,omitempty"`
	SidePanelWidth             float64     `yaml:"sidePanelWidth"`
	ShowBottomLine             bool        `yaml:"showBottomLine"`
	ExpandFocusedSidePanel     bool        `yaml:"expandFocusedSidePanel"`
	ScreenMode                 string      `yaml:"screenMode,omitempty"`
	ContainerStatusHealthStyle string      `yaml:"containerStatusHealthStyle"`
	Border                     string      `yaml:"border"`
}

type GraphConfig struct {
	Min      float64 `yaml:"min,omitempty"`
	Max      float64 `yaml:"max,omitempty"`
	Height   int     `yaml:"height,omitempty"`
	Caption  string  `yaml:"caption,omitempty"`
	StatPath string  `yaml:"statPath,omitempty"`
	Color    string  `yaml:"color,omitempty"`
	MinType  string  `yaml:"minType,omitempty"`
	MaxType  string  `yaml:"maxType,omitempty"`
}

type StatsConfig struct {
	Graphs      []GraphConfig `yaml:"graphs"`
	MaxDuration time.Duration `yaml:"maxDuration,omitempty"`
}

type CustomCommands struct {
	Containers []CustomCommand `yaml:"containers,omitempty"`
	Images     []CustomCommand `yaml:"images,omitempty"`
	Volumes    []CustomCommand `yaml:"volumes,omitempty"`
	Networks   []CustomCommand `yaml:"networks,omitempty"`
}

type Replacements struct {
	ImageNamePrefixes map[string]string `yaml:"imageNamePrefixes,omitempty"`
}

type CustomCommand struct {
	Name             string       `yaml:"name"`
	Attach           bool         `yaml:"attach"`
	Shell            bool         `yaml:"shell"`
	Command          string       `yaml:"command"`
	InternalFunction func() error `yaml:"-"`
}

type LogsConfig struct {
	Timestamps bool   `yaml:"timestamps,omitempty"`
	Since      string `yaml:"since,omitempty"`
	Tail       string `yaml:"tail,omitempty"`
}

type OSConfig struct {
	OpenCommand     string `yaml:"openCommand,omitempty"`
	OpenLinkCommand string `yaml:"openLinkCommand,omitempty"`
}

func GetDefaultConfig() UserConfig {
	duration, err := time.ParseDuration("3m")
	if err != nil {
		panic(err)
	}

	return UserConfig{
		Gui: GuiConfig{
			ScrollHeight:      2,
			Language:          "auto",
			ScrollPastBottom:  false,
			IgnoreMouseEvents: false,
			Theme: ThemeConfig{
				ActiveBorderColor:   []string{"green", "bold"},
				InactiveBorderColor: []string{"default"},
				SelectedLineBgColor: []string{"blue"},
				OptionsTextColor:    []string{"blue"},
			},
			ShowAllContainers:          true,
			ReturnImmediately:          false,
			WrapMainPanel:              true,
			LegacySortContainers:       false,
			SidePanelWidth:             0.25,
			ShowBottomLine:             true,
			ExpandFocusedSidePanel:     false,
			ScreenMode:                 "normal",
			ContainerStatusHealthStyle: "long",
		},
		ConfirmOnQuit: false,
		Logs: LogsConfig{
			Timestamps: false,
			Since:      "60m",
			Tail:       "",
		},
		CustomCommands: CustomCommands{
			Containers: []CustomCommand{},
			Images:     []CustomCommand{},
			Volumes:    []CustomCommand{},
			Networks:   []CustomCommand{},
		},
		BulkCommands: CustomCommands{
			Containers: []CustomCommand{
				{
					Name:    "prune stopped",
					Command: "container prune",
				},
			},
			Images: []CustomCommand{
				{
					Name:    "prune dangling",
					Command: "container image prune",
				},
				{
					Name:    "prune all",
					Command: "container image prune -a",
				},
			},
			Volumes: []CustomCommand{
				{
					Name:    "prune",
					Command: "container volume prune",
				},
			},
			Networks: []CustomCommand{
				{
					Name:    "prune",
					Command: "container network prune",
				},
			},
		},
		OS: GetPlatformDefaultConfig(),
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
		Replacements: Replacements{
			ImageNamePrefixes: map[string]string{},
		},
	}
}

type AppConfig struct {
	Debug       bool   `long:"debug" env:"DEBUG" default:"false"`
	Version     string `long:"version" env:"VERSION" default:"unversioned"`
	Commit      string `long:"commit" env:"COMMIT"`
	BuildDate   string `long:"build-date" env:"BUILD_DATE"`
	Name        string `long:"name" env:"NAME" default:"lazyapple"`
	BuildSource string `long:"build-source" env:"BUILD_SOURCE" default:""`
	UserConfig  *UserConfig
	ConfigDir   string
	ProjectDir  string
	ProjectName string
}

func NewAppConfig(name, version, commit, date string, buildSource string, debuggingFlag bool, composeFiles []string, projectDir string, projectName string) (*AppConfig, error) {
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
		Debug:       debuggingFlag || os.Getenv("DEBUG") == "TRUE",
		BuildSource: buildSource,
		UserConfig:  userConfig,
		ConfigDir:   configDir,
		ProjectDir:  projectDir,
		ProjectName: projectName,
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

	err := os.MkdirAll(folder, 0o755)
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

	content, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(content, base); err != nil {
		return nil, err
	}

	return base, nil
}

func (c *AppConfig) WriteToUserConfig(updateConfig func(*UserConfig) error) error {
	userConfig, err := loadUserConfig(c.ConfigDir, &UserConfig{})
	if err != nil {
		return err
	}

	if err := updateConfig(userConfig); err != nil {
		return err
	}

	file, err := os.OpenFile(c.ConfigFilename(), os.O_WRONLY|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}

	return yaml.NewEncoder(file).Encode(userConfig)
}

func (c *AppConfig) ConfigFilename() string {
	return filepath.Join(c.ConfigDir, "config.yml")
}
