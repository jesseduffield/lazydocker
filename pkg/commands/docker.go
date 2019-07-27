package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acarl005/stripansi"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/imdario/mergo"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
)

const (
	APIVersion = "1.20"
)

// DockerCommand is our main docker interface
type DockerCommand struct {
	Log                    *logrus.Entry
	OSCommand              *OSCommand
	Tr                     *i18n.TranslationSet
	Config                 *config.AppConfig
	Client                 *client.Client
	InDockerComposeProject bool
	ShowExited             bool
	ErrorChan              chan error
	ContainerMutex         sync.Mutex
	ServiceMutex           sync.Mutex
	Services               []*Service
	Containers             []*Container
	// DisplayContainers is the array of containers we will display in the containers panel. If Gui.ShowAllContainers is false, this will only be those containers which aren't based on a service. This reduces clutter and duplication in the UI
	DisplayContainers []*Container
	Images            []*Image
	Volumes           []*Volume
}

// LimitedDockerCommand is a stripped-down DockerCommand with just the methods the container/service/image might need
type LimitedDockerCommand interface {
	NewCommandObject(CommandObject) CommandObject
}

// CommandObject is what we pass to our template resolvers when we are running a custom command. We do not guarantee that all fields will be populated: just the ones that make sense for the current context
type CommandObject struct {
	DockerCompose string
	Service       *Service
	Container     *Container
	Image         *Image
	Volume        *Volume
}

// NewCommandObject takes a command object and returns a default command object with the passed command object merged in
func (c *DockerCommand) NewCommandObject(obj CommandObject) CommandObject {
	defaultObj := CommandObject{DockerCompose: c.Config.UserConfig.CommandTemplates.DockerCompose}
	mergo.Merge(&defaultObj, obj)
	return defaultObj
}

// NewDockerCommand it runs docker commands
func NewDockerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*DockerCommand, error) {
	cli, err := client.NewClientWithOpts(client.WithVersion(APIVersion))
	if err != nil {
		return nil, err
	}

	dockerCommand := &DockerCommand{
		Log:                    log,
		OSCommand:              osCommand,
		Tr:                     tr,
		Config:                 config,
		Client:                 cli,
		ErrorChan:              errorChan,
		ShowExited:             true,
		InDockerComposeProject: true,
	}

	command := utils.ApplyTemplate(
		config.UserConfig.CommandTemplates.CheckDockerComposeConfig,
		dockerCommand.NewCommandObject(CommandObject{}),
	)

	log.Warn(command)

	err = osCommand.RunCommand(
		utils.ApplyTemplate(
			config.UserConfig.CommandTemplates.CheckDockerComposeConfig,
			dockerCommand.NewCommandObject(CommandObject{}),
		),
	)
	if err != nil {
		dockerCommand.InDockerComposeProject = false
		log.Warn(err.Error())
	}

	return dockerCommand, nil
}

// MonitorContainerStats is a function
func (c *DockerCommand) MonitorContainerStats() {
	go c.MonitorCLIContainerStats()
	go c.MonitorClientContainerStats()
}

// MonitorCLIContainerStats monitors a stream of container stats and updates the containers as each new stats object is received
func (c *DockerCommand) MonitorCLIContainerStats() {
	command := `docker stats --all --no-trunc --format '{{json .}}'`
	cmd := c.OSCommand.RunCustomCommand(command)

	r, err := cmd.StdoutPipe()
	if err != nil {
		c.ErrorChan <- err
		return
	}

	cmd.Start()

	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		var stats ContainerCliStat
		// need to strip ANSI codes because uses escape sequences to clear the screen with each refresh
		cleanString := stripansi.Strip(scanner.Text())
		if err := json.Unmarshal([]byte(cleanString), &stats); err != nil {
			c.ErrorChan <- err
			return
		}
		c.ContainerMutex.Lock()
		for _, container := range c.Containers {
			if container.ID == stats.ID {
				container.CLIStats = stats
			}
		}
		c.ContainerMutex.Unlock()
	}

	cmd.Wait()
}

// MonitorClientContainerStats is a function
func (c *DockerCommand) MonitorClientContainerStats() {
	// periodically loop through running containers and see if we need to create a monitor goroutine for any
	// every second we check if we need to spawn a new goroutine
	for range time.Tick(time.Second) {
		for _, container := range c.Containers {
			if !container.MonitoringStats {
				go c.createClientStatMonitor(container)
			}
		}
	}
}

func (c *DockerCommand) createClientStatMonitor(container *Container) {
	container.MonitoringStats = true
	stream, err := c.Client.ContainerStats(context.Background(), container.ID, true)
	if err != nil {
		c.ErrorChan <- err
		return
	}

	defer stream.Body.Close()

	scanner := bufio.NewScanner(stream.Body)
	for scanner.Scan() {
		data := scanner.Bytes()
		var stats ContainerStats
		json.Unmarshal(data, &stats)

		recordedStats := RecordedStats{
			ClientStats: stats,
			DerivedStats: DerivedStats{
				CPUPercentage:    stats.CalculateContainerCPUPercentage(),
				MemoryPercentage: stats.CalculateContainerMemoryUsage(),
			},
			RecordedAt: time.Now(),
		}

		c.ContainerMutex.Lock()
		container.StatHistory = append(container.StatHistory, recordedStats)
		container.EraseOldHistory()
		c.ContainerMutex.Unlock()
	}

	container.MonitoringStats = false
}

// RefreshContainersAndServices returns a slice of docker containers
func (c *DockerCommand) RefreshContainersAndServices() error {
	c.ServiceMutex.Lock()
	defer c.ServiceMutex.Unlock()

	currentServices := c.Services

	containers, err := c.GetContainers()
	if err != nil {
		return err
	}

	var services []*Service
	// we only need to get these services once because they won't change in the runtime of the program
	if currentServices != nil {
		services = currentServices
	} else {
		services, err = c.GetServices()
		if err != nil {
			return err
		}
	}

	var displayContainers = containers

	if c.InDockerComposeProject {
		var standaloneContainers, err = c.assignContainersToServices(containers, services)
		if err != nil {
			return err
		}

		if !c.Config.UserConfig.Gui.ShowAllContainers {
			displayContainers = standaloneContainers
		}
	}

	// sort services first by whether they have a linked container, and second by alphabetical order
	sort.Slice(services, func(i, j int) bool {
		if len(services[i].Containers) > 0 && len(services[j].Containers) == 0 {
			return true
		}

		if len(services[i].Containers) == 0 && len(services[j].Containers) > 0 {
			return false
		}

		return services[i].Name < services[j].Name
	})

	c.Containers = containers
	c.Services = services
	c.DisplayContainers = c.filterOutExited(displayContainers)

	return nil
}

func (c *DockerCommand) assignContainersToServices(containers []*Container, services []*Service) ([]*Container, error) {
	// Get a list of the Container IDs for this Docker Compose project.
	composeCommand := c.Config.UserConfig.CommandTemplates.DockerCompose
	output, err := c.OSCommand.RunCommandWithOutput(fmt.Sprintf("%s ps -q", composeCommand))
	if err != nil {
		return nil, err
	}

	// Create an index for Container IDs to make lookups less expensive.
	lines := utils.SplitLines(output)
	serviceContainerIds := make(map[string]bool)
	for _, line := range lines {
		serviceContainerIds[line] = true
	}

	// Index Services by name.
	serviceMap := make(map[string]*Service)
	for _, service := range services {
		service.Containers = make([]*Container, 0, 1)
		serviceMap[service.Name] = service
	}

	// Walk the list of Containers, categorizing them as either associated with a
	// Service or as standalone.
	serviceContainerCount := len(serviceContainerIds)
	standaloneContainerCount := len(containers) - serviceContainerCount
	standaloneContainers := make([]*Container, 0, standaloneContainerCount)
	for _, container := range containers {
		if !container.OneOff && serviceContainerIds[container.ID] {
			service := serviceMap[container.ServiceName]
			service.Containers = append(service.Containers, container)
		} else {
			standaloneContainers = append(standaloneContainers, container)
		}
	}

	return standaloneContainers, nil
}

// filterOutExited filters out the exited containers if c.ShowExited is false
func (c *DockerCommand) filterOutExited(containers []*Container) []*Container {
	if c.ShowExited {
		return containers
	}
	toReturn := []*Container{}
	for _, container := range containers {
		if container.Container.State != "exited" {
			toReturn = append(toReturn, container)
		}
	}
	return toReturn
}

// GetContainers gets the docker containers
func (c *DockerCommand) GetContainers() ([]*Container, error) {
	c.ContainerMutex.Lock()
	defer c.ContainerMutex.Unlock()

	existingContainers := c.Containers

	containers, err := c.Client.ContainerList(context.Background(), types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	ownContainers := make([]*Container, len(containers))

	for i, container := range containers {
		var newContainer *Container

		// check if we already data stored against the container
		for _, existingContainer := range existingContainers {
			if existingContainer.ID == container.ID {
				newContainer = existingContainer
				break
			}
		}

		// initialise the container if it's completely new
		if newContainer == nil {
			newContainer = &Container{
				ID:            container.ID,
				Client:        c.Client,
				OSCommand:     c.OSCommand,
				Log:           c.Log,
				Config:        c.Config,
				DockerCommand: c,
				Tr:            c.Tr,
			}
		}

		newContainer.Container = container
		// if the container is made with a name label we will use that
		if name, ok := container.Labels["name"]; ok {
			newContainer.Name = name
		} else {
			newContainer.Name = strings.TrimLeft(container.Names[0], "/")
		}
		newContainer.ServiceName = container.Labels["com.docker.compose.service"]
		newContainer.ProjectName = container.Labels["com.docker.compose.project"]
		newContainer.ContainerNumber = container.Labels["com.docker.compose.container"]
		newContainer.OneOff = container.Labels["com.docker.compose.oneoff"] == "True"

		ownContainers[i] = newContainer
	}

	return ownContainers, nil
}

// GetServices gets services
func (c *DockerCommand) GetServices() ([]*Service, error) {
	if !c.InDockerComposeProject {
		return nil, nil
	}

	composeCommand := c.Config.UserConfig.CommandTemplates.DockerCompose
	output, err := c.OSCommand.RunCommandWithOutput(fmt.Sprintf("%s config --services", composeCommand))
	if err != nil {
		return nil, err
	}

	serviceNames := utils.SplitLines(output)
	services := make([]*Service, len(serviceNames))
	for i, name := range serviceNames {
		services[i] = &Service{
			Name:          name,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			DockerCommand: c,
			Containers:    make([]*Container, 0, 1),
		}
	}

	return services, nil
}

// UpdateContainerDetails attaches the details returned from docker inspect to each of the containers
// this contains a bit more info than what you get from the go-docker client
func (c *DockerCommand) UpdateContainerDetails() error {
	c.ContainerMutex.Lock()
	defer c.ContainerMutex.Unlock()

	containers := c.Containers

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

// ViewAllLogs attaches to a subprocess viewing all the logs from docker-compose
func (c *DockerCommand) ViewAllLogs() (*exec.Cmd, error) {
	cmd := c.OSCommand.ExecutableFromString(
		utils.ApplyTemplate(
			c.OSCommand.Config.UserConfig.CommandTemplates.ViewAllLogs,
			c.NewCommandObject(CommandObject{}),
		),
	)

	c.OSCommand.PrepareForChildren(cmd)

	return cmd, nil
}

// DockerComposeConfig returns the result of 'docker-compose config'
func (c *DockerCommand) DockerComposeConfig() string {
	output, err := c.OSCommand.RunCommandWithOutput(
		utils.ApplyTemplate(
			c.OSCommand.Config.UserConfig.CommandTemplates.DockerComposeConfig,
			c.NewCommandObject(CommandObject{}),
		),
	)

	if err != nil {
		output = err.Error()
	}
	return output
}
