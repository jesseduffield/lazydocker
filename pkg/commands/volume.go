package commands

import (
	"context"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// Volume : A docker Volume
type Volume struct {
	Name          string
	Volume        *volume.Volume
	Client        *client.Client
	OSCommand     *OSCommand
	Log           *logrus.Entry
	DockerCommand LimitedDockerCommand
}

// RefreshVolumes gets the volumes and stores them
func (c *DockerCommand) RefreshVolumes() ([]*Volume, error) {
	result, err := c.Client.VolumeList(context.Background(), volume.ListOptions{})
	if err != nil {
		return nil, err
	}

	volumes := result.Volumes

	ownVolumes := make([]*Volume, len(volumes))

	for i, vol := range volumes {
		ownVolumes[i] = &Volume{
			Name:          vol.Name,
			Volume:        vol,
			Client:        c.Client,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			DockerCommand: c,
		}
	}

	return ownVolumes, nil
}

// PruneVolumes prunes volumes
func (c *DockerCommand) PruneVolumes() error {
	_, err := c.Client.VolumesPrune(context.Background(), filters.Args{})
	return err
}

// Remove removes the volume
func (v *Volume) Remove(force bool) error {
	return v.Client.VolumeRemove(context.Background(), v.Name, force)
}
