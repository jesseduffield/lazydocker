package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainerRuntimeAdapter(t *testing.T) {
	tests := []struct {
		name        string
		runtimeType string
		hasDocker   bool
		hasApple    bool
	}{
		{
			name:        "docker runtime",
			runtimeType: "docker",
			hasDocker:   true,
			hasApple:    false,
		},
		{
			name:        "apple runtime",
			runtimeType: "apple",
			hasDocker:   false,
			hasApple:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dockerCmd *DockerCommand
			var appleCmd *AppleContainerCommand

			if tt.hasDocker {
				// Create a mock docker command (we can't easily create a real one in tests)
				dockerCmd = &DockerCommand{}
			}
			if tt.hasApple {
				// Create a mock apple container command
				appleCmd = &AppleContainerCommand{}
			}

			adapter := NewContainerRuntimeAdapter(dockerCmd, appleCmd, tt.runtimeType)

			// Test basic properties
			assert.Equal(t, tt.runtimeType, adapter.GetRuntimeName())
			assert.NotEmpty(t, adapter.GetRuntimeVersion())

			// Test that the adapter correctly identifies its runtime type
			switch tt.runtimeType {
			case "docker":
				assert.Equal(t, dockerCmd, adapter.dockerCommand)
				assert.Nil(t, adapter.appleContainerCommand)
			case "apple":
				assert.Nil(t, adapter.dockerCommand)
				assert.Equal(t, appleCmd, adapter.appleContainerCommand)
			}
		})
	}
}

func TestContainerRuntimeAdapterErrorHandling(t *testing.T) {
	tests := []struct {
		name         string
		runtimeType  string
		dockerCmd    *DockerCommand
		appleCmd     *AppleContainerCommand
		expectError  bool
		errorMessage string
	}{
		{
			name:         "docker runtime with nil command",
			runtimeType:  "docker",
			dockerCmd:    nil,
			appleCmd:     nil,
			expectError:  true,
			errorMessage: "docker command not available",
		},
		{
			name:         "apple runtime with nil command",
			runtimeType:  "apple",
			dockerCmd:    nil,
			appleCmd:     nil,
			expectError:  true,
			errorMessage: "apple command not available",
		},
		{
			name:         "unsupported runtime",
			runtimeType:  "unsupported",
			dockerCmd:    nil,
			appleCmd:     nil,
			expectError:  true,
			errorMessage: "unsupported runtime 'unsupported'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewContainerRuntimeAdapter(tt.dockerCmd, tt.appleCmd, tt.runtimeType)

			// Test GetContainers error handling
			containers, err := adapter.GetContainers()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
				assert.Nil(t, containers)
			}

			// Test RefreshImages error handling
			images, err := adapter.RefreshImages()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
				assert.Nil(t, images)
			}

			// Test PruneContainers error handling
			err = adapter.PruneContainers()
			if tt.expectError {
				assert.Error(t, err)
				// Apple runtime returns operation not supported error
				if tt.runtimeType == "apple" {
					assert.Contains(t, err.Error(), "container pruning not supported by apple runtime")
				} else {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
			}
		})
	}
}

func TestContainerRuntimeAdapterAppleSpecificBehavior(t *testing.T) {
	adapter := NewContainerRuntimeAdapter(nil, &AppleContainerCommand{}, "apple")

	// Test that Apple Container returns empty services
	services, err := adapter.GetServices()
	assert.Nil(t, err)
	assert.Empty(t, services)

	// Test that Apple Container doesn't support compose projects
	assert.False(t, adapter.InDockerComposeProject())

	// Test that Apple Container returns empty volumes
	volumes, err := adapter.RefreshVolumes()
	assert.Nil(t, err)
	assert.Empty(t, volumes)

	// Test that Apple Container returns empty networks
	networks, err := adapter.RefreshNetworks()
	assert.Nil(t, err)
	assert.Empty(t, networks)

	// Test that Apple Container doesn't support certain operations
	err = adapter.PruneContainers()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container pruning not supported by apple runtime")

	err = adapter.PruneImages()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "image pruning not supported by apple runtime")

	err = adapter.PruneVolumes()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume pruning not supported by apple runtime")

	err = adapter.PruneNetworks()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "network pruning not supported by apple runtime")

	// Test ViewAllLogs returns error for Apple Container
	cmd, err := adapter.ViewAllLogs()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "viewing all logs not supported by apple runtime")
	assert.Nil(t, cmd)

	// Test DockerComposeConfig returns empty for Apple Container
	config := adapter.DockerComposeConfig()
	assert.Empty(t, config)
}

func TestContainerRuntimeAdapterClose(t *testing.T) {
	// Test closing with docker runtime
	dockerAdapter := NewContainerRuntimeAdapter(&DockerCommand{}, nil, "docker")
	err := dockerAdapter.Close()
	// We expect no error because our mock DockerCommand doesn't implement Close properly
	// In a real scenario, this would call the actual Close method
	assert.Nil(t, err)

	// Test closing with apple runtime
	appleAdapter := NewContainerRuntimeAdapter(nil, &AppleContainerCommand{}, "apple")
	err = appleAdapter.Close()
	assert.Nil(t, err) // Apple Container doesn't implement io.Closer, so this should always be nil
}
