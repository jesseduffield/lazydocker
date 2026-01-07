//go:build !windows

package util

// TODO once rootless function is consolidated under libpod, we
//  should work to take darwin from this

import (
	"path/filepath"

	"github.com/containers/podman/v5/pkg/rootless"
	"go.podman.io/storage/pkg/homedir"
)

// GetRootlessRuntimeDir returns the runtime directory when running as non root
func GetRootlessRuntimeDir() (string, error) {
	if !rootless.IsRootless() {
		return "", nil
	}
	return homedir.GetRuntimeDir()
}

// GetRootlessConfigHomeDir returns the config home directory when running as non root
func GetRootlessConfigHomeDir() (string, error) {
	return homedir.GetConfigHome()
}

// GetRootlessPauseProcessPidPath returns the path to the file that holds the pid for
// the pause process.
func GetRootlessPauseProcessPidPath() (string, error) {
	runtimeDir, err := homedir.GetRuntimeDir()
	if err != nil {
		return "", err
	}
	// Note this path must be kept in sync with pkg/rootless/rootless_linux.go
	// We only want a single pause process per user, so we do not want to use
	// the tmpdir which can be changed via --tmpdir.
	return filepath.Join(runtimeDir, "libpod", "tmp", "pause.pid"), nil
}
