package commands

import (
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
)

// Image : A docker Image
type Image struct {
	Name      string
	ID        string
	Image     types.ImageSummary
	Client    *client.Client
	OSCommand *OSCommand
	Log       *logrus.Entry
}

// GetDisplayStrings returns the display string of Image
func (i *Image) GetDisplayStrings(isFocused bool) []string {
	return []string{utils.ColoredString(i.Name, color.FgWhite)}
}
