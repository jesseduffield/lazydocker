package commands

import (
	"context"
	"strings"

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

	return []string{utils.ColoredString(i.Name, color.FgWhite), utils.ColoredString(i.Tag, color.FgWhite), utils.FormatDecimalBytes(int(i.Image.Size))}
}

// Remove removes the image
func (i *Image) Remove(options types.ImageRemoveOptions) error {
	if _, err := i.Client.ImageRemove(context.Background(), i.ID, options); err != nil {
		return err
	}

	return nil
}

// Layer is a layer in an image's history
type Layer struct {
	types.ImageHistory
}

// GetDisplayStrings returns the array of strings describing the layer
func (l *Layer) GetDisplayStrings(isFocused bool) []string {
	tag := ""
	if len(l.Tags) > 0 {
		tag = l.Tags[0]
	}

	id := strings.TrimPrefix(l.ID, "sha256:")
	if len(id) > 10 {
		id = id[0:10]
	}
	idColor := color.FgWhite
	if id == "<missing>" {
		idColor = color.FgBlack
	}

	dockerFileCommandPrefix := "/bin/sh -c #(nop) "
	createdBy := l.CreatedBy
	if strings.Contains(l.CreatedBy, dockerFileCommandPrefix) {
		createdBy = strings.Trim(strings.TrimPrefix(l.CreatedBy, dockerFileCommandPrefix), " ")
		split := strings.Split(createdBy, " ")
		createdBy = utils.ColoredString(split[0], color.FgYellow) + " " + strings.Join(split[1:], " ")
	}

	createdBy = strings.ReplaceAll(createdBy, "\t", " ")

	size := utils.FormatBinaryBytes(int(l.Size))
	sizeColor := color.FgWhite
	if size == "0B" {
		sizeColor = color.FgBlack
	}

	return []string{
		utils.ColoredString(id, idColor),
		utils.ColoredString(tag, color.FgGreen),
		utils.ColoredString(size, sizeColor),
		createdBy,
	}
}

// RenderHistory renders the history of the image
func (i *Image) RenderHistory() (string, error) {

	history, err := i.Client.ImageHistory(context.Background(), i.ID)
	if err != nil {
		return "", err
	}

	layers := make([]*Layer, len(history))
	for i, layer := range history {
		layers[i] = &Layer{layer}
	}

	return utils.RenderList(layers, utils.WithHeader([]string{"ID", "TAG", "SIZE", "COMMAND"}))
}
