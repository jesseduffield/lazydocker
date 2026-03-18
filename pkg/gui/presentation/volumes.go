package presentation

import (
	"github.com/jesseduffield/lazycontainer/pkg/commands"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
)

func GetVolumeDisplayStrings(volume *commands.Volume) []string {
	return []string{utils.FormatBinaryBytes(int(volume.AppleVolume.SizeInBytes)), volume.Name}
}
