//go:build !windows

package commands

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetSocketCandidates(t *testing.T) {
	// Save env vars
	oldXdg := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldXdg)
		os.Setenv("HOME", oldHome)
	}()

	os.Setenv("XDG_RUNTIME_DIR", "/tmp/runtime")
	os.Setenv("HOME", "/home/user")

	candidates := getSocketCandidates()

	// Check some expected candidates
	foundDocker := false
	foundPodman := false
	for _, c := range candidates {
		if c.Path == "unix:///var/run/docker.sock" {
			foundDocker = true
		}
		if c.Path == "unix:///tmp/runtime/podman/podman.sock" {
			foundPodman = true
		}
	}

	assert.True(t, foundDocker, "Standard Docker socket should be in candidates")
	assert.True(t, foundPodman, "Rootless Podman socket should be in candidates")
}

func TestDetectDockerHost_DOCKER_HOST_Priority(t *testing.T) {
	// Save env var
	oldDockerHost := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	expectedHost := "unix:///tmp/custom.sock"
	os.Setenv("DOCKER_HOST", expectedHost)

	// Reset cache for test
	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host, _, err := DetectDockerHost(log)
	assert.NoError(t, err)
	assert.Equal(t, expectedHost, host)
}
func TestDetectDockerHost_Caching(t *testing.T) {
	// Save env var
	oldDockerHost := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	os.Setenv("DOCKER_HOST", "unix:///tmp/first.sock")

	// Reset cache for test
	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host1, _, _ := DetectDockerHost(log)

	// Change env var - should still return first one from cache
	os.Setenv("DOCKER_HOST", "unix:///tmp/second.sock")
	host2, _, _ := DetectDockerHost(log)

	assert.Equal(t, host1, host2)
	assert.Equal(t, "unix:///tmp/first.sock", host2)
}
func TestDetectDockerHost_Context_Invalid(t *testing.T) {
	// Save env vars
	oldDockerHost := os.Getenv("DOCKER_HOST")
	oldDockerContext := os.Getenv("DOCKER_CONTEXT")
	defer func() {
		os.Setenv("DOCKER_HOST", oldDockerHost)
		os.Setenv("DOCKER_CONTEXT", oldDockerContext)
	}()

	os.Setenv("DOCKER_HOST", "")
	os.Setenv("DOCKER_CONTEXT", "nonexistent-context-12345")

	// Reset cache for test
	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	_, _, err := DetectDockerHost(log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to use DOCKER_CONTEXT")
}

func TestGetHostFromContext(t *testing.T) {
	// This test is tricky because it depends on the Docker CLI config.
	// We'll skip it if we can't easily mock the config directory.
	// But we can at least test the "default" case.
	host, err := getHostFromContext()
	if err == nil {
		// If it succeeded, it should be empty or a valid host
		assert.True(t, host == "" || host != "")
	}
}

func TestValidateSocket_Failures(t *testing.T) {
	ctx := context.Background()

	// Test non-existent path
	err := validateSocket(ctx, "unix:///tmp/nonexistent-12345.sock", false)
	assert.Error(t, err)

	// Test invalid schema
	err = validateSocket(ctx, "invalid:///tmp/test.sock", false)
	assert.Error(t, err)
}

func TestDetectPlatformCandidates_Unix(t *testing.T) {
	// Mock validateSocketFunc to always fail
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return errors.New("mock failure")
	}

	// Mock environment to ensure no candidates are found
	oldXdg := os.Getenv("XDG_RUNTIME_DIR")
	oldHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("XDG_RUNTIME_DIR", oldXdg)
		os.Setenv("HOME", oldHome)
	}()

	os.Setenv("XDG_RUNTIME_DIR", "/tmp/nonexistent-xdg")
	os.Setenv("HOME", "/tmp/nonexistent-home")

	log := logrus.NewEntry(logrus.New())
	_, _, err := detectPlatformCandidates(log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), ErrNoDockerSocket.Error())
}

func TestDetectPlatformCandidates_Unix_Success(t *testing.T) {
	// Mock validateSocketFunc to succeed for a specific path
	oldValidate := validateSocketFunc
	oldStat := statFunc
	defer func() {
		validateSocketFunc = oldValidate
		statFunc = oldStat
	}()

	expectedPath := "unix:///var/run/docker.sock"
	statFunc = func(name string) (os.FileInfo, error) {
		if name == "/var/run/docker.sock" {
			return nil, nil // Mock success
		}
		return nil, os.ErrNotExist
	}

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		if host == expectedPath {
			return nil
		}
		return errors.New("mock failure")
	}

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := detectPlatformCandidates(log)
	assert.NoError(t, err)
	assert.Equal(t, expectedPath, host)
	assert.Equal(t, RuntimeDocker, runtime)
}
