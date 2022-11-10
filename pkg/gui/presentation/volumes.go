package presentation

import "github.com/jesseduffield/lazydocker/pkg/commands"

func GetVolumeDisplayStrings(volume *commands.Volume) []string {
	return []string{volume.Volume.Driver, volume.Name}
}
