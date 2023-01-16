package presentation

import "github.com/jesseduffield/lazydocker/pkg/commands"

func GetNetworkDisplayStrings(network *commands.Network) []string {
	return []string{network.Network.Driver, network.Name}
}
