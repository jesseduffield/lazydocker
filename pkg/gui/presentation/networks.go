package presentation

import "github.com/christophe-duc/lazypodman/pkg/commands"

func GetNetworkDisplayStrings(network *commands.Network) []string {
	return []string{network.Summary.Driver, network.Name}
}
