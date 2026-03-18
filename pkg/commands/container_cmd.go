package commands

import (
	"io"
	ogLog "log"
	"time"

	"github.com/jesseduffield/lazycontainer/pkg/config"
	"github.com/jesseduffield/lazycontainer/pkg/i18n"
	"github.com/jesseduffield/lazycontainer/pkg/utils"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
)

type ContainerCommand struct {
	Log             *logrus.Entry
	OSCommand       *OSCommand
	Tr              *i18n.TranslationSet
	Config          *config.AppConfig
	Client          *ContainerClient
	ErrorChan       chan error
	ContainerMutex  deadlock.Mutex
	ImageMutex      deadlock.Mutex
	VolumeMutex     deadlock.Mutex
	NetworkMutex    deadlock.Mutex

	Closers []io.Closer
}

var _ io.Closer = &ContainerCommand{}

type LimitedContainerCommand interface {
	NewCommandObject(CommandObject) CommandObject
}

func NewContainerCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*ContainerCommand, error) {
	client, err := NewContainerClient(log, osCommand, tr, config, errorChan)
	if err != nil {
		ogLog.Fatal(err)
	}

	containerCommand := &ContainerCommand{
		Log:        log,
		OSCommand:  osCommand,
		Tr:         tr,
		Config:     config,
		Client:     client,
		ErrorChan:  errorChan,
		Closers:    []io.Closer{client},
	}

	return containerCommand, nil
}

func (c *ContainerCommand) Close() error {
	return utils.CloseMany(c.Closers)
}

func (c *ContainerCommand) NewCommandObject(obj CommandObject) CommandObject {
	return obj
}

func (c *ContainerCommand) CreateClientStatMonitor(container *Container) {
	container.MonitoringStats = true

	var prevStats *ContainerStats
	lastTime := time.Now()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !container.MonitoringStats {
			return
		}

		now := time.Now()
		timeDelta := now.Sub(lastTime)

		stats, err := c.Client.StatsContainer(container.ID, true)
		if err != nil {
			c.Log.Error(err)
			continue
		}

		if len(stats) > 0 {
			containerStats := ConvertAppleStatsToContainerStats(stats[0], prevStats, timeDelta)

			recordedStats := &RecordedStats{
				ClientStats: *containerStats,
				DerivedStats: DerivedStats{
					CPUPercentage:    containerStats.CalculateContainerCPUPercentage(),
					MemoryPercentage: containerStats.CalculateContainerMemoryUsage(),
				},
				RecordedAt: now,
			}

			container.appendStats(recordedStats, c.Config.UserConfig.Stats.MaxDuration)

			prevStats = containerStats
			lastTime = now
		}
	}

	container.MonitoringStats = false
}

func (c *ContainerCommand) RefreshContainersAndServices(currentContainers []*Container) ([]*Container, error) {
	containers := c.RefreshContainers(currentContainers)
	return containers, nil
}

func (c *ContainerCommand) RefreshContainers(currentContainers []*Container) []*Container {
	return c.Client.RefreshContainers(currentContainers)
}

func (c *ContainerCommand) RefreshImages() ([]*Image, error) {
	return c.Client.RefreshImages()
}

func (c *ContainerCommand) RefreshVolumes() ([]*Volume, error) {
	return c.Client.RefreshVolumes()
}

func (c *ContainerCommand) RefreshNetworks() ([]*Network, error) {
	return c.Client.RefreshNetworks()
}

func (c *ContainerCommand) PruneContainers() error {
	return c.Client.PruneContainers()
}

func (c *ContainerCommand) PruneImages() error {
	return c.Client.PruneImages(false)
}

func (c *ContainerCommand) PruneVolumes() error {
	return c.Client.PruneVolumes()
}

func (c *ContainerCommand) PruneNetworks() error {
	return c.Client.PruneNetworks()
}

func (c *ContainerCommand) SystemDF() (*SystemDF, error) {
	return c.Client.SystemDF()
}

func (c *ContainerCommand) SystemStatus() (*SystemStatus, error) {
	return c.Client.SystemStatus()
}

func (c *ContainerCommand) SystemStart() error {
	return c.Client.SystemStart()
}

func (c *ContainerCommand) SystemStop() error {
	return c.Client.SystemStop()
}

func (c *ContainerCommand) BuildImage(contextDir string, dockerfile string, tag string) error {
	return c.Client.BuildImage(contextDir, dockerfile, tag)
}

func (c *ContainerCommand) GetProjectNames(containers []*Container) []string {
	return []string{}
}
