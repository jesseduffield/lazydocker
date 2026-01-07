package commands

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewCommandObject tests the NewCommandObject method
func TestNewCommandObject(t *testing.T) {
	podmanCmd := NewDummyPodmanCommand()

	t.Run("returns default object when empty input", func(t *testing.T) {
		obj := podmanCmd.NewCommandObject(CommandObject{})
		assert.Equal(t, "podman-compose", obj.PodmanCompose)
	})

	t.Run("merges passed object with defaults", func(t *testing.T) {
		container := &Container{ID: "abc123", Name: "test-container"}
		obj := podmanCmd.NewCommandObject(CommandObject{Container: container})

		assert.Equal(t, "podman-compose", obj.PodmanCompose)
		assert.NotNil(t, obj.Container)
		assert.Equal(t, "abc123", obj.Container.ID)
	})
}

// TestPodmanCommandClose tests the Close method
func TestPodmanCommandClose(t *testing.T) {
	t.Run("closes runtime when set", func(t *testing.T) {
		mock := &MockRuntime{}
		closeCalled := false
		mock.CloseFunc = func() error {
			closeCalled = true
			return nil
		}

		podmanCmd := NewDummyPodmanCommand()
		podmanCmd.Runtime = mock

		err := podmanCmd.Close()
		assert.NoError(t, err)
		assert.True(t, closeCalled)
	})

	t.Run("handles nil runtime gracefully", func(t *testing.T) {
		podmanCmd := NewDummyPodmanCommand()
		podmanCmd.Runtime = nil

		err := podmanCmd.Close()
		assert.NoError(t, err)
	})
}

// TestCalculateCPUPercentageFromEntry tests CPU percentage calculation
func TestCalculateCPUPercentageFromEntry(t *testing.T) {
	testCases := []struct {
		name     string
		stats    ContainerStatsEntry
		expected float64
	}{
		{
			name: "normal CPU usage",
			stats: ContainerStatsEntry{
				CPUStats: CPUStats{
					CPUUsage:       CPUUsage{TotalUsage: 2000000000},
					SystemCPUUsage: 20000000000,
				},
				PreCPUStats: CPUStats{
					CPUUsage:       CPUUsage{TotalUsage: 1000000000},
					SystemCPUUsage: 10000000000,
				},
			},
			expected: 10.0, // (1000000000 / 10000000000) * 100 = 10%
		},
		{
			name: "zero system delta",
			stats: ContainerStatsEntry{
				CPUStats: CPUStats{
					CPUUsage:       CPUUsage{TotalUsage: 1000000000},
					SystemCPUUsage: 10000000000,
				},
				PreCPUStats: CPUStats{
					CPUUsage:       CPUUsage{TotalUsage: 500000000},
					SystemCPUUsage: 10000000000,
				},
			},
			expected: 0.0, // system delta is 0
		},
		{
			name: "zero CPU delta",
			stats: ContainerStatsEntry{
				CPUStats: CPUStats{
					CPUUsage:       CPUUsage{TotalUsage: 1000000000},
					SystemCPUUsage: 20000000000,
				},
				PreCPUStats: CPUStats{
					CPUUsage:       CPUUsage{TotalUsage: 1000000000},
					SystemCPUUsage: 10000000000,
				},
			},
			expected: 0.0, // CPU delta is 0
		},
		{
			name: "all zeros",
			stats: ContainerStatsEntry{
				CPUStats:    CPUStats{},
				PreCPUStats: CPUStats{},
			},
			expected: 0.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateCPUPercentageFromEntry(tc.stats)
			assert.InDelta(t, tc.expected, result, 0.01)
		})
	}
}

// TestCalculateMemoryPercentageFromEntry tests memory percentage calculation
func TestCalculateMemoryPercentageFromEntry(t *testing.T) {
	testCases := []struct {
		name     string
		stats    ContainerStatsEntry
		expected float64
	}{
		{
			name: "50% memory usage",
			stats: ContainerStatsEntry{
				MemoryStats: MemoryStats{
					Usage: 256 * 1024 * 1024, // 256 MiB
					Limit: 512 * 1024 * 1024, // 512 MiB
				},
			},
			expected: 50.0,
		},
		{
			name: "100% memory usage",
			stats: ContainerStatsEntry{
				MemoryStats: MemoryStats{
					Usage: 512 * 1024 * 1024,
					Limit: 512 * 1024 * 1024,
				},
			},
			expected: 100.0,
		},
		{
			name: "zero limit",
			stats: ContainerStatsEntry{
				MemoryStats: MemoryStats{
					Usage: 256 * 1024 * 1024,
					Limit: 0,
				},
			},
			expected: 0.0,
		},
		{
			name: "zero usage",
			stats: ContainerStatsEntry{
				MemoryStats: MemoryStats{
					Usage: 0,
					Limit: 512 * 1024 * 1024,
				},
			},
			expected: 0.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := calculateMemoryPercentageFromEntry(tc.stats)
			assert.InDelta(t, tc.expected, result, 0.01)
		})
	}
}

// TestConvertStatsEntryToContainerStats tests stats conversion
func TestConvertStatsEntryToContainerStats(t *testing.T) {
	now := time.Now()
	entry := ContainerStatsEntry{
		Read:    now,
		PreRead: now.Add(-time.Second),
		CPUStats: CPUStats{
			CPUUsage: CPUUsage{
				TotalUsage:        1000000000,
				PercpuUsage:       []int64{500000000, 500000000},
				UsageInKernelmode: 100000000,
				UsageInUsermode:   900000000,
			},
			SystemCPUUsage: 10000000000,
			OnlineCpus:     2,
		},
		PreCPUStats: CPUStats{
			CPUUsage: CPUUsage{
				TotalUsage: 900000000,
			},
			SystemCPUUsage: 9000000000,
			OnlineCpus:     2,
		},
		MemoryStats: MemoryStats{
			Usage:    104857600,
			MaxUsage: 209715200,
			Limit:    536870912,
		},
		PidsStats: PidsStats{
			Current: 10,
		},
		Name: "test-container",
		ID:   "abc123",
	}

	result := convertStatsEntryToContainerStats(entry)

	assert.Equal(t, now, result.Read)
	assert.Equal(t, int64(1000000000), result.CPUStats.CPUUsage.TotalUsage)
	assert.Equal(t, 2, result.CPUStats.OnlineCpus)
	assert.Equal(t, 104857600, result.MemoryStats.Usage)
	assert.Equal(t, int64(536870912), result.MemoryStats.Limit)
	assert.Equal(t, 10, result.PidsStats.Current)
	assert.Equal(t, "test-container", result.Name)
	assert.Equal(t, "abc123", result.ID)
}

// TestPodmanCommandGetContainers tests GetContainers method
func TestPodmanCommandGetContainers(t *testing.T) {
	mock := &MockRuntime{}
	podmanCmd := NewDummyPodmanCommand()
	podmanCmd.Runtime = mock

	t.Run("returns containers from runtime", func(t *testing.T) {
		mock.ListContainersFunc = func(ctx context.Context) ([]ContainerSummary, error) {
			return []ContainerSummary{
				{
					ID:     "abc123",
					Names:  []string{"/my-container"},
					Image:  "alpine:latest",
					State:  "running",
					Labels: map[string]string{},
				},
				{
					ID:     "def456",
					Names:  []string{"/another-container"},
					Image:  "nginx:latest",
					State:  "exited",
					Labels: map[string]string{"name": "custom-name"},
				},
			}, nil
		}

		mock.InspectContainerFunc = func(ctx context.Context, id string) (*ContainerDetails, error) {
			return &ContainerDetails{ID: id}, nil
		}

		containers, err := podmanCmd.GetContainers(nil)
		assert.NoError(t, err)
		assert.Len(t, containers, 2)
		assert.Equal(t, "abc123", containers[0].ID)
		assert.Equal(t, "my-container", containers[0].Name)
		assert.Equal(t, "custom-name", containers[1].Name) // Uses label name
	})

	t.Run("reuses existing container data", func(t *testing.T) {
		existingContainer := &Container{
			ID:   "abc123",
			Name: "existing-container",
		}

		mock.ListContainersFunc = func(ctx context.Context) ([]ContainerSummary, error) {
			return []ContainerSummary{
				{
					ID:     "abc123",
					Names:  []string{"/my-container"},
					State:  "running",
					Labels: map[string]string{},
				},
			}, nil
		}

		containers, err := podmanCmd.GetContainers([]*Container{existingContainer})
		assert.NoError(t, err)
		assert.Len(t, containers, 1)
		assert.Same(t, existingContainer, containers[0])
	})

	t.Run("extracts compose labels", func(t *testing.T) {
		mock.ListContainersFunc = func(ctx context.Context) ([]ContainerSummary, error) {
			return []ContainerSummary{
				{
					ID:    "abc123",
					Names: []string{"/project_service_1"},
					State: "running",
					Labels: map[string]string{
						"com.docker.compose.service":   "web",
						"com.docker.compose.project":   "myproject",
						"com.docker.compose.container": "1",
						"com.docker.compose.oneoff":    "True",
					},
				},
			}, nil
		}

		containers, err := podmanCmd.GetContainers(nil)
		assert.NoError(t, err)
		assert.Len(t, containers, 1)
		assert.Equal(t, "web", containers[0].ServiceName)
		assert.Equal(t, "myproject", containers[0].ProjectName)
		assert.Equal(t, "1", containers[0].ContainerNumber)
		assert.True(t, containers[0].OneOff)
	})
}

// TestPodmanCommandRefreshImages tests RefreshImages method
func TestPodmanCommandRefreshImages(t *testing.T) {
	mock := &MockRuntime{}
	podmanCmd := NewDummyPodmanCommand()
	podmanCmd.Runtime = mock

	t.Run("returns images from runtime", func(t *testing.T) {
		mock.ListImagesFunc = func(ctx context.Context) ([]ImageSummary, error) {
			return []ImageSummary{
				{
					ID:       "sha256:abc123",
					RepoTags: []string{"alpine:latest", "alpine:3.19"},
					Size:     5242880,
				},
				{
					ID:       "sha256:def456",
					RepoTags: []string{"nginx:1.25"},
					Size:     142606336,
				},
			}, nil
		}

		images, err := podmanCmd.RefreshImages()
		assert.NoError(t, err)
		assert.Len(t, images, 2)
		assert.Equal(t, "sha256:abc123", images[0].ID)
		assert.Equal(t, "alpine", images[0].Name)
		assert.Equal(t, "latest", images[0].Tag)
	})

	t.Run("handles images with no tags", func(t *testing.T) {
		mock.ListImagesFunc = func(ctx context.Context) ([]ImageSummary, error) {
			return []ImageSummary{
				{
					ID:       "sha256:abc123",
					RepoTags: []string{},
					Size:     5242880,
				},
			}, nil
		}

		images, err := podmanCmd.RefreshImages()
		assert.NoError(t, err)
		assert.Len(t, images, 1)
		assert.Equal(t, "none", images[0].Name)
		assert.Equal(t, "", images[0].Tag)
	})
}

// TestPodmanCommandRefreshVolumes tests RefreshVolumes method
func TestPodmanCommandRefreshVolumes(t *testing.T) {
	mock := &MockRuntime{}
	podmanCmd := NewDummyPodmanCommand()
	podmanCmd.Runtime = mock

	mock.ListVolumesFunc = func(ctx context.Context) ([]VolumeSummary, error) {
		return []VolumeSummary{
			{
				Name:       "mydata",
				Driver:     "local",
				Mountpoint: "/var/lib/containers/storage/volumes/mydata/_data",
			},
			{
				Name:       "cache",
				Driver:     "local",
				Mountpoint: "/var/lib/containers/storage/volumes/cache/_data",
			},
		}, nil
	}

	volumes, err := podmanCmd.RefreshVolumes()
	assert.NoError(t, err)
	assert.Len(t, volumes, 2)
	assert.Equal(t, "mydata", volumes[0].Name)
	assert.Equal(t, "cache", volumes[1].Name)
}

// TestPodmanCommandRefreshNetworks tests RefreshNetworks method
func TestPodmanCommandRefreshNetworks(t *testing.T) {
	mock := &MockRuntime{}
	podmanCmd := NewDummyPodmanCommand()
	podmanCmd.Runtime = mock

	mock.ListNetworksFunc = func(ctx context.Context) ([]NetworkSummary, error) {
		return []NetworkSummary{
			{Name: "bridge", ID: "net1", Driver: "bridge"},
			{Name: "host", ID: "net2", Driver: "host"},
		}, nil
	}

	networks, err := podmanCmd.RefreshNetworks()
	assert.NoError(t, err)
	assert.Len(t, networks, 2)
	assert.Equal(t, "bridge", networks[0].Name)
	assert.Equal(t, "host", networks[1].Name)
}

// TestPodmanCommandPruneOperations tests prune methods
func TestPodmanCommandPruneOperations(t *testing.T) {
	mock := &MockRuntime{}
	podmanCmd := NewDummyPodmanCommand()
	podmanCmd.Runtime = mock

	t.Run("PruneContainers", func(t *testing.T) {
		mock.PruneContainersFunc = func(ctx context.Context) error {
			return nil
		}

		err := podmanCmd.PruneContainers()
		assert.NoError(t, err)
		assert.True(t, mock.WasCalled("PruneContainers"))
	})

	mock.Reset()

	t.Run("PruneImages", func(t *testing.T) {
		mock.PruneImagesFunc = func(ctx context.Context) error {
			return nil
		}

		err := podmanCmd.PruneImages()
		assert.NoError(t, err)
		assert.True(t, mock.WasCalled("PruneImages"))
	})

	mock.Reset()

	t.Run("PruneVolumes", func(t *testing.T) {
		mock.PruneVolumesFunc = func(ctx context.Context) error {
			return nil
		}

		err := podmanCmd.PruneVolumes()
		assert.NoError(t, err)
		assert.True(t, mock.WasCalled("PruneVolumes"))
	})

	mock.Reset()

	t.Run("PruneNetworks", func(t *testing.T) {
		mock.PruneNetworksFunc = func(ctx context.Context) error {
			return nil
		}

		err := podmanCmd.PruneNetworks()
		assert.NoError(t, err)
		assert.True(t, mock.WasCalled("PruneNetworks"))
	})
}

// TestPodmanCommandAssignContainersToServices tests service assignment
func TestPodmanCommandAssignContainersToServices(t *testing.T) {
	podmanCmd := NewDummyPodmanCommand()

	containers := []*Container{
		{ID: "c1", ServiceName: "web", OneOff: false},
		{ID: "c2", ServiceName: "db", OneOff: false},
		{ID: "c3", ServiceName: "web", OneOff: true}, // OneOff should not be assigned
	}

	services := []*Service{
		{Name: "web"},
		{Name: "db"},
		{Name: "cache"},
	}

	podmanCmd.assignContainersToServices(containers, services)

	assert.Equal(t, containers[0], services[0].Container) // web
	assert.Equal(t, containers[1], services[1].Container) // db
	assert.Nil(t, services[2].Container)                  // cache has no container
}

// TestPodmanCommandGetServices tests GetServices method
func TestPodmanCommandGetServices(t *testing.T) {
	t.Run("returns nil when not in compose project", func(t *testing.T) {
		podmanCmd := NewDummyPodmanCommand()
		podmanCmd.InComposeProject = false

		services, err := podmanCmd.GetServices()
		assert.NoError(t, err)
		assert.Nil(t, services)
	})
}

// TestCommandObjectFields tests CommandObject struct
func TestCommandObjectFields(t *testing.T) {
	container := &Container{ID: "c1", Name: "test"}
	service := &Service{ID: "s1", Name: "web"}
	image := &Image{ID: "i1", Name: "alpine"}
	volume := &Volume{Name: "v1"}
	network := &Network{Name: "n1"}

	obj := CommandObject{
		PodmanCompose: "podman-compose",
		Service:       service,
		Container:     container,
		Image:         image,
		Volume:        volume,
		Network:       network,
	}

	assert.Equal(t, "podman-compose", obj.PodmanCompose)
	assert.Equal(t, container, obj.Container)
	assert.Equal(t, service, obj.Service)
	assert.Equal(t, image, obj.Image)
	assert.Equal(t, volume, obj.Volume)
	assert.Equal(t, network, obj.Network)
}

// TestPodmanCommandImplementsCloser verifies PodmanCommand implements io.Closer
func TestPodmanCommandImplementsCloser(t *testing.T) {
	podmanCmd := NewDummyPodmanCommand()
	var closer interface{} = podmanCmd

	_, ok := closer.(interface{ Close() error })
	assert.True(t, ok)
}
