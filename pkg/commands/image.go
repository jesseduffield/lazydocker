package commands

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/samber/lo"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
)

// Image : A docker Image
type Image struct {
	Name          string
	Tag           string
	ID            string
	Image         dockerTypes.ImageSummary
	Client        *client.Client
	OSCommand     *OSCommand
	Log           *logrus.Entry
	DockerCommand LimitedDockerCommand
}

// Remove removes the image
func (i *Image) Remove(options dockerTypes.ImageRemoveOptions) error {
	if _, err := i.Client.ImageRemove(context.Background(), i.ID, options); err != nil {
		return err
	}

	return nil
}

func getHistoryResponseItemDisplayStrings(layer image.HistoryResponseItem) []string {
	tag := ""
	if len(layer.Tags) > 0 {
		tag = layer.Tags[0]
	}

	id := strings.TrimPrefix(layer.ID, "sha256:")
	if len(id) > 10 {
		id = id[0:10]
	}
	idColor := color.FgWhite
	if id == "<missing>" {
		idColor = color.FgBlue
	}

	dockerFileCommandPrefix := "/bin/sh -c #(nop) "
	createdBy := layer.CreatedBy
	if strings.Contains(layer.CreatedBy, dockerFileCommandPrefix) {
		createdBy = strings.Trim(strings.TrimPrefix(layer.CreatedBy, dockerFileCommandPrefix), " ")
		split := strings.Split(createdBy, " ")
		createdBy = utils.ColoredString(split[0], color.FgYellow) + " " + strings.Join(split[1:], " ")
	}

	createdBy = strings.Replace(createdBy, "\t", " ", -1)

	size := utils.FormatBinaryBytes(int(layer.Size))
	sizeColor := color.FgWhite
	if size == "0B" {
		sizeColor = color.FgBlue
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

	tableBody := lo.Map(history, func(layer image.HistoryResponseItem, _ int) []string {
		return getHistoryResponseItemDisplayStrings(layer)
	})

	headers := [][]string{{"ID", "TAG", "SIZE", "COMMAND"}}
	table := append(headers, tableBody...)

	return utils.RenderTable(table)
}

// RefreshImages returns a slice of docker images
func (c *DockerCommand) RefreshImages() ([]*Image, error) {
	images, err := c.Client.ImageList(context.Background(), dockerTypes.ImageListOptions{})
	if err != nil {
		return nil, err
	}

	ownImages := make([]*Image, len(images))

	for i, image := range images {
		firstTag := ""
		tags := image.RepoTags
		if len(tags) > 0 {
			firstTag = tags[0]
		}

		nameParts := strings.Split(firstTag, ":")
		tag := ""
		name := "none"
		if len(nameParts) > 1 {
			tag = nameParts[len(nameParts)-1]
			name = strings.Join(nameParts[:len(nameParts)-1], ":")

			for prefix, replacement := range c.Config.UserConfig.Replacements.ImageNamePrefixes {
				if strings.HasPrefix(name, prefix) {
					name = strings.Replace(name, prefix, replacement, 1)
					break
				}
			}
		}

		ownImages[i] = &Image{
			ID:            image.ID,
			Name:          name,
			Tag:           tag,
			Image:         image,
			Client:        c.Client,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			DockerCommand: c,
		}
	}

	return ownImages, nil
}

// PruneImages prunes images
func (c *DockerCommand) PruneImages() error {
	_, err := c.Client.ImagesPrune(context.Background(), filters.Args{})
	return err
}
