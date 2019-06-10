package commands

import (
	"context"
	"sort"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

// Volume : A docker Volume
type Volume struct {
	Name      string
	Volume    *types.Volume
	Client    *client.Client
	OSCommand *OSCommand
	Log       *logrus.Entry
}

// GetDisplayStrings returns the dispaly string of Container
func (v *Volume) GetDisplayStrings(isFocused bool) []string {
	return []string{v.Volume.Driver, v.Name}
}

// RefreshVolumes gets the volumes and stores them
func (c *DockerCommand) RefreshVolumes() error {
	result, err := c.Client.VolumeList(context.Background(), filters.Args{})
	if err != nil {
		return err
	}

	volumes := result.Volumes

	ownVolumes := make([]*Volume, len(volumes))

	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})

	for i, volume := range volumes {
		ownVolumes[i] = &Volume{
			Name:      volume.Name,
			Volume:    volume,
			Client:    c.Client,
			OSCommand: c.OSCommand,
			Log:       c.Log,
		}
	}

	c.Volumes = ownVolumes

	return nil
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
