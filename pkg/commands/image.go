package commands

import (
	"context"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/samber/lo"

	"github.com/docker/docker/api/types"
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
	Image         types.ImageSummary
	Client        *client.Client
	OSCommand     *OSCommand
	Log           *logrus.Entry
	DockerCommand LimitedDockerCommand
}

// GetDisplayStrings returns the display string of Image
func (i *Image) GetDisplayStrings(isFocused bool) []string {
	return []string{i.Name, i.Tag, utils.FormatDecimalBytes(int(i.Image.Size))}
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
	image.HistoryResponseItem
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
		idColor = color.FgBlue
	}

	dockerFileCommandPrefix := "/bin/sh -c #(nop) "
	createdBy := l.CreatedBy
	if strings.Contains(l.CreatedBy, dockerFileCommandPrefix) {
		createdBy = strings.Trim(strings.TrimPrefix(l.CreatedBy, dockerFileCommandPrefix), " ")
		split := strings.Split(createdBy, " ")
		createdBy = utils.ColoredString(split[0], color.FgYellow) + " " + strings.Join(split[1:], " ")
	}

	createdBy = strings.Replace(createdBy, "\t", " ", -1)

	size := utils.FormatBinaryBytes(int(l.Size))
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

	layers := make([]*Layer, len(history))
	for i, layer := range history {
		layers[i] = &Layer{layer}
	}

	return utils.RenderList(layers, utils.WithHeader([]string{"ID", "TAG", "SIZE", "COMMAND"}))
}

// RefreshImages returns a slice of docker images
func (c *DockerCommand) RefreshImages(filterString string) ([]*Image, error) {
	images, err := c.Client.ImageList(context.Background(), types.ImageListOptions{})
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

	ownImages = lo.Filter(ownImages, func(image *Image, _ int) bool {
		return !lo.SomeBy(c.Config.UserConfig.Ignore, func(ignore string) bool {
			return strings.Contains(image.Name, ignore)
		})
	})

	if filterString != "" {
		ownImages = lo.Filter(ownImages, func(image *Image, _ int) bool {
			return strings.Contains(image.Name, filterString)
		})
	}

	noneLabel := "<none>"

	sort.Slice(ownImages, func(i, j int) bool {
		if ownImages[i].Name == noneLabel && ownImages[j].Name != noneLabel {
			return false
		}

		if ownImages[i].Name != noneLabel && ownImages[j].Name == noneLabel {
			return true
		}

		return ownImages[i].Name < ownImages[j].Name
	})

	return ownImages, nil
}

// PruneImages prunes images
func (c *DockerCommand) PruneImages() error {
	_, err := c.Client.ImagesPrune(context.Background(), filters.Args{})
	return err
}
