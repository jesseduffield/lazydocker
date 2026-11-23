package gui

import (
	"strings"
	"testing"

	dockernetwork "github.com/docker/docker/api/types/network"
)

func TestNetworkConfigStrShowsContainers(t *testing.T) {
	output := networkConfigStr("bridge", dockernetwork.Inspect{
		ID:     "network-id",
		Name:   "bridge",
		Driver: "bridge",
		Scope:  "local",
		Containers: map[string]dockernetwork.EndpointResource{
			"endpoint-1": {
				Name:       "app",
				EndpointID: "endpoint-1",
			},
		},
	})

	if !strings.Contains(output, "Containers:") {
		t.Fatalf("expected containers section, got %q", output)
	}

	if !strings.Contains(output, "app") || !strings.Contains(output, "endpoint-1") {
		t.Fatalf("expected container details in output, got %q", output)
	}
}

func TestNetworkConfigStrShowsNoneWhenEmpty(t *testing.T) {
	output := networkConfigStr("bridge", dockernetwork.Inspect{
		ID:     "network-id",
		Name:   "bridge",
		Driver: "bridge",
		Scope:  "local",
	})

	if !strings.Contains(output, "Containers:") || !strings.Contains(output, "none") {
		t.Fatalf("expected empty containers output, got %q", output)
	}
}
