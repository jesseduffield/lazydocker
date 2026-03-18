package presentation

import "github.com/jesseduffield/lazycontainer/pkg/commands"

func GetNetworkDisplayStrings(network *commands.Network) []string {
	return []string{network.AppleNetwork.Config.Mode, network.Name}
}
