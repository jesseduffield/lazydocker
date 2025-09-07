package app

import (
	"os/exec"
	"testing"

	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestNewAppRuntimeSelection(t *testing.T) {
	tests := []struct {
		name          string
		runtime       string
		expectError   bool
		errorContains string
		expectDocker  bool
		expectApple   bool
	}{
		{
			name:         "docker runtime",
			runtime:      "docker",
			expectError:  false,
			expectDocker: true,
			expectApple:  false,
		},
		{
			name:          "apple runtime",
			runtime:       "apple",
			expectError:   !appleAvailable(),
			errorContains: "Apple Container CLI not found",
			expectDocker:  false,
			expectApple:   appleAvailable(),
		},
		{
			name:          "invalid runtime",
			runtime:       "invalid",
			expectError:   true,
			errorContains: "unsupported runtime 'invalid'",
			expectDocker:  false,
			expectApple:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create app config with the test runtime
			appConfig, err := config.NewAppConfig(
				"lazydocker",
				"test-version",
				"test-commit",
				"test-date",
				"test-build-source",
				false,      // debug
				[]string{}, // compose files
				"/tmp",     // project dir
				tt.runtime,
			)

			if tt.runtime == "invalid" {
				// Should fail at config creation for invalid runtime
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			assert.Nil(t, err)
			assert.Equal(t, tt.runtime, appConfig.Runtime)

			// Try to create the app
			app, err := NewApp(appConfig)

			if tt.expectError {
				assert.NotNil(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.Nil(t, err)
				assert.NotNil(t, app)
				assert.Equal(t, tt.runtime, app.Config.Runtime)

				// Check that the correct command was initialized
				if tt.expectDocker {
					assert.NotNil(t, app.DockerCommand, "DockerCommand should be initialized for docker runtime")
					assert.Nil(t, app.AppleContainerCommand, "AppleContainerCommand should be nil for docker runtime")
					assert.NotNil(t, app.ContainerRuntime, "ContainerRuntime should be initialized")
					assert.Equal(t, "docker", app.ContainerRuntime.GetRuntimeName())
				}
				if tt.expectApple {
					assert.Nil(t, app.DockerCommand, "DockerCommand should be nil for apple runtime")
					assert.NotNil(t, app.AppleContainerCommand, "AppleContainerCommand should be initialized for apple runtime")
					assert.NotNil(t, app.ContainerRuntime, "ContainerRuntime should be initialized")
					assert.Equal(t, "apple", app.ContainerRuntime.GetRuntimeName())
				}
			}
		})
	}
}

func appleAvailable() bool {
	_, err := exec.LookPath("container")
	return err == nil
}

func TestAppRuntimeFieldsInitialization(t *testing.T) {
	// Test that app properly initializes with docker runtime
	appConfig, err := config.NewAppConfig(
		"lazydocker",
		"test-version",
		"test-commit",
		"test-date",
		"test-build-source",
		false,
		[]string{},
		"/tmp",
		"docker",
	)
	assert.Nil(t, err)

	app, err := NewApp(appConfig)
	assert.Nil(t, err)
	assert.NotNil(t, app)

	// Check that all required fields are initialized
	assert.NotNil(t, app.Config)
	assert.NotNil(t, app.Log)
	assert.NotNil(t, app.OSCommand)
	assert.NotNil(t, app.Tr)
	assert.NotNil(t, app.ErrorChan)

	// For docker runtime
	assert.NotNil(t, app.DockerCommand)
	assert.Nil(t, app.AppleContainerCommand)
	assert.NotNil(t, app.ContainerRuntime)
	assert.Equal(t, "docker", app.ContainerRuntime.GetRuntimeName())
	assert.NotNil(t, app.Gui)
}

func TestAppKnownErrorHandling(t *testing.T) {
	// Create a basic app for testing error handling
	appConfig, err := config.NewAppConfig(
		"lazydocker",
		"test-version",
		"test-commit",
		"test-date",
		"test-build-source",
		false,
		[]string{},
		"/tmp",
		"docker",
	)
	assert.Nil(t, err)

	app, err := NewApp(appConfig)
	assert.Nil(t, err)

	tests := []struct {
		name         string
		errorMessage string
		expectKnown  bool
		expectedText string
	}{
		{
			name:         "docker permission error",
			errorMessage: "Got permission denied while trying to connect to the Docker daemon socket",
			expectKnown:  true,
			expectedText: app.Tr.CannotAccessDockerSocketError,
		},
		{
			name:         "apple container not found",
			errorMessage: "Apple Container CLI not found",
			expectKnown:  true,
			expectedText: "Apple Container CLI not found. Please ensure the 'container' command is installed and available in your PATH.",
		},
		{
			name:         "failed to get containers",
			errorMessage: "failed to get containers from runtime",
			expectKnown:  true,
			expectedText: "Failed to retrieve containers. Please check if the container runtime is running.",
		},
		{
			name:         "unknown error",
			errorMessage: "some unknown error message",
			expectKnown:  false,
			expectedText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock error with the test message
			mockError := &mockError{message: tt.errorMessage}

			text, known := app.KnownError(mockError)

			assert.Equal(t, tt.expectKnown, known)
			if tt.expectKnown {
				assert.Equal(t, tt.expectedText, text)
			} else {
				assert.Empty(t, text)
			}
		})
	}
}

// mockError is a simple error implementation for testing
type mockError struct {
	message string
}

func (e *mockError) Error() string {
	return e.message
}
