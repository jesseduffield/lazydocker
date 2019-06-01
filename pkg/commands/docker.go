package commands

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/sirupsen/logrus"
)

// DockerCommand is our main git interface
type DockerCommand struct {
	Log       *logrus.Entry
	OSCommand *OSCommand
	Tr        *i18n.Localizer
	Config    config.AppConfigurer
	Client    *client.Client
}

// NewDockerCommand it runs git commands
func NewDockerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.Localizer, config config.AppConfigurer) (*DockerCommand, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}

	return &DockerCommand{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    config,
		Client:    cli,
	}, nil
}

// UpdateContainerStats takes a slice of containers and returns the same slice but with new stats added
// TODO: consider using this for everything stats-related
func (c *DockerCommand) UpdateContainerStats(containers []*Container) ([]*Container, error) {
	// TODO: consider using a stream rather than polling
	command := `docker stats --all --no-trunc --no-stream --format '{{json .}}'`
	output, err := c.OSCommand.RunCommandWithOutput(command)
	if err != nil {
		return nil, err
	}

	jsonStats := "[" + strings.Join(
		strings.Split(
			strings.Trim(output, "\n"), "\n",
		), ",",
	) + "]"

	c.Log.Warn(jsonStats)

	var stats []ContainerCliStat
	if err := json.Unmarshal([]byte(jsonStats), &stats); err != nil {
		return nil, err
	}

	for _, stat := range stats {
		for _, container := range containers {
			if container.ID == stat.ID {
				container.Stats = stat
			}
		}
	}

	return containers, nil
}

// GetContainers returns a slice of docker containers
func (c *DockerCommand) GetContainers() ([]*Container, error) {
	containers, err := c.Client.ContainerList(context.Background(), types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	ownContainers := make([]*Container, len(containers))

	ids := []string{}

	for i, container := range containers {
		ids = append(ids, container.ID)

		ownContainers[i] = &Container{
			ID:              container.ID,
			Name:            strings.TrimLeft(container.Names[0], "/"),
			ServiceName:     container.Labels["com.docker.compose.service"],
			ProjectName:     container.Labels["com.docker.compose.project"],
			ContainerNumber: container.Labels["com.docker.compose.container"],
			Container:       container,
			Client:          c.Client,
			OSCommand:       c.OSCommand,
			Log:             c.Log,
		}
	}

	cmd := c.OSCommand.RunCustomCommand("docker inspect " + strings.Join(ids, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	var details []*Details
	if err := json.Unmarshal(output, &details); err != nil {
		return nil, err
	}

	for i, container := range ownContainers {
		container.Details = *details[i]
	}

	// ownContainers, err = c.UpdateContainerStats(ownContainers)
	// if err != nil {
	// 	return nil, err
	// }

	return ownContainers, nil
}

// GetImages returns a slice of docker images
func (c *DockerCommand) GetImages() ([]*Image, error) {
	images, err := c.Client.ImageList(context.Background(), types.ImageListOptions{})
	if err != nil {
		return nil, err
	}

	ownImages := make([]*Image, len(images))

	for i, image := range images {
		// func (cli *Client) ImageHistory(ctx context.Context, imageID string) ([]image.HistoryResponseItem, error)

		name := "none"
		tags := image.RepoTags
		if len(tags) > 0 {
			name = tags[0]
		}

		nameParts := strings.Split(name, ":")

		ownImages[i] = &Image{
			ID:        image.ID,
			Name:      nameParts[0],
			Tag:       nameParts[1],
			Image:     image,
			Client:    c.Client,
			OSCommand: c.OSCommand,
			Log:       c.Log,
		}
	}

	return ownImages, nil
}

// PruneImages prunes images
func (c *DockerCommand) PruneImages() error {
	_, err := c.Client.ImagesPrune(context.Background(), filters.Args{})
	return err
}
