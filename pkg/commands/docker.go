package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
)

// DockerCommand is our main git interface
type DockerCommand struct {
	Log                    *logrus.Entry
	OSCommand              *OSCommand
	Tr                     *i18n.Localizer
	Config                 *config.AppConfig
	Client                 *client.Client
	InDockerComposeProject bool
}

// NewDockerCommand it runs git commands
func NewDockerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.Localizer, config *config.AppConfig) (*DockerCommand, error) {
	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}

	return &DockerCommand{
		Log:                    log,
		OSCommand:              osCommand,
		Tr:                     tr,
		Config:                 config,
		Client:                 cli,
		InDockerComposeProject: true, // TODO: determine this at startup
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

// GetContainersAndServices returns a slice of docker containers
func (c *DockerCommand) GetContainersAndServices(currentServices []*Service) ([]*Container, []*Service, error) {

	containers, err := c.GetContainers()
	if err != nil {
		return nil, nil, err
	}

	var services []*Service
	// we only need to get these services once because they won't change in the runtime of the program
	if currentServices != nil {
		services = currentServices
	} else {
		services, err = c.GetServices()
		if err != nil {
			return nil, nil, err
		}
	}

	// find out which services have corresponding containers and assign them
	for _, service := range services {
		for _, container := range containers {
			if container.ServiceID != "" && container.ServiceID == service.ID {
				service.Container = container
			}
		}
	}

	// sort services first by whether they have a linked container, and second by alphabetical order
	sort.Slice(services, func(i, j int) bool {
		if services[i].Container != nil && services[j].Container == nil {
			return true
		}

		if services[i].Container == nil && services[j].Container != nil {
			return false
		}

		return services[i].Name < services[j].Name
	})

	return containers, services, nil
}

// GetContainers gets the docker containers
func (c *DockerCommand) GetContainers() ([]*Container, error) {
	containers, err := c.Client.ContainerList(context.Background(), types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	ownContainers := make([]*Container, len(containers))

	for i, container := range containers {
		ownContainers[i] = &Container{
			ID:              container.ID,
			Name:            strings.TrimLeft(container.Names[0], "/"),
			ServiceName:     container.Labels["com.docker.compose.service"],
			ServiceID:       container.Labels["com.docker.compose.config-hash"],
			ProjectName:     container.Labels["com.docker.compose.project"],
			ContainerNumber: container.Labels["com.docker.compose.container"],
			Container:       container,
			Client:          c.Client,
			OSCommand:       c.OSCommand,
			Log:             c.Log,
		}
	}

	c.UpdateContainerDetails(ownContainers)

	// ownContainers, err = c.UpdateContainerStats(ownContainers)
	// if err != nil {
	// 	return nil, err
	// }

	return ownContainers, nil
}

// GetServices gets services
func (c *DockerCommand) GetServices() ([]*Service, error) {
	if !c.InDockerComposeProject {
		return nil, nil
	}

	composeCommand := c.Config.UserConfig.CommandTemplates.DockerCompose
	output, err := c.OSCommand.RunCommandWithOutput(fmt.Sprintf("%s config --hash=*", composeCommand))
	if err != nil {
		return nil, err
	}

	// output looks like:
	// service1 998d6d286b0499e0ff23d66302e720991a2asdkf9c30d0542034f610daf8a971
	// service2 asdld98asdklasd9bccd02438de0994f8e19cbe691feb3755336ec5ca2c55971

	lines := utils.SplitLines(output)
	services := make([]*Service, len(lines))
	for i, str := range lines {
		arr := strings.Split(str, " ")
		services[i] = &Service{
			Name:      arr[0],
			ID:        arr[1],
			OSCommand: c.OSCommand,
			Log:       c.Log,
		}
	}

	return services, nil
}

// UpdateContainerDetails attaches the details returned from docker inspect to each of the containers
// this contains a bit more info than what you get from the go-docker client
func (c *DockerCommand) UpdateContainerDetails(containers []*Container) error {
	ids := make([]string, len(containers))
	for i, container := range containers {
		ids[i] = container.ID
	}

	cmd := c.OSCommand.RunCustomCommand("docker inspect " + strings.Join(ids, " "))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	var details []*Details
	if err := json.Unmarshal(output, &details); err != nil {
		return err
	}

	for i, container := range containers {
		container.Details = *details[i]
	}

	return nil
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
