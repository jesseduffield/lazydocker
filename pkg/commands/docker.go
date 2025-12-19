package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	ogLog "log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	cliconfig "github.com/docker/cli/cli/config"
	ddocker "github.com/docker/cli/cli/context/docker"
	ctxstore "github.com/docker/cli/cli/context/store"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/imdario/mergo"
	"github.com/jesseduffield/lazydocker/pkg/commands/ssh"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
)

const (
	dockerHostEnvKey = "DOCKER_HOST"
)

// DockerCommand is our main docker interface
type DockerCommand struct {
	Log                    *logrus.Entry
	OSCommand              *OSCommand
	Tr                     *i18n.TranslationSet
	Config                 *config.AppConfig
	Client                 *client.Client
	InDockerComposeProject bool
	ErrorChan              chan error
	ContainerMutex         deadlock.Mutex
	ServiceMutex           deadlock.Mutex

	Closers []io.Closer
}

var _ io.Closer = &DockerCommand{}

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
	Network       *Network
}

// NewCommandObject takes a command object and returns a default command object with the passed command object merged in
func (c *DockerCommand) NewCommandObject(obj CommandObject) CommandObject {
	defaultObj := CommandObject{DockerCompose: c.Config.UserConfig.CommandTemplates.DockerCompose}
	_ = mergo.Merge(&defaultObj, obj)
	return defaultObj
}

// NewDockerCommand it runs docker commands
func NewDockerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*DockerCommand, error) {
	dockerHost, err := determineDockerHost()
	if err != nil {
		ogLog.Printf("> could not determine host %v", err)
	}

	// NOTE: Inject the determined docker host to the environment. This allows the
	//       `SSHHandler.HandleSSHDockerHost()` to create a local unix socket tunneled
	//       over SSH to the specified ssh host.
	if strings.HasPrefix(dockerHost, "ssh://") {
		os.Setenv(dockerHostEnvKey, dockerHost)
	}

	tunnelCloser, err := ssh.NewSSHHandler(osCommand).HandleSSHDockerHost()
	if err != nil {
		ogLog.Fatal(err)
	}

	// Retrieve the docker host from the environment which could have been set by
	// the `SSHHandler.HandleSSHDockerHost()` and override `dockerHost`.
	dockerHostFromEnv := os.Getenv(dockerHostEnvKey)
	if dockerHostFromEnv != "" {
		dockerHost = dockerHostFromEnv
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation(), client.WithHost(dockerHost))
	if err != nil {
		ogLog.Fatal(err)
	}

	dockerCommand := &DockerCommand{
		Log:                    log,
		OSCommand:              osCommand,
		Tr:                     tr,
		Config:                 config,
		Client:                 cli,
		ErrorChan:              errorChan,
		InDockerComposeProject: true,
		Closers:                []io.Closer{tunnelCloser},
	}

	dockerCommand.setDockerComposeCommand(config)

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

func (c *DockerCommand) setDockerComposeCommand(config *config.AppConfig) {
	if config.UserConfig.CommandTemplates.DockerCompose != "docker compose" {
		return
	}

	// it's possible that a user is still using docker-compose, so we'll check if 'docker comopose' is available, and if not, we'll fall back to 'docker-compose'
	err := c.OSCommand.RunCommand("docker compose version")
	if err != nil {
		config.UserConfig.CommandTemplates.DockerCompose = "docker-compose"
	}
}

func (c *DockerCommand) Close() error {
	return utils.CloseMany(c.Closers)
}

func (c *DockerCommand) CreateClientStatMonitor(container *Container) {
	container.MonitoringStats = true
	stream, err := c.Client.ContainerStats(context.Background(), container.ID, true)
	if err != nil {
		// not creating error panel because if we've disconnected from docker we'll
		// have already created an error panel
		c.Log.Error(err)
		container.MonitoringStats = false
		return
	}

	defer stream.Body.Close()

	scanner := bufio.NewScanner(stream.Body)
	for scanner.Scan() {
		data := scanner.Bytes()
		var stats ContainerStats
		_ = json.Unmarshal(data, &stats)

		recordedStats := &RecordedStats{
			ClientStats: stats,
			DerivedStats: DerivedStats{
				CPUPercentage:    stats.CalculateContainerCPUPercentage(),
				MemoryPercentage: stats.CalculateContainerMemoryUsage(),
			},
			RecordedAt: time.Now(),
		}

		container.appendStats(recordedStats, c.Config.UserConfig.Stats.MaxDuration)
	}

	container.MonitoringStats = false
}

func (c *DockerCommand) RefreshContainersAndServices(currentServices []*Service, currentContainers []*Container) ([]*Container, []*Service, error) {
	c.ServiceMutex.Lock()
	defer c.ServiceMutex.Unlock()

	containers, err := c.GetContainers(currentContainers)
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

	c.assignContainersToServices(containers, services)

	return containers, services, nil
}

func (c *DockerCommand) assignContainersToServices(containers []*Container, services []*Service) {
L:
	for _, service := range services {
		for _, ctr := range containers {
			if !ctr.OneOff && ctr.ServiceName == service.Name {
				service.Container = ctr
				continue L
			}
		}
		service.Container = nil
	}
}

// GetContainers gets the docker containers
func (c *DockerCommand) GetContainers(existingContainers []*Container) ([]*Container, error) {
	c.ContainerMutex.Lock()
	defer c.ContainerMutex.Unlock()

	containers, err := c.Client.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	ownContainers := make([]*Container, len(containers))

	for i, ctr := range containers {
		var newContainer *Container

		// check if we already have data stored against the container
		for _, existingContainer := range existingContainers {
			if existingContainer.ID == ctr.ID {
				newContainer = existingContainer
				break
			}
		}

		// initialise the container if it's completely new
		if newContainer == nil {
			newContainer = &Container{
				ID:            ctr.ID,
				Client:        c.Client,
				OSCommand:     c.OSCommand,
				Log:           c.Log,
				DockerCommand: c,
				Tr:            c.Tr,
			}
		}

		newContainer.Container = ctr
		// if the container is made with a name label we will use that
		if name, ok := ctr.Labels["name"]; ok {
			newContainer.Name = name
		} else {
			if len(ctr.Names) > 0 {
				newContainer.Name = strings.TrimLeft(ctr.Names[0], "/")
			} else {
				newContainer.Name = ctr.ID
			}
		}
		newContainer.ServiceName = ctr.Labels["com.docker.compose.service"]
		newContainer.ProjectName = ctr.Labels["com.docker.compose.project"]
		newContainer.ContainerNumber = ctr.Labels["com.docker.compose.container"]
		newContainer.OneOff = ctr.Labels["com.docker.compose.oneoff"] == "True"

		ownContainers[i] = newContainer
	}

	c.SetContainerDetails(ownContainers)

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

	// output looks like:
	// service1
	// service2

	lines := utils.SplitLines(output)
	services := make([]*Service, len(lines))
	for i, str := range lines {
		services[i] = &Service{
			Name:          str,
			ID:            str,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			DockerCommand: c,
		}
	}

	return services, nil
}

func (c *DockerCommand) RefreshContainerDetails(containers []*Container) error {
	c.ContainerMutex.Lock()
	defer c.ContainerMutex.Unlock()

	c.SetContainerDetails(containers)

	return nil
}

// Attaches the details returned from docker inspect to each of the containers
// this contains a bit more info than what you get from the go-docker client
func (c *DockerCommand) SetContainerDetails(containers []*Container) {
	wg := sync.WaitGroup{}
	for _, ctr := range containers {
		ctr := ctr
		wg.Add(1)
		go func() {
			details, err := c.Client.ContainerInspect(context.Background(), ctr.ID)
			if err != nil {
				c.Log.Error(err)
			} else {
				ctr.Details = details
			}
			wg.Done()
		}()
	}
	wg.Wait()
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

// determineDockerHost tries to the determine the docker host that we should connect to
// in the following order of decreasing precedence:
//   - value of "DOCKER_HOST" environment variable
//   - host retrieved from the current context (specified via DOCKER_CONTEXT)
//   - "default docker host" for the host operating system, otherwise
func determineDockerHost() (string, error) {
	// If the docker host is explicitly set via the "DOCKER_HOST" environment variable,
	// then its a no-brainer :shrug:
	if os.Getenv("DOCKER_HOST") != "" {
		return os.Getenv("DOCKER_HOST"), nil
	}

	currentContext := os.Getenv("DOCKER_CONTEXT")
	if currentContext == "" {
		cf, err := cliconfig.Load(cliconfig.Dir())
		if err != nil {
			return "", err
		}
		currentContext = cf.CurrentContext
	}

	// On some systems (windows) `default` is stored in the docker config as the currentContext.
	if currentContext == "" || currentContext == "default" {
		// If a docker context is neither specified via the "DOCKER_CONTEXT" environment variable nor via the
		// $HOME/.docker/config file, then we fall back to connecting to the "default docker host" meant for
		// the host operating system.
		return defaultDockerHost, nil
	}

	storeConfig := ctxstore.NewConfig(
		func() interface{} { return &ddocker.EndpointMeta{} },
		ctxstore.EndpointTypeGetter(ddocker.DockerEndpoint, func() interface{} { return &ddocker.EndpointMeta{} }),
	)

	st := ctxstore.New(cliconfig.ContextStoreDir(), storeConfig)
	md, err := st.GetMetadata(currentContext)
	if err != nil {
		return "", err
	}
	dockerEP, ok := md.Endpoints[ddocker.DockerEndpoint]
	if !ok {
		return "", err
	}
	dockerEPMeta, ok := dockerEP.(ddocker.EndpointMeta)
	if !ok {
		return "", fmt.Errorf("expected docker.EndpointMeta, got %T", dockerEP)
	}

	if dockerEPMeta.Host != "" {
		return dockerEPMeta.Host, nil
	}

	// We might end up here, if the context was created with the `host` set to an empty value (i.e. '').
	// For example:
	// ```sh
	// docker context create foo --docker "host="
	// ```
	// In such scenario, we mimic the `docker` cli and try to connect to the "default docker host".
	return defaultDockerHost, nil
}
