package gui

import (
	"sort"
	"testing"

	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/stretchr/testify/assert"
)

func sampleContainers() []*commands.Container {
	return []*commands.Container{
		{
			ID:             "1",
			Name:           "1",
			AppleContainer: commands.AppleContainer{Status: "exited"},
		},
		{
			ID:             "2",
			Name:           "2",
			AppleContainer: commands.AppleContainer{Status: "running"},
		},
		{
			ID:             "3",
			Name:           "3",
			AppleContainer: commands.AppleContainer{Status: "running"},
		},
		{
			ID:             "4",
			Name:           "4",
			AppleContainer: commands.AppleContainer{Status: "created"},
		},
	}
}

func expectedPerStatusContainers() []*commands.Container {
	return []*commands.Container{
		{
			ID:             "2",
			Name:           "2",
			AppleContainer: commands.AppleContainer{Status: "running"},
		},
		{
			ID:             "3",
			Name:           "3",
			AppleContainer: commands.AppleContainer{Status: "running"},
		},
		{
			ID:             "1",
			Name:           "1",
			AppleContainer: commands.AppleContainer{Status: "exited"},
		},
		{
			ID:             "4",
			Name:           "4",
			AppleContainer: commands.AppleContainer{Status: "created"},
		},
	}
}

func expectedLegacySortedContainers() []*commands.Container {
	return []*commands.Container{
		{
			ID:             "1",
			Name:           "1",
			AppleContainer: commands.AppleContainer{Status: "exited"},
		},
		{
			ID:             "2",
			Name:           "2",
			AppleContainer: commands.AppleContainer{Status: "running"},
		},
		{
			ID:             "3",
			Name:           "3",
			AppleContainer: commands.AppleContainer{Status: "running"},
		},
		{
			ID:             "4",
			Name:           "4",
			AppleContainer: commands.AppleContainer{Status: "created"},
		},
	}
}

func assertEqualContainers(t *testing.T, left *commands.Container, right *commands.Container) {
	t.Helper()
	assert.Equal(t, left.GetStatus(), right.GetStatus())
	assert.Equal(t, left.ID, right.ID)
	assert.Equal(t, left.Name, right.Name)
}

func TestSortContainers(t *testing.T) {
	actual := sampleContainers()

	expected := expectedPerStatusContainers()

	sort.Slice(actual, func(i, j int) bool {
		return sortContainers(actual[i], actual[j], false)
	})

	assert.Equal(t, len(actual), len(expected))

	for i := 0; i < len(actual); i++ {
		assertEqualContainers(t, expected[i], actual[i])
	}
}

func TestLegacySortedContainers(t *testing.T) {
	actual := sampleContainers()

	expected := expectedLegacySortedContainers()

	sort.Slice(actual, func(i, j int) bool {
		return sortContainers(actual[i], actual[j], true)
	})

	assert.Equal(t, len(actual), len(expected))

	for i := 0; i < len(actual); i++ {
		assertEqualContainers(t, expected[i], actual[i])
	}
}
