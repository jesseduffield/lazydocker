package commands

import (
	"github.com/sirupsen/logrus"
)

type Volume struct {
	Name         string
	AppleVolume  AppleVolume
	Client       *ContainerClient
	OSCommand    *OSCommand
	Log          *logrus.Entry
	ContainerCmd LimitedContainerCommand
}

func (v *Volume) Remove() error {
	return v.Client.RemoveVolume(v.Name)
}

func (c *ContainerClient) RefreshVolumes() ([]*Volume, error) {
	appleVolumes, err := c.ListVolumes()
	if err != nil {
		return nil, err
	}

	ownVolumes := make([]*Volume, len(appleVolumes))

	for i, vol := range appleVolumes {
		ownVolumes[i] = &Volume{
			Name:        vol.Name,
			AppleVolume: vol,
			Client:      c,
			OSCommand:   c.OSCommand,
			Log:         c.Log,
		}
	}

	return ownVolumes, nil
}
