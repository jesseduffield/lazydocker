//go:build !windows

package commands

import (
	"os"
	"sync"
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
	dockerHostOnce = sync.Once{}
	cachedDockerHost = ""

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
	dockerHostOnce = sync.Once{}
	cachedDockerHost = ""

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
	dockerHostOnce = sync.Once{}
	cachedDockerHost = ""

	log := logrus.NewEntry(logrus.New())
	_, _, err := DetectDockerHost(log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to use DOCKER_CONTEXT")
}
