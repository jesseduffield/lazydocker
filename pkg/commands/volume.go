package commands

import (
	"context"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

// Volume : A docker Volume
type Volume struct {
	Name          string
	Volume        *types.Volume
	Client        *client.Client
	OSCommand     *OSCommand
	Log           *logrus.Entry
	DockerCommand LimitedDockerCommand
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

	// we're sorting these volumes based on whether they have labels defined,
	// because those are the ones you typically care about.
	// Within that, we also sort them alphabetically
	sort.Slice(volumes, func(i, j int) bool {
		if len(volumes[i].Labels) == 0 && len(volumes[j].Labels) > 0 {
			return false
		}
		if len(volumes[i].Labels) > 0 && len(volumes[j].Labels) == 0 {
			return true
		}
		return volumes[i].Name < volumes[j].Name
	})

	for i, volume := range volumes {
		ownVolumes[i] = &Volume{
			Name:          volume.Name,
			Volume:        volume,
			Client:        c.Client,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			DockerCommand: c,
		}
	}

	ownVolumes = lo.Filter(ownVolumes, func(volume *Volume, _ int) bool {
		return !lo.SomeBy(c.Config.UserConfig.Ignore, func(ignore string) bool {
			return strings.Contains(volume.Name, ignore)
		})
	})

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
