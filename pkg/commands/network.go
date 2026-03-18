package commands

import (
	"github.com/sirupsen/logrus"
)

type Network struct {
	Name          string
	AppleNetwork  AppleNetwork
	Client        *ContainerClient
	OSCommand     *OSCommand
	Log           *logrus.Entry
	ContainerCmd  LimitedContainerCommand
}

func (n *Network) Remove() error {
	return n.Client.RemoveNetwork(n.Name)
}

func (c *ContainerClient) RefreshNetworks() ([]*Network, error) {
	appleNetworks, err := c.ListNetworks()
	if err != nil {
		return nil, err
	}

	ownNetworks := make([]*Network, len(appleNetworks))

	for i, net := range appleNetworks {
		ownNetworks[i] = &Network{
			Name:         net.ID,
			AppleNetwork: net,
			Client:       c,
			OSCommand:    c.OSCommand,
			Log:          c.Log,
		}
	}

	return ownNetworks, nil
}
