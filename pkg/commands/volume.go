package commands

import (
	"context"

	"github.com/sirupsen/logrus"
)

// Volume represents a Podman volume
type Volume struct {
	Name          string
	Summary       VolumeSummary
	Runtime       ContainerRuntime
	OSCommand     *OSCommand
	Log           *logrus.Entry
	PodmanCommand LimitedPodmanCommand
}

// Remove removes the volume
func (v *Volume) Remove(force bool) error {
	ctx := context.Background()
	return v.Runtime.RemoveVolume(ctx, v.Name, force)
}
