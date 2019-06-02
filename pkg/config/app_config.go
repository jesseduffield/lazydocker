package config

import (
	"io/ioutil"
	"path/filepath"

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
	Gui              GuiConfig
	Reporting        string
	ConfirmOnQuit    bool
	CommandTemplates CommandTemplatesConfig
	OS               OSConfig
	Update           UpdateConfig
}

type ThemeConfig struct {
	ActiveBorderColor   []string
	InactiveBorderColor []string
	OptionsTextColor    []string
}

type GuiConfig struct {
	ScrollHeight     int
	ScrollPastBottom bool
	MouseEvents      bool
	Theme            ThemeConfig
}

type CommandTemplatesConfig struct {
	RestartService string
	DockerCompose  string
	StopService    string
}

type OSConfig struct {
	OpenCommand     string
	OpenLinkCommand string
}

type UpdateConfig struct {
	Method string
}

// GetDefaultConfig returns the application default configuration
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
		},
		Reporting:     "undetermined",
		ConfirmOnQuit: false,
		CommandTemplates: CommandTemplatesConfig{
			RestartService: "docker-compose restart {{ .Name }}",
			DockerCompose:  "apdev compose",
			StopService:    "apdev stop {{ .Name }}",
		},
		OS: GetPlatformDefaultConfig(),
		Update: UpdateConfig{
			Method: "never",
		},
	}
}

// AppState stores data between runs of the app like when the last update check
// was performed and which other repos have been checked out
type AppState struct {
	LastUpdateCheck int64
}

func getDefaultAppState() []byte {
	return []byte(`
    lastUpdateCheck: 0
  `)
}

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
