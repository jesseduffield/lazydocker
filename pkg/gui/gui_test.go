package gui

import (
	"testing"

	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNewGuiWithContainerCommand(t *testing.T) {
	// Create test dependencies
	log := logrus.NewEntry(logrus.New())
	osCommand := commands.NewOSCommand(log, &config.AppConfig{})
	tr := &i18n.TranslationSet{}
	errorChan := make(chan error)

	tests := []struct {
		name        string
		runtime     string
		expectError bool
	}{
		{
			name:        "docker runtime",
			runtime:     "docker",
			expectError: false,
		},
		{
			name:        "apple runtime",
			runtime:     "apple",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create app config
			appConfig, err := config.NewAppConfig(
				"lazydocker",
				"test-version",
				"test-commit",
				"test-date",
				"test-build-source",
				false,
				[]string{},
				"/tmp",
				tt.runtime,
			)
			assert.Nil(t, err)

			// Create mock container runtime
			containerRuntime := commands.NewContainerRuntimeAdapter(nil, nil, tt.runtime)
			containerCommand := commands.NewGuiContainerCommand(containerRuntime, nil, appConfig)

			// Create GUI
			gui, err := NewGui(log, nil, containerCommand, osCommand, tr, appConfig, errorChan)

			if tt.expectError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.NotNil(t, gui)
				assert.Equal(t, containerCommand, gui.ContainerCommand)
				assert.Equal(t, appConfig, gui.Config)
			}
		})
	}
}

func TestGuiContainerCommandIntegration(t *testing.T) {
	// Test that GUI can use ContainerCommand methods
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

	// Create mock runtime adapter
	containerRuntime := &commands.ContainerRuntimeAdapter{}
	containerCommand := commands.NewGuiContainerCommand(containerRuntime, nil, appConfig)

	// Test runtime name
	assert.Equal(t, "", containerCommand.GetRuntimeName()) // Empty because we didn't set runtimeType

	// Test that methods don't panic when called with nil internals
	containers, err := containerCommand.GetContainers(nil)
	assert.NotNil(t, err) // Should error because runtime is not properly initialized
	assert.Nil(t, containers)

	// Test InDockerComposeProject
	assert.False(t, containerCommand.InDockerComposeProject())

	// Test DockerComposeConfig
	assert.Empty(t, containerCommand.DockerComposeConfig())
}
