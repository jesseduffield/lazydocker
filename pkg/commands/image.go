package commands

import (
	"strings"

	"github.com/fatih/color"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
	"github.com/sirupsen/logrus"
)

type Image struct {
	Name         string
	Tag          string
	ID           string
	AppleImage   AppleImage
	Client       *ContainerClient
	OSCommand    *OSCommand
	Log          *logrus.Entry
	ContainerCmd LimitedContainerCommand
}

func (i *Image) Remove(force bool) error {
	return i.Client.RemoveImage(i.ID, force)
}

func (i *Image) Pull() error {
	return i.Client.PullImage(i.Name + ":" + i.Tag)
}

func getImageDisplayStrings(img *Image, replacements map[string]string) []string {
	name := img.Name
	for prefix, replacement := range replacements {
		if strings.HasPrefix(name, prefix) {
			name = strings.Replace(name, prefix, replacement, 1)
			break
		}
	}

	id := strings.TrimPrefix(img.AppleImage.Descriptor.Digest, "sha256:")
	if len(id) > 10 {
		id = id[0:10]
	}
	idColor := color.FgWhite
	if id == "<missing>" {
		idColor = color.FgBlue
	}

	size := img.AppleImage.FullSize
	if size == "" {
		size = utils.FormatBinaryBytes(int(img.AppleImage.Descriptor.Size))
	}

	return []string{
		utils.ColoredString(id, idColor),
		utils.ColoredString(name, color.FgGreen),
		utils.ColoredString(img.Tag, color.FgCyan),
		utils.ColoredString(size, color.FgYellow),
	}
}

func (c *ContainerClient) RefreshImages() ([]*Image, error) {
	appleImages, err := c.ListImages()
	if err != nil {
		return nil, err
	}

	ownImages := make([]*Image, len(appleImages))

	for i, img := range appleImages {
		nameParts := strings.Split(img.Reference, ":")
		name := ""
		tag := "latest"
		if len(nameParts) > 1 {
			tag = nameParts[len(nameParts)-1]
			name = strings.Join(nameParts[:len(nameParts)-1], ":")
		} else if len(nameParts) == 1 {
			name = nameParts[0]
		}

		for prefix, replacement := range c.Config.UserConfig.Replacements.ImageNamePrefixes {
			if strings.HasPrefix(name, prefix) {
				name = strings.Replace(name, prefix, replacement, 1)
				break
			}
		}

		ownImages[i] = &Image{
			ID:           img.Descriptor.Digest,
			Name:         name,
			Tag:          tag,
			AppleImage:   img,
			Client:       c,
			OSCommand:    c.OSCommand,
			Log:          c.Log,
			ContainerCmd: c,
		}
	}

	return ownImages, nil
}
