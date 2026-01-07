package gui

import (
	"sort"
	"testing"

	"github.com/christophe-duc/lazypodman/pkg/commands"
	"github.com/stretchr/testify/assert"
)

func sampleContainers() []*commands.Container {
	return []*commands.Container{
		{
			ID:   "1",
			Name: "1",
			Summary: commands.ContainerSummary{
				State: "exited",
			},
		},
		{
			ID:   "2",
			Name: "2",
			Summary: commands.ContainerSummary{
				State: "running",
			},
		},
		{
			ID:   "3",
			Name: "3",
			Summary: commands.ContainerSummary{
				State: "running",
			},
		},
		{
			ID:   "4",
			Name: "4",
			Summary: commands.ContainerSummary{
				State: "created",
			},
		},
	}
}

func expectedPerStatusContainers() []*commands.Container {
	return []*commands.Container{
		{
			ID:   "2",
			Name: "2",
			Summary: commands.ContainerSummary{
				State: "running",
			},
		},
		{
			ID:   "3",
			Name: "3",
			Summary: commands.ContainerSummary{
				State: "running",
			},
		},
		{
			ID:   "1",
			Name: "1",
			Summary: commands.ContainerSummary{
				State: "exited",
			},
		},
		{
			ID:   "4",
			Name: "4",
			Summary: commands.ContainerSummary{
				State: "created",
			},
		},
	}
}

func expectedLegacySortedContainers() []*commands.Container {
	return []*commands.Container{
		{
			ID:   "1",
			Name: "1",
			Summary: commands.ContainerSummary{
				State: "exited",
			},
		},
		{
			ID:   "2",
			Name: "2",
			Summary: commands.ContainerSummary{
				State: "running",
			},
		},
		{
			ID:   "3",
			Name: "3",
			Summary: commands.ContainerSummary{
				State: "running",
			},
		},
		{
			ID:   "4",
			Name: "4",
			Summary: commands.ContainerSummary{
				State: "created",
			},
		},
	}
}

func assertEqualContainers(t *testing.T, left *commands.Container, right *commands.Container) {
	t.Helper()
	assert.Equal(t, left.Summary.State, right.Summary.State)
	assert.Equal(t, left.Summary.ID, right.Summary.ID)
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

func TestSortContainerListItems(t *testing.T) {
	// Create test data: 2 pods with containers and 2 standalone containers
	items := []*commands.ContainerListItem{
		// Standalone container "zebra"
		{
			IsPod: false,
			Container: &commands.Container{
				ID:   "standalone-z",
				Name: "zebra",
				Summary: commands.ContainerSummary{
					State: "running",
				},
			},
			Indent: 0,
		},
		// Pod "beta" with containers
		{
			IsPod: true,
			Pod: &commands.Pod{
				ID:   "pod-beta",
				Name: "beta",
			},
			Indent: 0,
		},
		{
			IsPod: false,
			Container: &commands.Container{
				ID:   "ctr-beta-y",
				Name: "yak",
				Summary: commands.ContainerSummary{
					State:   "running",
					Pod:     "pod-beta",
					PodName: "beta",
				},
			},
			Indent: 2,
		},
		// Standalone container "apple"
		{
			IsPod: false,
			Container: &commands.Container{
				ID:   "standalone-a",
				Name: "apple",
				Summary: commands.ContainerSummary{
					State: "exited",
				},
			},
			Indent: 0,
		},
		// Pod "alpha" with containers
		{
			IsPod: true,
			Pod: &commands.Pod{
				ID:   "pod-alpha",
				Name: "alpha",
			},
			Indent: 0,
		},
		{
			IsPod: false,
			Container: &commands.Container{
				ID:   "ctr-alpha-b",
				Name: "bear",
				Summary: commands.ContainerSummary{
					State:   "running",
					Pod:     "pod-alpha",
					PodName: "alpha",
				},
			},
			Indent: 2,
		},
		{
			IsPod: false,
			Container: &commands.Container{
				ID:   "ctr-alpha-a",
				Name: "ant",
				Summary: commands.ContainerSummary{
					State:   "exited",
					Pod:     "pod-alpha",
					PodName: "alpha",
				},
			},
			Indent: 2,
		},
		{
			IsPod: false,
			Container: &commands.Container{
				ID:   "ctr-beta-x",
				Name: "xray",
				Summary: commands.ContainerSummary{
					State:   "exited",
					Pod:     "pod-beta",
					PodName: "beta",
				},
			},
			Indent: 2,
		},
	}

	// Sort the items
	sort.Slice(items, func(i, j int) bool {
		return sortContainerListItems(items[i], items[j], false)
	})

	// Expected order (with legacySort=false, sorts by state then name):
	// 1. pod alpha (alphabetically first pod)
	// 2.   ant (container in alpha, alphabetically first)
	// 3.   bear (container in alpha)
	// 4. pod beta (alphabetically second pod)
	// 5.   xray (container in beta, alphabetically first)
	// 6.   yak (container in beta)
	// 7. zebra (standalone, running - state 1)
	// 8. apple (standalone, exited - state 2)

	expectedOrder := []string{"alpha", "ant", "bear", "beta", "xray", "yak", "zebra", "apple"}

	assert.Equal(t, len(expectedOrder), len(items))
	for i, item := range items {
		assert.Equal(t, expectedOrder[i], item.Name(), "Item at index %d should be %s but was %s", i, expectedOrder[i], item.Name())
	}
}
