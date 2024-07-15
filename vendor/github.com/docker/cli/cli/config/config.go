package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/credentials"
	"github.com/docker/cli/cli/config/types"
	"github.com/docker/docker/pkg/homedir"
	"github.com/pkg/errors"
)

const (
	// EnvOverrideConfigDir is the name of the environment variable that can be
	// used to override the location of the client configuration files (~/.docker).
	//
	// It takes priority over the default, but can be overridden by the "--config"
	// command line option.
	EnvOverrideConfigDir = "DOCKER_CONFIG"

	// ConfigFileName is the name of the client configuration file inside the
	// config-directory.
	ConfigFileName = "config.json"
	configFileDir  = ".docker"
	contextsDir    = "contexts"
)

var (
	initConfigDir = new(sync.Once)
	configDir     string
)

// resetConfigDir is used in testing to reset the "configDir" package variable
// and its sync.Once to force re-lookup between tests.
func resetConfigDir() {
	configDir = ""
	initConfigDir = new(sync.Once)
}

// Dir returns the directory the configuration file is stored in
func Dir() string {
	initConfigDir.Do(func() {
		configDir = os.Getenv(EnvOverrideConfigDir)
		if configDir == "" {
			configDir = filepath.Join(homedir.Get(), configFileDir)
		}
	})
	return configDir
}

// ContextStoreDir returns the directory the docker contexts are stored in
func ContextStoreDir() string {
	return filepath.Join(Dir(), contextsDir)
}

// SetDir sets the directory the configuration file is stored in
func SetDir(dir string) {
	// trigger the sync.Once to synchronise with Dir()
	initConfigDir.Do(func() {})
	configDir = filepath.Clean(dir)
}

// Path returns the path to a file relative to the config dir
func Path(p ...string) (string, error) {
	path := filepath.Join(append([]string{Dir()}, p...)...)
	if !strings.HasPrefix(path, Dir()+string(filepath.Separator)) {
		return "", errors.Errorf("path %q is outside of root config directory %q", path, Dir())
	}
	return path, nil
}

// LoadFromReader is a convenience function that creates a ConfigFile object from
// a reader
func LoadFromReader(configData io.Reader) (*configfile.ConfigFile, error) {
	configFile := configfile.ConfigFile{
		AuthConfigs: make(map[string]types.AuthConfig),
	}
	err := configFile.LoadFromReader(configData)
	return &configFile, err
}

// Load reads the configuration files in the given directory, and sets up
// the auth config information and returns values.
func Load(configDir string) (*configfile.ConfigFile, error) {
	if configDir == "" {
		configDir = Dir()
	}
	return load(configDir)
}

func load(configDir string) (*configfile.ConfigFile, error) {
	filename := filepath.Join(configDir, ConfigFileName)
	configFile := configfile.New(filename)

	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			//
			// if file is there but we can't stat it for any reason other
			// than it doesn't exist then stop
			return configFile, nil
		}
		// if file is there but we can't stat it for any reason other
		// than it doesn't exist then stop
		return configFile, nil
	}
	defer file.Close()
	err = configFile.LoadFromReader(file)
	if err != nil {
		err = errors.Wrap(err, filename)
	}
	return configFile, err
}

// LoadDefaultConfigFile attempts to load the default config file and returns
// an initialized ConfigFile struct if none is found.
func LoadDefaultConfigFile(stderr io.Writer) *configfile.ConfigFile {
	configFile, err := load(Dir())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "WARNING: Error loading config file: %v\n", err)
	}
	if !configFile.ContainsAuth() {
		configFile.CredentialsStore = credentials.DetectDefaultStore(configFile.CredentialsStore)
	}
	return configFile
}
