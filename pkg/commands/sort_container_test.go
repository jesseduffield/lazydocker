package commands

import (
	"github.com/docker/docker/api/types"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/stretchr/testify/assert"
	"testing"
)

func sampleContainers(userConfig *config.AppConfig) []*Container {
	return []*Container{
		{
			ID: "1",
			Container: types.Container{
				State: "exited",
			},
			Config: userConfig,
		},
		{
			ID: "2",
			Container: types.Container{
				State: "running",
			},
			Config: userConfig,
		},
		{
			ID: "3",
			Container: types.Container{
				State: "running",
			},
			Config: userConfig,
		},
		{
			ID: "4",
			Container: types.Container{
				State: "created",
			},
			Config: userConfig,
		},
	}
}

func expectedPerStatusContainers(appConfig *config.AppConfig) []*Container {
	return []*Container{
		{
			ID: "2",
			Container: types.Container{
				State: "running",
			},
			Config: appConfig,
		},
		{
			ID: "3",
			Container: types.Container{
				State: "running",
			},
			Config: appConfig,
		},
		{
			ID: "1",
			Container: types.Container{
				State: "exited",
			},
			Config: appConfig,
		},
		{
			ID: "4",
			Container: types.Container{
				State: "created",
			},
			Config: appConfig,
		},
	}
}

func expectedLegacySortedContainers(appConfig *config.AppConfig) []*Container {
	return []*Container{
		{
			ID: "1",
			Container: types.Container{
				State: "exited",
			},
			Config: appConfig,
		},
		{
			ID: "2",
			Container: types.Container{
				State: "running",
			},
			Config: appConfig,
		},
		{
			ID: "3",
			Container: types.Container{
				State: "running",
			},
			Config: appConfig,
		},
		{
			ID: "4",
			Container: types.Container{
				State: "created",
			},
			Config: appConfig,
		},
	}
}

func TestSortContainers(t *testing.T) {
	appConfig := NewDummyAppConfig()
	appConfig.UserConfig = &config.UserConfig{
		Gui: config.GuiConfig{
			SortContainersByName: false,
		},
	}
	command := &DockerCommand{
		Config: appConfig,
	}

	containers := sampleContainers(appConfig)

	sorted := expectedPerStatusContainers(appConfig)

	ct := command.sortedContainers(containers)

	assert.Equal(t, len(ct), len(sorted))

	for i := 0; i < len(ct); i++ {
		assert.Equal(t, ct[i].Container.State, sorted[i].Container.State)
		assert.Equal(t, ct[i].Container.ID, sorted[i].Container.ID)
	}
}

func TestLegacySortedContainers(t *testing.T) {
	appConfig := NewDummyAppConfig()
	appConfig.UserConfig = &config.UserConfig{
		Gui: config.GuiConfig{
			SortContainersByName: true,
		},
	}
	command := &DockerCommand{
		Config: appConfig,
	}

	containers := sampleContainers(appConfig)

	sorted := expectedLegacySortedContainers(appConfig)

	ct := command.sortedContainers(containers)

	for i := 0; i < len(ct); i++ {
		assert.Equal(t, sorted[i].Container.State, ct[i].Container.State)
		assert.Equal(t, sorted[i].Container.ID, ct[i].Container.ID)
	}
}
