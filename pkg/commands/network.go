package commands

import (
	"context"

	"github.com/sirupsen/logrus"
)

// Network represents a Podman network
type Network struct {
	Name          string
	Summary       NetworkSummary
	Runtime       ContainerRuntime
	OSCommand     *OSCommand
	Log           *logrus.Entry
	PodmanCommand LimitedPodmanCommand
}

// Remove removes the network
func (v *Network) Remove() error {
	ctx := context.Background()
	return v.Runtime.RemoveNetwork(ctx, v.Name)
}
