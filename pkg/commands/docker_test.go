package commands

import (
	"os"
	"testing"

	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
)

// TestNewDockerClientVersionNegotiation verifies that newDockerClient allows
// API version negotiation even when DOCKER_API_VERSION is set.
//
// This is a regression test for https://github.com/jesseduffield/lazydocker/issues/715
// where users got "client version 1.25 is too old" errors because FromEnv()
// includes WithVersionFromEnv() which sets manualOverride=true, preventing
// API version negotiation.
func TestNewDockerClientVersionNegotiation(t *testing.T) {
	// Save original env var and restore after test
	originalAPIVersion := os.Getenv("DOCKER_API_VERSION")
	defer func() {
		if originalAPIVersion == "" {
			os.Unsetenv("DOCKER_API_VERSION")
		} else {
			os.Setenv("DOCKER_API_VERSION", originalAPIVersion)
		}
	}()

	// Set DOCKER_API_VERSION to an old version that would cause
	// "client version 1.25 is too old" errors if negotiation is disabled
	os.Setenv("DOCKER_API_VERSION", "1.25")

	t.Run("FromEnv locks version preventing negotiation", func(t *testing.T) {
		// This demonstrates the problematic behavior we're avoiding.
		// When using FromEnv with DOCKER_API_VERSION set, the client
		// version gets locked to 1.25 and negotiation is disabled.
		cli, err := client.NewClientWithOpts(
			client.FromEnv,
			client.WithAPIVersionNegotiation(),
		)
		assert.NoError(t, err)
		defer cli.Close()

		// Version is locked to the env var value
		assert.Equal(t, "1.25", cli.ClientVersion())
	})

	t.Run("newDockerClient allows version negotiation", func(t *testing.T) {
		// Test the actual production function.
		// Use DefaultDockerHost for cross-platform compatibility
		// (unix socket on Linux/macOS, named pipe on Windows).
		cli, err := newDockerClient(client.DefaultDockerHost)
		assert.NoError(t, err)
		defer cli.Close()

		// Version is NOT locked to the env var value (1.25).
		// Instead, it uses the library's default version and will negotiate
		// with the server on first request. This is the key difference that
		// fixes the "version too old" error.
		assert.NotEqual(t, "1.25", cli.ClientVersion(),
			"client version should not be locked to DOCKER_API_VERSION env var")
	})
}
