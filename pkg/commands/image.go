package commands

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
)

// Image : A docker Image
type Image struct {
	Name      string
	Tag       string
	ID        string
	Image     types.ImageSummary
	Client    *client.Client
	OSCommand *OSCommand
	Log       *logrus.Entry
}

// GetDisplayStrings returns the display string of Image
func (i *Image) GetDisplayStrings(isFocused bool) []string {
	return []string{utils.ColoredString(i.Name, color.FgWhite), utils.ColoredString(i.Tag, color.FgWhite)}
}

// Remove removes the image
func (i *Image) Remove(options types.ImageRemoveOptions) error {
	if _, err := i.Client.ImageRemove(context.Background(), i.ID, options); err != nil {
		return err
	}

	return nil
}
