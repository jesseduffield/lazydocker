//go:build windows

package util

import (
	"errors"
	"fmt"
	"path/filepath"

	"go.podman.io/storage/pkg/homedir"
)

var errNotImplemented = errors.New("not yet implemented")

// IsCgroup2UnifiedMode returns whether we are running in cgroup 2 unified mode.
func IsCgroup2UnifiedMode() (bool, error) {
	return false, fmt.Errorf("IsCgroup2Unified: %w", errNotImplemented)
}

// GetContainerPidInformationDescriptors returns a string slice of all supported
// format descriptors of GetContainerPidInformation.
func GetContainerPidInformationDescriptors() ([]string, error) {
	return nil, fmt.Errorf("GetContainerPidInformationDescriptors: %w", errNotImplemented)
}

// GetRootlessPauseProcessPidPath returns the path to the file that holds the pid for
// the pause process
func GetRootlessPauseProcessPidPath() (string, error) {
	return "", fmt.Errorf("GetRootlessPauseProcessPidPath: %w", errNotImplemented)
}

// GetRootlessRuntimeDir returns the runtime directory
func GetRootlessRuntimeDir() (string, error) {
	data, err := homedir.GetDataHome()
	if err != nil {
		return "", err
	}
	runtimeDir := filepath.Join(data, "containers", "podman")
	return runtimeDir, nil
}

// GetRootlessConfigHomeDir returns the config home directory when running as non root
func GetRootlessConfigHomeDir() (string, error) {
	return "", errors.New("this function is not implemented for windows")
}
