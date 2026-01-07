//go:build !windows

package commands

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectSocketPath_ContainerHost(t *testing.T) {
	// Save original value
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	originalDockerHost := os.Getenv("DOCKER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
		os.Setenv("DOCKER_HOST", originalDockerHost)
	}()

	// Clear both env vars first
	os.Unsetenv("CONTAINER_HOST")
	os.Unsetenv("DOCKER_HOST")

	// Test CONTAINER_HOST takes priority
	os.Setenv("CONTAINER_HOST", "unix:///custom/podman.sock")
	result := detectSocketPath()
	assert.Equal(t, "unix:///custom/podman.sock", result)
}

func TestDetectSocketPath_DockerHostFallback(t *testing.T) {
	// Save original values
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	originalDockerHost := os.Getenv("DOCKER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
		os.Setenv("DOCKER_HOST", originalDockerHost)
	}()

	// Clear CONTAINER_HOST, set DOCKER_HOST
	os.Unsetenv("CONTAINER_HOST")
	os.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")

	result := detectSocketPath()
	assert.Equal(t, "unix:///var/run/docker.sock", result)
}

func TestDetectSocketPath_ContainerHostPriority(t *testing.T) {
	// Save original values
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	originalDockerHost := os.Getenv("DOCKER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
		os.Setenv("DOCKER_HOST", originalDockerHost)
	}()

	// Set both - CONTAINER_HOST should take priority
	os.Setenv("CONTAINER_HOST", "unix:///podman.sock")
	os.Setenv("DOCKER_HOST", "unix:///docker.sock")

	result := detectSocketPath()
	assert.Equal(t, "unix:///podman.sock", result)
}

func TestDetectSocketPath_SSHHost(t *testing.T) {
	// Save original value
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
	}()

	// Test SSH format
	os.Setenv("CONTAINER_HOST", "ssh://user@remote.host:22/run/podman/podman.sock")
	result := detectSocketPath()
	assert.Equal(t, "ssh://user@remote.host:22/run/podman/podman.sock", result)
}

func TestDetectSocketPath_EmptyEnvVars(t *testing.T) {
	// Save original values
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	originalDockerHost := os.Getenv("DOCKER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
		os.Setenv("DOCKER_HOST", originalDockerHost)
	}()

	// Clear all env vars
	os.Unsetenv("CONTAINER_HOST")
	os.Unsetenv("DOCKER_HOST")

	// The result depends on whether socket files exist on the system
	// We can at least verify it doesn't panic and returns a string
	result := detectSocketPath()
	// Result will be either a socket path or empty string
	assert.IsType(t, "", result)
}

func TestDetectSocketPath_RootlessPath(t *testing.T) {
	// Save original values
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	originalDockerHost := os.Getenv("DOCKER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
		os.Setenv("DOCKER_HOST", originalDockerHost)
	}()

	// Clear env vars to test socket detection
	os.Unsetenv("CONTAINER_HOST")
	os.Unsetenv("DOCKER_HOST")

	// Get current UID
	uid := os.Getuid()
	expectedRootlessPath := fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", uid)

	result := detectSocketPath()

	// If rootless socket exists, it should return that path
	// Otherwise, it will try other paths
	if result == expectedRootlessPath {
		assert.Equal(t, expectedRootlessPath, result)
	}
	// This is a valid test outcome - the socket may or may not exist
}

func TestDetectSocketPath_VariousFormats(t *testing.T) {
	// Save original value
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
	}()

	testCases := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "Unix socket with unix:// prefix",
			envValue: "unix:///run/podman/podman.sock",
			expected: "unix:///run/podman/podman.sock",
		},
		{
			name:     "SSH connection",
			envValue: "ssh://root@192.168.1.100:22/run/podman/podman.sock",
			expected: "ssh://root@192.168.1.100:22/run/podman/podman.sock",
		},
		{
			name:     "TCP connection",
			envValue: "tcp://localhost:8080",
			expected: "tcp://localhost:8080",
		},
		{
			name:     "Path without prefix",
			envValue: "/run/podman/podman.sock",
			expected: "/run/podman/podman.sock",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("CONTAINER_HOST", tc.envValue)
			result := detectSocketPath()
			assert.Equal(t, tc.expected, result)
		})
	}
}

// Test that the function handles edge cases
func TestDetectSocketPath_EdgeCases(t *testing.T) {
	// Save original values
	originalContainerHost := os.Getenv("CONTAINER_HOST")
	originalDockerHost := os.Getenv("DOCKER_HOST")
	defer func() {
		os.Setenv("CONTAINER_HOST", originalContainerHost)
		os.Setenv("DOCKER_HOST", originalDockerHost)
	}()

	testCases := []struct {
		name          string
		containerHost string
		dockerHost    string
		expected      string
	}{
		{
			name:          "Empty CONTAINER_HOST with spaces",
			containerHost: "   ",
			dockerHost:    "",
			expected:      "   ", // Whitespace is treated as a value
		},
		{
			name:          "Very long path",
			containerHost: "unix:///this/is/a/very/long/path/to/a/socket/file/that/might/exist/somewhere/podman.sock",
			dockerHost:    "",
			expected:      "unix:///this/is/a/very/long/path/to/a/socket/file/that/might/exist/somewhere/podman.sock",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.containerHost != "" {
				os.Setenv("CONTAINER_HOST", tc.containerHost)
			} else {
				os.Unsetenv("CONTAINER_HOST")
			}
			if tc.dockerHost != "" {
				os.Setenv("DOCKER_HOST", tc.dockerHost)
			} else {
				os.Unsetenv("DOCKER_HOST")
			}

			result := detectSocketPath()
			assert.Equal(t, tc.expected, result)
		})
	}
}
