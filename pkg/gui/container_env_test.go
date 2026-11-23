package gui

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/jesseduffield/lazydocker/pkg/commands"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestContainerEnvAlphabeticalSorting(t *testing.T) {
	// Create a mock Gui with minimal setup
	log := logrus.NewEntry(logrus.New())
	gui := &Gui{
		Log: log,
		Tr:  i18n.NewTranslationSet(log, "en"),
	}

	// Create a container with unsorted environment variables
	c := &commands.Container{}
	c.Details = container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{},
		Config: &container.Config{
			Env: []string{
				"ZEBRA=value1",
				"ALPHA=value2",
				"MIDDLE=value3",
				"BETA=value4",
			},
		},
	}

	// Call containerEnv
	result := gui.containerEnv(c)

	// The result should contain environment variables in alphabetical order
	// Check that ALPHA comes before BETA, BETA before MIDDLE, and MIDDLE before ZEBRA
	alphaIndex := strings.Index(result, "ALPHA")
	betaIndex := strings.Index(result, "BETA")
	middleIndex := strings.Index(result, "MIDDLE")
	zebraIndex := strings.Index(result, "ZEBRA")

	assert.True(t, alphaIndex >= 0, "ALPHA should be present in result")
	assert.True(t, betaIndex >= 0, "BETA should be present in result")
	assert.True(t, middleIndex >= 0, "MIDDLE should be present in result")
	assert.True(t, zebraIndex >= 0, "ZEBRA should be present in result")

	assert.True(t, alphaIndex < betaIndex, "ALPHA should come before BETA")
	assert.True(t, betaIndex < middleIndex, "BETA should come before MIDDLE")
	assert.True(t, middleIndex < zebraIndex, "MIDDLE should come before ZEBRA")
}

func TestContainerEnvEmptyEnvironment(t *testing.T) {
	log := logrus.NewEntry(logrus.New())
	gui := &Gui{
		Log: log,
		Tr:  i18n.NewTranslationSet(log, "en"),
	}

	c := &commands.Container{}
	c.Details = container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{},
		Config: &container.Config{
			Env: []string{},
		},
	}

	result := gui.containerEnv(c)
	assert.Contains(t, result, gui.Tr.NothingToDisplay)
}

func TestContainerEnvDetailsNotLoaded(t *testing.T) {
	log := logrus.NewEntry(logrus.New())
	gui := &Gui{
		Log: log,
		Tr:  i18n.NewTranslationSet(log, "en"),
	}

	c := &commands.Container{}
	// Details is not properly initialized, so DetailsLoaded() will return false

	result := gui.containerEnv(c)
	assert.Contains(t, result, gui.Tr.WaitingForContainerInfo)
}
