//go:build !windows

package config

import (
	"os"
	"path/filepath"

	"go.podman.io/storage/pkg/unshare"
)

// _configPath is the path to the containers/containers.conf
// inside a given config directory.
const _configPath = "containers/containers.conf"

// userConfigPath returns the path to the users local config that is
// not shared with other users. It uses $XDG_CONFIG_HOME/containers...
// if set or $HOME/.config/containers... if not.
func userConfigPath() (string, error) {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, _configPath), nil
	}
	home, err := unshare.HomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, userOverrideContainersConfig), nil
}

// overrideContainersConfigPath returns the default config path overridden
// by the root user.
func overrideContainersConfigPath() (string, error) {
	return overrideContainersConfig, nil
}
