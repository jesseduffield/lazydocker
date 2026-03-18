package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/jesseduffield/lazycontainer/pkg/config"
	"github.com/jesseduffield/lazycontainer/pkg/i18n"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
)

type ContainerClient struct {
	Log        *logrus.Entry
	OSCommand  *OSCommand
	Tr         *i18n.TranslationSet
	Config     *config.AppConfig
	ErrorChan  chan error

	ContainerMutex deadlock.Mutex
	ImageMutex     deadlock.Mutex
	VolumeMutex    deadlock.Mutex
	NetworkMutex   deadlock.Mutex
}

func NewContainerClient(log *logrus.Entry, osCommand *OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*ContainerClient, error) {
	return &ContainerClient{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    config,
		ErrorChan: errorChan,
	}, nil
}

func (c *ContainerClient) runContainerCommand(args ...string) ([]byte, error) {
	cmd := exec.Command("container", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s: %s", err.Error(), string(exitErr.Stderr))
		}
		return nil, err
	}
	return output, nil
}

func (c *ContainerClient) runContainerCommandWithOutput(args ...string) (string, error) {
	output, err := c.runContainerCommand(args...)
	return string(output), err
}

// Containers

func (c *ContainerClient) ListContainers(all bool) ([]AppleContainer, error) {
	args := []string{"list", "--format", "json"}
	if all {
		args = []string{"list", "--all", "--format", "json"}
	}

	output, err := c.runContainerCommand(args...)
	if err != nil {
		return nil, err
	}

	if len(output) == 0 {
		return []AppleContainer{}, nil
	}

	var containers []AppleContainer
	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse container list: %w", err)
	}

	return containers, nil
}

func (c *ContainerClient) InspectContainer(id string) (*AppleContainer, error) {
	output, err := c.runContainerCommand("inspect", id)
	if err != nil {
		return nil, err
	}

	var containers []AppleContainer
	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse container inspect: %w", err)
	}

	if len(containers) == 0 {
		return nil, fmt.Errorf("container %s not found", id)
	}

	return &containers[0], nil
}

func (c *ContainerClient) CreateContainer(opts CreateContainerOptions) error {
	args := []string{"create"}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	for _, env := range opts.Env {
		args = append(args, "-e", env)
	}

	if opts.WorkingDir != "" {
		args = append(args, "-w", opts.WorkingDir)
	}

	if opts.CPUS > 0 {
		args = append(args, "-c", fmt.Sprintf("%d", opts.CPUS))
	}

	if opts.Memory != "" {
		args = append(args, "-m", opts.Memory)
	}

	for _, vol := range opts.Volumes {
		args = append(args, "-v", vol)
	}

	for _, net := range opts.Networks {
		args = append(args, "--network", net)
	}

	for _, port := range opts.Ports {
		args = append(args, "-p", port)
	}

	for k, v := range opts.Labels {
		args = append(args, "-l", fmt.Sprintf("%s=%s", k, v))
	}

	if opts.Interactive {
		args = append(args, "-i")
	}

	if opts.TTY {
		args = append(args, "-t")
	}

	args = append(args, opts.Image)

	if len(opts.Command) > 0 {
		args = append(args, opts.Command...)
	}

	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) RunContainer(opts CreateContainerOptions) (string, error) {
	args := []string{"run"}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	if opts.Detach {
		args = append(args, "-d")
	}

	for _, env := range opts.Env {
		args = append(args, "-e", env)
	}

	if opts.WorkingDir != "" {
		args = append(args, "-w", opts.WorkingDir)
	}

	if opts.CPUS > 0 {
		args = append(args, "-c", fmt.Sprintf("%d", opts.CPUS))
	}

	if opts.Memory != "" {
		args = append(args, "-m", opts.Memory)
	}

	for _, vol := range opts.Volumes {
		args = append(args, "-v", vol)
	}

	for _, net := range opts.Networks {
		args = append(args, "--network", net)
	}

	for _, port := range opts.Ports {
		args = append(args, "-p", port)
	}

	for k, v := range opts.Labels {
		args = append(args, "-l", fmt.Sprintf("%s=%s", k, v))
	}

	if opts.Interactive {
		args = append(args, "-i")
	}

	if opts.TTY {
		args = append(args, "-t")
	}

	args = append(args, opts.Image)

	if len(opts.Command) > 0 {
		args = append(args, opts.Command...)
	}

	output, err := c.runContainerCommand(args...)
	return string(output), err
}

func (c *ContainerClient) StartContainer(id string) error {
	_, err := c.runContainerCommand("start", id)
	return err
}

func (c *ContainerClient) StopContainer(id string) error {
	_, err := c.runContainerCommand("stop", id)
	return err
}

func (c *ContainerClient) RestartContainer(id string) error {
	_, err := c.runContainerCommand("stop", id)
	if err != nil {
		return err
	}
	return c.StartContainer(id)
}

func (c *ContainerClient) KillContainer(id string, signal string) error {
	args := []string{"kill", id}
	if signal != "" {
		args = []string{"kill", "--signal", signal, id}
	}
	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) RemoveContainer(id string, force bool) error {
	args := []string{"rm", id}
	if force {
		args = []string{"rm", "--force", id}
	}
	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) PruneContainers() error {
	_, err := c.runContainerCommand("prune")
	return err
}

func (c *ContainerClient) ExecContainer(id string, opts ExecOptions) *exec.Cmd {
	args := []string{"exec"}

	for _, env := range opts.Env {
		args = append(args, "-e", env)
	}

	if opts.WorkingDir != "" {
		args = append(args, "-w", opts.WorkingDir)
	}

	if opts.Interactive {
		args = append(args, "-i")
	}

	if opts.TTY {
		args = append(args, "-t")
	}

	if opts.User != "" {
		args = append(args, "-u", opts.User)
	}

	args = append(args, id)
	args = append(args, opts.Command...)

	return exec.Command("container", args...)
}

func (c *ContainerClient) LogsContainer(id string, follow bool, tail int) *exec.Cmd {
	args := []string{"logs"}

	if follow {
		args = append(args, "--follow")
	}

	if tail > 0 {
		args = append(args, "-n", fmt.Sprintf("%d", tail))
	}

	args = append(args, id)

	return exec.Command("container", args...)
}

func (c *ContainerClient) StatsContainer(id string, noStream bool) ([]AppleContainerStats, error) {
	args := []string{"stats", "--format", "json"}

	if noStream {
		args = append(args, "--no-stream")
	}

	if id != "" {
		args = append(args, id)
	}

	output, err := c.runContainerCommand(args...)
	if err != nil {
		return nil, err
	}

	if len(output) == 0 {
		return []AppleContainerStats{}, nil
	}

	var stats []AppleContainerStats
	if err := json.Unmarshal(output, &stats); err != nil {
		return nil, fmt.Errorf("failed to parse container stats: %w", err)
	}

	return stats, nil
}

// Images

func (c *ContainerClient) ListImages() ([]AppleImage, error) {
	c.ImageMutex.Lock()
	defer c.ImageMutex.Unlock()

	output, err := c.runContainerCommand("image", "list", "--format", "json")
	if err != nil {
		return nil, err
	}

	if len(output) == 0 {
		return []AppleImage{}, nil
	}

	var images []AppleImage
	if err := json.Unmarshal(output, &images); err != nil {
		return nil, fmt.Errorf("failed to parse image list: %w", err)
	}

	return images, nil
}

func (c *ContainerClient) PullImage(ref string) error {
	_, err := c.runContainerCommand("image", "pull", ref)
	return err
}

func (c *ContainerClient) RemoveImage(id string, force bool) error {
	args := []string{"image", "rm", id}
	if force {
		args = []string{"image", "rm", "--force", id}
	}
	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) PruneImages(all bool) error {
	args := []string{"image", "prune"}
	if all {
		args = []string{"image", "prune", "-a"}
	}
	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) BuildImage(contextDir string, dockerfile string, tag string) error {
	args := []string{"build"}

	if dockerfile != "" {
		args = append(args, "-f", dockerfile)
	}

	if tag != "" {
		args = append(args, "-t", tag)
	}

	if contextDir != "" {
		args = append(args, contextDir)
	}

	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) SaveImage(ids []string, output string) error {
	args := []string{"image", "save"}
	args = append(args, ids...)
	if output != "" {
		args = append(args, "--output", output)
	}
	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) LoadImage(input string) error {
	args := []string{"image", "load"}
	if input != "" {
		args = append(args, "--input", input)
	}
	_, err := c.runContainerCommand(args...)
	return err
}

// Volumes

func (c *ContainerClient) ListVolumes() ([]AppleVolume, error) {
	c.VolumeMutex.Lock()
	defer c.VolumeMutex.Unlock()

	output, err := c.runContainerCommand("volume", "list", "--format", "json")
	if err != nil {
		return nil, err
	}

	if len(output) == 0 {
		return []AppleVolume{}, nil
	}

	var volumes []AppleVolume
	if err := json.Unmarshal(output, &volumes); err != nil {
		return nil, fmt.Errorf("failed to parse volume list: %w", err)
	}

	return volumes, nil
}

func (c *ContainerClient) CreateVolume(name string) error {
	args := []string{"volume", "create"}
	if name != "" {
		args = append(args, name)
	}
	_, err := c.runContainerCommand(args...)
	return err
}

func (c *ContainerClient) RemoveVolume(name string) error {
	_, err := c.runContainerCommand("volume", "rm", name)
	return err
}

func (c *ContainerClient) PruneVolumes() error {
	_, err := c.runContainerCommand("volume", "prune")
	return err
}

// Networks

func (c *ContainerClient) ListNetworks() ([]AppleNetwork, error) {
	c.NetworkMutex.Lock()
	defer c.NetworkMutex.Unlock()

	output, err := c.runContainerCommand("network", "list", "--format", "json")
	if err != nil {
		return nil, err
	}

	if len(output) == 0 {
		return []AppleNetwork{}, nil
	}

	var networks []AppleNetwork
	if err := json.Unmarshal(output, &networks); err != nil {
		return nil, fmt.Errorf("failed to parse network list: %w", err)
	}

	return networks, nil
}

func (c *ContainerClient) CreateNetwork(name string) error {
	_, err := c.runContainerCommand("network", "create", name)
	return err
}

func (c *ContainerClient) RemoveNetwork(name string) error {
	_, err := c.runContainerCommand("network", "rm", name)
	return err
}

func (c *ContainerClient) PruneNetworks() error {
	_, err := c.runContainerCommand("network", "prune")
	return err
}

// System

func (c *ContainerClient) SystemDF() (*SystemDF, error) {
	output, err := c.runContainerCommand("system", "df", "--format", "json")
	if err != nil {
		return nil, err
	}

	var df SystemDF
	if err := json.Unmarshal(output, &df); err != nil {
		return nil, fmt.Errorf("failed to parse system df: %w", err)
	}

	return &df, nil
}

func (c *ContainerClient) SystemStatus() (*SystemStatus, error) {
	output, err := c.runContainerCommand("system", "status")
	if err != nil {
		return &SystemStatus{Running: false}, nil
	}

	status := &SystemStatus{
		Running: strings.Contains(string(output), "running"),
	}

	return status, nil
}

func (c *ContainerClient) SystemStart() error {
	_, err := c.runContainerCommand("system", "start")
	return err
}

func (c *ContainerClient) SystemStop() error {
	_, err := c.runContainerCommand("system", "stop")
	return err
}

func (c *ContainerClient) SystemVersion() (string, error) {
	return c.runContainerCommandWithOutput("--version")
}

// Monitor stats for a container
func (c *ContainerClient) CreateStatsMonitor(container *Container, callback func(stats AppleContainerStats)) {
	container.MonitoringStats = true

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !container.MonitoringStats {
			return
		}

		stats, err := c.StatsContainer(container.ID, true)
		if err != nil {
			c.Log.Error(err)
			continue
		}

		if len(stats) > 0 {
			callback(stats[0])
		}
	}
}

var _ io.Closer = &ContainerClient{}

func (c *ContainerClient) Close() error {
	return nil
}

// CommandObject is what we pass to our template resolvers
type CommandObject struct {
	Container *Container
	Image     *Image
	Volume    *Volume
	Network   *Network
}

func (c *ContainerClient) NewCommandObject(obj CommandObject) CommandObject {
	return obj
}

func (c *ContainerClient) RefreshContainersAndServices(currentContainers []*Container) ([]*Container, error) {
	return c.RefreshContainers(currentContainers), nil
}

func (c *ContainerClient) RefreshContainers(currentContainers []*Container) []*Container {
	c.ContainerMutex.Lock()
	defer c.ContainerMutex.Unlock()

	appleContainers, err := c.ListContainers(true)
	if err != nil {
		c.Log.Error(err)
		return currentContainers
	}

	containers := make([]*Container, len(appleContainers))

	for i, ac := range appleContainers {
		var existingContainer *Container
		for _, cc := range currentContainers {
			if cc.ID == ac.Configuration.ID {
				existingContainer = cc
				break
			}
		}

		if existingContainer != nil {
			existingContainer.AppleContainer = ac
			existingContainer.Name = ac.Configuration.ID
			if ac.Configuration.ID != "" {
				existingContainer.Name = ac.Configuration.ID
			}
			containers[i] = existingContainer
		} else {
			container := &Container{
				ID:             ac.Configuration.ID,
				Name:           ac.Configuration.ID,
				AppleContainer: ac,
				Client:         c,
				OSCommand:      c.OSCommand,
				Log:            c.Log,
				Tr:             c.Tr,
			}
			containers[i] = container
		}
	}

	return containers
}

func (c *ContainerClient) GetProjectNames(containers []*Container) []string {
	return []string{}
}
