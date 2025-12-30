//go:build windows

package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDetectPlatformCandidates_Windows_DockerSuccess(t *testing.T) {
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		if host == "npipe:////./pipe/docker_engine" {
			return nil
		}
		return errors.New("mock failure")
	}

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := detectPlatformCandidates(log)
	assert.NoError(t, err)
	assert.Equal(t, "npipe:////./pipe/docker_engine", host)
	assert.Equal(t, RuntimeDocker, runtime)
}

func TestDetectPlatformCandidates_Windows_PodmanSuccess(t *testing.T) {
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		if host == "npipe:////./pipe/podman-machine-default" {
			return nil
		}
		return errors.New("mock failure")
	}

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := detectPlatformCandidates(log)
	assert.NoError(t, err)
	assert.Equal(t, "npipe:////./pipe/podman-machine-default", host)
	assert.Equal(t, RuntimePodman, runtime)
}

func TestDetectPlatformCandidates_Windows_Failure(t *testing.T) {
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return errors.New("mock failure")
	}

	log := logrus.NewEntry(logrus.New())
	_, _, err := detectPlatformCandidates(log)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNoContainerSocket)
}

func TestGetPodmanPipes(t *testing.T) {
	log := logrus.NewEntry(logrus.New())

	// Test fallback when home dir is missing or empty
	// We can't easily mock UserHomeDir without refactoring, but we can test the logic
	// by creating the expected directory structure in a temp dir and setting USERPROFILE.

	tmpDir, err := os.MkdirTemp("", "podman-test")
	assert.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("failed to remove temp dir %s: %v", tmpDir, err)
		}
	})

	// Set USERPROFILE to our temp dir
	oldProfile := os.Getenv("USERPROFILE")
	t.Cleanup(func() { os.Setenv("USERPROFILE", oldProfile) })
	os.Setenv("USERPROFILE", tmpDir)

	// 1. Test fallback when directory doesn't exist
	pipes := getPodmanPipes(log)
	assert.Equal(t, []string{"npipe:////./pipe/podman-machine-default"}, pipes)

	// 2. Test with actual config files
	configDir := filepath.Join(tmpDir, ".config", "containers", "podman", "machine", "wsl")
	err = os.MkdirAll(configDir, 0755)
	assert.NoError(t, err)

	err = os.WriteFile(filepath.Join(configDir, "machine1.json"), []byte("{}"), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(configDir, "machine2.json"), []byte("{}"), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(configDir, "not-a-config.txt"), []byte("{}"), 0644)
	assert.NoError(t, err)

	pipes = getPodmanPipes(log)
	assert.Len(t, pipes, 2)
	assert.Contains(t, pipes, "npipe:////./pipe/machine1")
	assert.Contains(t, pipes, "npipe:////./pipe/machine2")

	// 3. Test with empty config directory (no JSON files)
	emptyDir := filepath.Join(tmpDir, ".config", "containers", "podman", "machine", "empty-wsl")
	err = os.MkdirAll(emptyDir, 0755)
	assert.NoError(t, err)

	// Create a new temp dir for this specific test
	tmpDir2, err := os.MkdirTemp("", "podman-test-empty")
	assert.NoError(t, err)
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir2); err != nil {
			t.Errorf("failed to remove temp dir %s: %v", tmpDir2, err)
		}
	})
	os.Setenv("USERPROFILE", tmpDir2)

	// Create the wsl directory but leave it empty
	emptyConfigDir := filepath.Join(tmpDir2, ".config", "containers", "podman", "machine", "wsl")
	err = os.MkdirAll(emptyConfigDir, 0755)
	assert.NoError(t, err)

	pipes = getPodmanPipes(log)
	assert.Equal(t, []string{"npipe:////./pipe/podman-machine-default"}, pipes)
}
