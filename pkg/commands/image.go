package commands

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	cliconfig "github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

const (
	dockerHubRegistryAddress = "https://index.docker.io/v1/"
	imageSearchResultLimit   = 50
)

// Image : A docker Image
type Image struct {
	Name          string
	Tag           string
	ID            string
	Image         image.Summary
	Client        *client.Client
	OSCommand     *OSCommand
	Log           *logrus.Entry
	DockerCommand LimitedDockerCommand
}

// Remove removes the image
func (i *Image) Remove(options image.RemoveOptions) error {
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
	images, err := c.Client.ImageList(context.Background(), image.ListOptions{})
	if err != nil {
		return nil, err
	}

	ownImages := make([]*Image, len(images))

	for i, img := range images {
		firstTag := ""
		tags := img.RepoTags
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
			ID:            img.ID,
			Name:          name,
			Tag:           tag,
			Image:         img,
			Client:        c.Client,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			DockerCommand: c,
		}
	}

	return ownImages, nil
}

// SearchImages searches Docker Hub for images matching the supplied term.
func (c *DockerCommand) SearchImages(ctx context.Context, term string) ([]registry.SearchResult, error) {
	registryAuth, err := dockerHubRegistryAuth()
	if err != nil {
		return nil, err
	}

	return c.Client.ImageSearch(ctx, term, registry.SearchOptions{
		Limit:        imageSearchResultLimit,
		RegistryAuth: registryAuth,
	})
}

// PullImage pulls an image into the local Docker daemon and consumes the full
// response stream so that errors reported by the registry are not missed.
func (c *DockerCommand) PullImage(reference string) error {
	registryAuth, err := dockerHubRegistryAuth()
	if err != nil {
		return err
	}

	reader, err := c.Client.ImagePull(context.Background(), reference, image.PullOptions{
		RegistryAuth: registryAuth,
	})
	if err != nil {
		return err
	}
	defer reader.Close()

	return consumeImagePullResponse(reader)
}

func dockerHubRegistryAuth() (string, error) {
	configFile, err := cliconfig.Load(cliconfig.Dir())
	if err != nil {
		return "", err
	}

	authConfig, err := configFile.GetAuthConfig(dockerHubRegistryAddress)
	if err != nil {
		return "", err
	}

	return registry.EncodeAuthConfig(registry.AuthConfig(authConfig))
}

func consumeImagePullResponse(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	for {
		var message struct {
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}

		if err := decoder.Decode(&message); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if message.ErrorDetail.Message != "" {
			return errors.New(message.ErrorDetail.Message)
		}
		if message.Error != "" {
			return errors.New(message.Error)
		}
	}
}

// PruneImages prunes images
func (c *DockerCommand) PruneImages() error {
	_, err := c.Client.ImagesPrune(context.Background(), filters.Args{})
	return err
}
