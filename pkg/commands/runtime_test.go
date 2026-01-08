package commands

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMockRuntimeImplementsInterface verifies MockRuntime implements ContainerRuntime
func TestMockRuntimeImplementsInterface(t *testing.T) {
	var _ ContainerRuntime = (*MockRuntime)(nil)
}

// TestMockRuntimeListContainers tests the ListContainers mock
func TestMockRuntimeListContainers(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	t.Run("returns error when not implemented", func(t *testing.T) {
		containers, err := mock.ListContainers(ctx)
		assert.Nil(t, containers)
		assert.Equal(t, ErrMockNotImplemented, err)
	})

	t.Run("returns custom result when function set", func(t *testing.T) {
		expectedContainers := []ContainerSummary{
			{ID: "abc123", Names: []string{"/test-container"}, State: "running"},
			{ID: "def456", Names: []string{"/another-container"}, State: "exited"},
		}

		mock.ListContainersFunc = func(ctx context.Context) ([]ContainerSummary, error) {
			return expectedContainers, nil
		}

		containers, err := mock.ListContainers(ctx)
		assert.NoError(t, err)
		assert.Len(t, containers, 2)
		assert.Equal(t, "abc123", containers[0].ID)
		assert.Equal(t, "running", containers[0].State)
	})

	t.Run("returns custom error when function set", func(t *testing.T) {
		customErr := errors.New("connection refused")
		mock.ListContainersFunc = func(ctx context.Context) ([]ContainerSummary, error) {
			return nil, customErr
		}

		containers, err := mock.ListContainers(ctx)
		assert.Nil(t, containers)
		assert.Equal(t, customErr, err)
	})
}

// TestMockRuntimeInspectContainer tests the InspectContainer mock
func TestMockRuntimeInspectContainer(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	t.Run("returns error when not implemented", func(t *testing.T) {
		details, err := mock.InspectContainer(ctx, "abc123")
		assert.Nil(t, details)
		assert.Equal(t, ErrMockNotImplemented, err)
	})

	t.Run("returns custom result when function set", func(t *testing.T) {
		expectedDetails := &ContainerDetails{
			ID:   "abc123",
			Name: "/test-container",
			State: &ContainerState{
				Status:  "running",
				Running: true,
			},
		}

		mock.InspectContainerFunc = func(ctx context.Context, id string) (*ContainerDetails, error) {
			assert.Equal(t, "abc123", id)
			return expectedDetails, nil
		}

		details, err := mock.InspectContainer(ctx, "abc123")
		assert.NoError(t, err)
		assert.Equal(t, "abc123", details.ID)
		assert.True(t, details.State.Running)
	})
}

// TestMockRuntimeContainerLifecycle tests container lifecycle mocks
func TestMockRuntimeContainerLifecycle(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	t.Run("StartContainer", func(t *testing.T) {
		mock.StartContainerFunc = func(ctx context.Context, id string) error {
			assert.Equal(t, "container1", id)
			return nil
		}

		err := mock.StartContainer(ctx, "container1")
		assert.NoError(t, err)
		assert.True(t, mock.WasCalled("StartContainer"))
	})

	t.Run("StopContainer", func(t *testing.T) {
		timeout := 10
		mock.StopContainerFunc = func(ctx context.Context, id string, timeoutPtr *int) error {
			assert.Equal(t, "container1", id)
			assert.Equal(t, 10, *timeoutPtr)
			return nil
		}

		err := mock.StopContainer(ctx, "container1", &timeout)
		assert.NoError(t, err)
	})

	t.Run("PauseContainer", func(t *testing.T) {
		mock.PauseContainerFunc = func(ctx context.Context, id string) error {
			return nil
		}

		err := mock.PauseContainer(ctx, "container1")
		assert.NoError(t, err)
	})

	t.Run("UnpauseContainer", func(t *testing.T) {
		mock.UnpauseContainerFunc = func(ctx context.Context, id string) error {
			return nil
		}

		err := mock.UnpauseContainer(ctx, "container1")
		assert.NoError(t, err)
	})

	t.Run("RestartContainer", func(t *testing.T) {
		mock.RestartContainerFunc = func(ctx context.Context, id string, t *int) error {
			return nil
		}

		err := mock.RestartContainer(ctx, "container1", nil)
		assert.NoError(t, err)
	})

	t.Run("RemoveContainer", func(t *testing.T) {
		mock.RemoveContainerFunc = func(ctx context.Context, id string, force bool, volumes bool) error {
			assert.Equal(t, "container1", id)
			assert.True(t, force)
			assert.False(t, volumes)
			return nil
		}

		err := mock.RemoveContainer(ctx, "container1", true, false)
		assert.NoError(t, err)
	})
}

// TestMockRuntimeContainerTop tests the ContainerTop mock
func TestMockRuntimeContainerTop(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	mock.ContainerTopFunc = func(ctx context.Context, id string) ([]string, [][]string, error) {
		headers := []string{"UID", "PID", "PPID", "C", "STIME", "TTY", "TIME", "CMD"}
		processes := [][]string{
			{"root", "1", "0", "0", "10:00", "?", "00:00:01", "/bin/sh"},
			{"root", "10", "1", "0", "10:00", "?", "00:00:00", "sleep infinity"},
		}
		return headers, processes, nil
	}

	headers, processes, err := mock.ContainerTop(ctx, "container1")
	assert.NoError(t, err)
	assert.Len(t, headers, 8)
	assert.Len(t, processes, 2)
}

// TestMockRuntimePruneContainers tests the PruneContainers mock
func TestMockRuntimePruneContainers(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	mock.PruneContainersFunc = func(ctx context.Context) error {
		return nil
	}

	err := mock.PruneContainers(ctx)
	assert.NoError(t, err)
	assert.True(t, mock.WasCalled("PruneContainers"))
}

// TestMockRuntimeContainerStats tests the ContainerStats mock
func TestMockRuntimeContainerStats(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	t.Run("returns error channel when not implemented", func(t *testing.T) {
		statsChan, errChan := mock.ContainerStats(ctx, "container1", false)
		assert.NotNil(t, statsChan)

		// Stats channel should be closed (empty)
		_, ok := <-statsChan
		assert.False(t, ok, "stats channel should be closed")

		err := <-errChan
		assert.Equal(t, ErrMockNotImplemented, err)
	})

	t.Run("streams stats when function set", func(t *testing.T) {
		mock.ContainerStatsFunc = func(ctx context.Context, id string, stream bool) (<-chan ContainerStatsEntry, <-chan error) {
			statsChan := make(chan ContainerStatsEntry, 2)
			errChan := make(chan error, 1)

			go func() {
				defer close(statsChan)
				defer close(errChan)

				statsChan <- ContainerStatsEntry{
					ID:   id,
					Name: "test-container",
					CPUStats: CPUStats{
						CPUUsage: CPUUsage{TotalUsage: 1000000000},
					},
					MemoryStats: MemoryStats{
						Usage: 104857600,
						Limit: 536870912,
					},
				}
			}()

			return statsChan, errChan
		}

		statsChan, errChan := mock.ContainerStats(ctx, "container1", false)

		select {
		case stats := <-statsChan:
			assert.Equal(t, "container1", stats.ID)
			assert.Equal(t, int64(104857600), stats.MemoryStats.Usage)
		case err := <-errChan:
			t.Fatalf("unexpected error: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for stats")
		}
	})
}

// TestMockRuntimeImageOperations tests image operation mocks
func TestMockRuntimeImageOperations(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	t.Run("ListImages", func(t *testing.T) {
		mock.ListImagesFunc = func(ctx context.Context) ([]ImageSummary, error) {
			return []ImageSummary{
				{ID: "sha256:abc123", RepoTags: []string{"alpine:latest"}, Size: 5242880},
			}, nil
		}

		images, err := mock.ListImages(ctx)
		assert.NoError(t, err)
		assert.Len(t, images, 1)
		assert.Equal(t, "sha256:abc123", images[0].ID)
	})

	t.Run("InspectImage", func(t *testing.T) {
		mock.InspectImageFunc = func(ctx context.Context, id string) (*ImageDetails, error) {
			return &ImageDetails{
				ID:           id,
				RepoTags:     []string{"alpine:latest"},
				Architecture: "amd64",
				Os:           "linux",
			}, nil
		}

		details, err := mock.InspectImage(ctx, "sha256:abc123")
		assert.NoError(t, err)
		assert.Equal(t, "amd64", details.Architecture)
	})

	t.Run("ImageHistory", func(t *testing.T) {
		mock.ImageHistoryFunc = func(ctx context.Context, id string) ([]ImageHistoryEntry, error) {
			return []ImageHistoryEntry{
				{ID: "layer1", CreatedBy: "/bin/sh -c #(nop) CMD [\"/bin/sh\"]"},
				{ID: "layer2", CreatedBy: "/bin/sh -c apk add --no-cache curl"},
			}, nil
		}

		history, err := mock.ImageHistory(ctx, "sha256:abc123")
		assert.NoError(t, err)
		assert.Len(t, history, 2)
	})

	t.Run("RemoveImage", func(t *testing.T) {
		mock.RemoveImageFunc = func(ctx context.Context, id string, force bool) error {
			assert.Equal(t, "sha256:abc123", id)
			assert.True(t, force)
			return nil
		}

		err := mock.RemoveImage(ctx, "sha256:abc123", true)
		assert.NoError(t, err)
	})

	t.Run("PruneImages", func(t *testing.T) {
		mock.PruneImagesFunc = func(ctx context.Context) error {
			return nil
		}

		err := mock.PruneImages(ctx)
		assert.NoError(t, err)
	})
}

// TestMockRuntimeVolumeOperations tests volume operation mocks
func TestMockRuntimeVolumeOperations(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	t.Run("ListVolumes", func(t *testing.T) {
		mock.ListVolumesFunc = func(ctx context.Context) ([]VolumeSummary, error) {
			return []VolumeSummary{
				{Name: "mydata", Driver: "local", Mountpoint: "/var/lib/containers/storage/volumes/mydata/_data"},
			}, nil
		}

		volumes, err := mock.ListVolumes(ctx)
		assert.NoError(t, err)
		assert.Len(t, volumes, 1)
		assert.Equal(t, "mydata", volumes[0].Name)
	})

	t.Run("RemoveVolume", func(t *testing.T) {
		mock.RemoveVolumeFunc = func(ctx context.Context, name string, force bool) error {
			assert.Equal(t, "mydata", name)
			return nil
		}

		err := mock.RemoveVolume(ctx, "mydata", false)
		assert.NoError(t, err)
	})

	t.Run("PruneVolumes", func(t *testing.T) {
		mock.PruneVolumesFunc = func(ctx context.Context) error {
			return nil
		}

		err := mock.PruneVolumes(ctx)
		assert.NoError(t, err)
	})
}

// TestMockRuntimeNetworkOperations tests network operation mocks
func TestMockRuntimeNetworkOperations(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	t.Run("ListNetworks", func(t *testing.T) {
		mock.ListNetworksFunc = func(ctx context.Context) ([]NetworkSummary, error) {
			return []NetworkSummary{
				{Name: "bridge", ID: "net123", Driver: "bridge"},
			}, nil
		}

		networks, err := mock.ListNetworks(ctx)
		assert.NoError(t, err)
		assert.Len(t, networks, 1)
		assert.Equal(t, "bridge", networks[0].Name)
	})

	t.Run("RemoveNetwork", func(t *testing.T) {
		mock.RemoveNetworkFunc = func(ctx context.Context, name string) error {
			assert.Equal(t, "mynetwork", name)
			return nil
		}

		err := mock.RemoveNetwork(ctx, "mynetwork")
		assert.NoError(t, err)
	})

	t.Run("PruneNetworks", func(t *testing.T) {
		mock.PruneNetworksFunc = func(ctx context.Context) error {
			return nil
		}

		err := mock.PruneNetworks(ctx)
		assert.NoError(t, err)
	})
}

// TestMockRuntimeEvents tests the Events mock
func TestMockRuntimeEvents(t *testing.T) {
	mock := &MockRuntime{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("returns error when not implemented", func(t *testing.T) {
		eventChan, errChan := mock.Events(ctx)
		assert.NotNil(t, eventChan)

		// Events channel should be closed (empty)
		_, ok := <-eventChan
		assert.False(t, ok, "events channel should be closed")

		err := <-errChan
		assert.Equal(t, ErrMockNotImplemented, err)
	})

	t.Run("streams events when function set", func(t *testing.T) {
		mock.EventsFunc = func(ctx context.Context) (<-chan Event, <-chan error) {
			eventChan := make(chan Event, 2)
			errChan := make(chan error, 1)

			go func() {
				defer close(eventChan)
				defer close(errChan)

				eventChan <- Event{
					Type:   "container",
					Action: "start",
					Actor: EventActor{
						ID:         "abc123",
						Attributes: map[string]string{"name": "test-container"},
					},
					Time: time.Now().Unix(),
				}
			}()

			return eventChan, errChan
		}

		eventChan, errChan := mock.Events(ctx)

		select {
		case event := <-eventChan:
			assert.Equal(t, "container", event.Type)
			assert.Equal(t, "start", event.Action)
		case err := <-errChan:
			t.Fatalf("unexpected error: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	})
}

// TestMockRuntimeLifecycle tests lifecycle methods
func TestMockRuntimeLifecycle(t *testing.T) {
	mock := &MockRuntime{}

	t.Run("Close returns nil by default", func(t *testing.T) {
		err := mock.Close()
		assert.NoError(t, err)
	})

	t.Run("Close returns custom error when set", func(t *testing.T) {
		customErr := errors.New("close failed")
		mock.CloseFunc = func() error {
			return customErr
		}

		err := mock.Close()
		assert.Equal(t, customErr, err)
	})

	t.Run("Mode returns mock by default", func(t *testing.T) {
		mock2 := &MockRuntime{}
		assert.Equal(t, "mock", mock2.Mode())
	})

	t.Run("Mode returns custom value when set", func(t *testing.T) {
		mock.ModeFunc = func() string {
			return "socket"
		}

		assert.Equal(t, "socket", mock.Mode())
	})
}

// TestMockRuntimeCallTracking tests the call tracking functionality
func TestMockRuntimeCallTracking(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	// Set up functions that don't error
	mock.ListContainersFunc = func(ctx context.Context) ([]ContainerSummary, error) {
		return nil, nil
	}
	mock.InspectContainerFunc = func(ctx context.Context, id string) (*ContainerDetails, error) {
		return nil, nil
	}

	// Make calls
	_, _ = mock.ListContainers(ctx)
	_, _ = mock.ListContainers(ctx)
	_, _ = mock.InspectContainer(ctx, "container1")
	_, _ = mock.InspectContainer(ctx, "container2")
	_ = mock.Close()

	// Verify call counts
	assert.Equal(t, 2, mock.CallCount("ListContainers"))
	assert.Equal(t, 2, mock.CallCount("InspectContainer"))
	assert.Equal(t, 1, mock.CallCount("Close"))
	assert.Equal(t, 0, mock.CallCount("StartContainer"))

	// Verify WasCalled
	assert.True(t, mock.WasCalled("ListContainers"))
	assert.True(t, mock.WasCalled("InspectContainer"))
	assert.False(t, mock.WasCalled("StartContainer"))

	// Verify call arguments
	assert.Len(t, mock.Calls, 5)
	assert.Equal(t, "InspectContainer", mock.Calls[2].Method)
	assert.Equal(t, "container1", mock.Calls[2].Args[0])
	assert.Equal(t, "container2", mock.Calls[3].Args[0])

	// Test Reset
	mock.Reset()
	assert.Len(t, mock.Calls, 0)
	assert.False(t, mock.WasCalled("ListContainers"))
}

// TestMockRuntimeWithPodmanCommand tests integration with PodmanCommand
func TestMockRuntimeWithPodmanCommand(t *testing.T) {
	mock := &MockRuntime{}
	mock.ListContainersFunc = func(ctx context.Context) ([]ContainerSummary, error) {
		return []ContainerSummary{
			{ID: "abc123", Names: []string{"/my-container"}, State: "running"},
		}, nil
	}

	// Create a dummy PodmanCommand and set the mock runtime
	podmanCmd := NewDummyPodmanCommand()
	podmanCmd.Runtime = mock

	// Verify the runtime is set
	assert.NotNil(t, podmanCmd.Runtime)
	assert.Equal(t, "mock", podmanCmd.Runtime.Mode())

	// Test that operations work through PodmanCommand
	containers, err := podmanCmd.Runtime.ListContainers(context.Background())
	assert.NoError(t, err)
	assert.Len(t, containers, 1)
}

// TestContainerRuntimeInterfaceCompleteness ensures all interface methods are tested
func TestContainerRuntimeInterfaceCompleteness(t *testing.T) {
	mock := &MockRuntime{}
	ctx := context.Background()

	// Container operations
	_, _ = mock.ListContainers(ctx)
	_, _ = mock.InspectContainer(ctx, "id")
	_ = mock.StartContainer(ctx, "id")
	_ = mock.StopContainer(ctx, "id", nil)
	_ = mock.PauseContainer(ctx, "id")
	_ = mock.UnpauseContainer(ctx, "id")
	_ = mock.RestartContainer(ctx, "id", nil)
	_ = mock.RemoveContainer(ctx, "id", false, false)
	_, _, _ = mock.ContainerTop(ctx, "id")
	_ = mock.PruneContainers(ctx)
	_, _ = mock.ContainerStats(ctx, "id", false)

	// Image operations
	_, _ = mock.ListImages(ctx)
	_, _ = mock.InspectImage(ctx, "id")
	_, _ = mock.ImageHistory(ctx, "id")
	_ = mock.RemoveImage(ctx, "id", false)
	_ = mock.PruneImages(ctx)

	// Volume operations
	_, _ = mock.ListVolumes(ctx)
	_ = mock.RemoveVolume(ctx, "name", false)
	_ = mock.PruneVolumes(ctx)

	// Network operations
	_, _ = mock.ListNetworks(ctx)
	_ = mock.RemoveNetwork(ctx, "name")
	_ = mock.PruneNetworks(ctx)

	// Events
	_, _ = mock.Events(ctx)

	// Lifecycle
	_ = mock.Close()
	_ = mock.Mode()

	// Verify all methods were called
	expectedMethods := []string{
		"ListContainers", "InspectContainer", "StartContainer", "StopContainer",
		"PauseContainer", "UnpauseContainer", "RestartContainer", "RemoveContainer",
		"ContainerTop", "PruneContainers", "ContainerStats",
		"ListImages", "InspectImage", "ImageHistory", "RemoveImage", "PruneImages",
		"ListVolumes", "RemoveVolume", "PruneVolumes",
		"ListNetworks", "RemoveNetwork", "PruneNetworks",
		"Events", "Close", "Mode",
	}

	for _, method := range expectedMethods {
		assert.True(t, mock.WasCalled(method), "Method %s was not called", method)
	}
}
