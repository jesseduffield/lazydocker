package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	ogLog "log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"
	"syscall"
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
	APIVersion = "1.25"
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
	Closers           []io.Closer
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

// handleSSHDockerHost overrides the DOCKER_HOST environment variable
// to point towards a local unix socket tunneled over SSH to the specified ssh host.
func handleSSHDockerHost() (io.Closer, error) {
	const key = "DOCKER_HOST"
	ctx := context.Background()
	u, err := url.Parse(os.Getenv(key))
	if err != nil {
		// if no or an invalid docker host is specified, continue nominally
		return noopCloser{}, nil
	}

	// if the docker host scheme is "ssh", forward the docker socket before creating the client
	if u.Scheme == "ssh" {
		tunnel, err := createDockerHostTunnel(ctx, u.Host)
		if err != nil {
			return noopCloser{}, fmt.Errorf("tunnel ssh docker host: %w", err)
		}
		err = os.Setenv(key, tunnel.SocketPath)
		if err != nil {
			return noopCloser{}, fmt.Errorf("override DOCKER_HOST to tunneled socket: %w", err)
		}

		return tunnel, nil
	}
	return noopCloser{}, nil
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

type TunneledDockerHost struct {
	SocketPath string
	cmd        *exec.Cmd
}

var _ io.Closer = (*TunneledDockerHost)(nil)

func (t *TunneledDockerHost) Close() error {
	return syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
}

func createDockerHostTunnel(ctx context.Context, remoteHost string) (*TunneledDockerHost, error) {
	socketDir, err := ioutil.TempDir("/tmp", "lazydocker-sshtunnel-")
	if err != nil {
		return nil, fmt.Errorf("create ssh tunnel tmp file: %w", err)
	}
	localSocket := path.Join(socketDir, "dockerhost.sock")

	cmd, err := tunnelSSH(ctx, remoteHost, localSocket)
	if err != nil {
		return nil, fmt.Errorf("tunnel docker host over ssh: %w", err)
	}

	// set a reasonable timeout, then wait for the socket to dial successfully
	// before attempting to create a new docker client
	const socketTunnelTimeout = 8 * time.Second
	ctx, cancel := context.WithTimeout(ctx, socketTunnelTimeout)
	defer cancel()

	err = retrySocketDial(ctx, localSocket)
	if err != nil {
		return nil, fmt.Errorf("ssh tunneled socket never became available: %w", err)
	}

	// construct the new DOCKER_HOST url with the proper scheme
	newDockerHostURL := url.URL{Scheme: "unix", Path: localSocket}
	return &TunneledDockerHost{
		SocketPath: newDockerHostURL.String(),
		cmd:        cmd,
	}, nil
}

// Attempt to dial the socket until it becomes available.
// The retry loop will continue until the parent context is canceled.
func retrySocketDial(ctx context.Context, socketPath string) error {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		// attempt to dial the socket, exit on success
		err := tryDial(ctx, socketPath)
		if err != nil {
			continue
		}
		return nil
	}
}

// Try to dial the specified unix socket, immediately close the connection if successfully created.
func tryDial(ctx context.Context, socketPath string) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}
func tunnelSSH(ctx context.Context, host, localSocket string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "ssh", "-L", localSocket+":/var/run/docker.sock", host, "-N")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Start()
	if err != nil {
		return nil, err
	}
	return cmd, nil
}

// Build a new docker client from the environment.
//
// Handle special cases including `ssh://` host schemes.
func clientBuilder(c *client.Client) error {
	return nil
}

// NewDockerCommand it runs docker commands
func NewDockerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*DockerCommand, error) {
	tunnelCloser, err := handleSSHDockerHost()
	if err != nil {
		ogLog.Fatal(err)
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithVersion(APIVersion))
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
		ShowExited:             true,
		InDockerComposeProject: true,
		Closers:                []io.Closer{tunnelCloser},
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

func (c *DockerCommand) Close() error {
	return utils.CloseMany(c.Closers)
}

// MonitorContainerStats is a function
func (c *DockerCommand) MonitorContainerStats() {
	// TODO: pass in a stop channel to these so we don't restart every time we come back from a subprocess
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
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
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

	c.assignContainersToServices(containers, services)

	var displayContainers = containers
	if !c.Config.UserConfig.Gui.ShowAllContainers {
		displayContainers = c.obtainStandaloneContainers(containers, services)
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

	c.Containers = containers
	c.Services = services
	c.DisplayContainers = c.filterOutExited(displayContainers)

	return nil
}

func (c *DockerCommand) assignContainersToServices(containers []*Container, services []*Service) {
L:
	for _, service := range services {
		for _, container := range containers {
			if !container.OneOff && container.ServiceName == service.Name {
				service.Container = container
				continue L
			}
		}
		service.Container = nil
	}
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

// obtainStandaloneContainers returns standalone containers. Standalone containers are containers which are either one-off containers, or whose service is not part of this docker-compose context
func (c *DockerCommand) obtainStandaloneContainers(containers []*Container, services []*Service) []*Container {
	standaloneContainers := []*Container{}
L:
	for _, container := range containers {
		for _, service := range services {
			if !container.OneOff && container.ServiceName != "" && container.ServiceName == service.Name {
				continue L
			}
		}
		standaloneContainers = append(standaloneContainers, container)
	}

	return standaloneContainers
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
			Name:          arr[0],
			ID:            arr[1],
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			DockerCommand: c,
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
