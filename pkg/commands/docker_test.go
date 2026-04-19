package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
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

func TestSetDockerComposeCommandRespectsExplicitUserOverride(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yml")
	content := []byte("commandTemplates:\n  dockerCompose: docker compose\n")

	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	originalConfigDir := os.Getenv("CONFIG_DIR")
	defer func() {
		if originalConfigDir == "" {
			os.Unsetenv("CONFIG_DIR")
		} else {
			os.Setenv("CONFIG_DIR", originalConfigDir)
		}
	}()
	os.Setenv("CONFIG_DIR", configDir)

	appConfig, err := config.NewAppConfig("lazydocker", "version", "commit", "date", "buildSource", false, nil, "projectDir", "")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	osCommand := NewOSCommand(NewDummyLog(), appConfig)
	osCommand.SetCommand(newHelperCommand(true))

	dockerCommand := &DockerCommand{
		OSCommand: osCommand,
		Config:    appConfig,
	}

	dockerCommand.setDockerComposeCommand(appConfig)

	assert.Equal(t, "docker compose", appConfig.UserConfig.CommandTemplates.DockerCompose)
}

func TestSetDockerComposeCommandFallsBackWhenUnconfigured(t *testing.T) {
	userConfig := config.GetDefaultConfig()
	appConfig := &config.AppConfig{UserConfig: &userConfig}
	osCommand := NewOSCommand(NewDummyLog(), appConfig)
	osCommand.SetCommand(newHelperCommand(true))

	dockerCommand := &DockerCommand{
		OSCommand: osCommand,
		Config:    appConfig,
	}

	dockerCommand.setDockerComposeCommand(appConfig)

	assert.Equal(t, "docker-compose", appConfig.UserConfig.CommandTemplates.DockerCompose)
}

func TestDockerCommandHelperProcess(t *testing.T) {
	if len(os.Args) < 4 || os.Args[2] != "--" || os.Args[3] != "docker-helper" {
		return
	}

	if len(os.Args) > 4 && os.Args[4] == "fail" {
		fmt.Fprint(os.Stderr, "simulated failure")
		os.Exit(1)
	}

	os.Exit(0)
}

func newHelperCommand(shouldFail bool) func(string, ...string) *exec.Cmd {
	return func(name string, arg ...string) *exec.Cmd {
		args := []string{"-test.run=TestDockerCommandHelperProcess", "--", "docker-helper"}
		if shouldFail {
			args = append(args, "fail")
		}
		return exec.Command(os.Args[0], args...)
	}
}
