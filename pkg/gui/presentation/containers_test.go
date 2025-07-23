package presentation

import (
	"testing"

	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/stretchr/testify/assert"

	dockerTypes "github.com/docker/docker/api/types"
)

func TestGetContainerDisplayStringsWithRuntime(t *testing.T) {
	guiConfig := &config.GuiConfig{
		ContainerStatusHealthStyle: "long",
	}

	container := &commands.Container{
		Name: "test-container",
		ID:   "abc123",
		Container: dockerTypes.Container{
			State: "running",
			Image: "nginx:latest",
			Ports: []dockerTypes.Port{},
		},
	}

	tests := []struct {
		name              string
		runtime           string
		expectedIndicator string
	}{
		{
			name:              "docker runtime",
			runtime:           "docker",
			expectedIndicator: "test-container",
		},
		{
			name:              "apple runtime",
			runtime:           "apple",
			expectedIndicator: "test-container",
		},
		{
			name:              "unknown runtime",
			runtime:           "unknown",
			expectedIndicator: "test-container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetContainerDisplayStrings(guiConfig, container, tt.runtime)

			// Check that we get the expected number of columns (status, substatus, name, cpu, addr, ports, image)
			assert.Len(t, result, 7)

			// Check that the container name includes the runtime indicator
			assert.Equal(t, tt.expectedIndicator, result[2])

			// Check other columns are populated
			assert.Equal(t, "running", result[0]) // status
			assert.NotEmpty(t, result[6])         // image
		})
	}
}

func TestGetContainerDisplayStringsWithDifferentStates(t *testing.T) {
	guiConfig := &config.GuiConfig{
		ContainerStatusHealthStyle: "icon",
	}

	tests := []struct {
		name           string
		containerState string
		expectedIcon   string
	}{
		{
			name:           "running container",
			containerState: "running",
			expectedIcon:   "▶",
		},
		{
			name:           "exited container",
			containerState: "exited",
			expectedIcon:   "⨯",
		},
		{
			name:           "paused container",
			containerState: "paused",
			expectedIcon:   "◫",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &commands.Container{
				Name: "test-container",
				ID:   "abc123",
				Container: dockerTypes.Container{
					State: tt.containerState,
					Image: "nginx:latest",
					Ports: []dockerTypes.Port{},
				},
			}

			result := GetContainerDisplayStrings(guiConfig, container, "docker")

			// Check status icon
			assert.Equal(t, tt.expectedIcon, result[0])
		})
	}
}
