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

		// Version should still be negotiable (not locked to env var)
		assert.NotEqual(t, "1.25", cli.ClientVersion())
	})
}

// TestGetContextTLSOptions tests that TLS options can be loaded from Docker contexts
func TestGetContextTLSOptions(t *testing.T) {
	// Save original env vars and restore after test
	originalContext := os.Getenv("DOCKER_CONTEXT")
	defer func() {
		if originalContext == "" {
			os.Unsetenv("DOCKER_CONTEXT")
		} else {
			os.Setenv("DOCKER_CONTEXT", originalContext)
		}
	}()

	// Test with no context set (should return nil)
	os.Unsetenv("DOCKER_CONTEXT")
	opts, err := getContextTLSOptions()
	assert.NoError(t, err)
	assert.Nil(t, opts)

	// Test with default context (should return nil)
	os.Setenv("DOCKER_CONTEXT", "default")
	opts, err = getContextTLSOptions()
	assert.NoError(t, err)
	assert.Nil(t, opts)

	// Test with non-existent context (should return nil, not error)
	os.Setenv("DOCKER_CONTEXT", "nonexistent-context")
	opts, err = getContextTLSOptions()
	assert.NoError(t, err)
	assert.Nil(t, opts)
}
