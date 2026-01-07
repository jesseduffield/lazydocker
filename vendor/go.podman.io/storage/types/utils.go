package types

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
)

func expandEnvPath(path string, rootlessUID int) (string, error) {
	var err error
	path = strings.ReplaceAll(path, "$UID", strconv.Itoa(rootlessUID))
	path = os.ExpandEnv(path)
	newpath, err := filepath.EvalSymlinks(path)
	if err != nil {
		newpath = filepath.Clean(path)
	}
	return newpath, nil
}

func DefaultConfigFile() (string, error) {
	if defaultConfigFileSet {
		return defaultConfigFile, nil
	}

	if path, ok := os.LookupEnv(storageConfEnv); ok {
		return path, nil
	}
	if !usePerUserStorage() {
		if err := fileutils.Exists(defaultOverrideConfigFile); err == nil {
			return defaultOverrideConfigFile, nil
		}
		return defaultConfigFile, nil
	}

	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "containers/storage.conf"), nil
	}
	home := homedir.Get()
	if home == "" {
		return "", errors.New("cannot determine user's homedir")
	}
	return filepath.Join(home, ".config/containers/storage.conf"), nil
}

func reloadConfigurationFileIfNeeded(configFile string, storeOptions *StoreOptions) {
	prevReloadConfig.mutex.Lock()
	defer prevReloadConfig.mutex.Unlock()

	fi, err := os.Stat(configFile)
	if err != nil {
		if !os.IsNotExist(err) {
			logrus.Warningf("Failed to read %s %v\n", configFile, err.Error())
		}
		return
	}

	mtime := fi.ModTime()
	if prevReloadConfig.storeOptions != nil && mtime.Equal(prevReloadConfig.mod) && prevReloadConfig.configFile == configFile {
		*storeOptions = *prevReloadConfig.storeOptions
		return
	}

	if err := ReloadConfigurationFile(configFile, storeOptions); err != nil {
		logrus.Warningf("Failed to reload %q %v\n", configFile, err)
		return
	}

	prevReloadConfig.storeOptions = storeOptions
	prevReloadConfig.mod = mtime
	prevReloadConfig.configFile = configFile
}
