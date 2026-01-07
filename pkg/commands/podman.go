package commands

import (
	"context"
	"fmt"
	"io"
	ogLog "log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/imdario/mergo"
	"github.com/christophe-duc/lazypodman/pkg/commands/ssh"
	"github.com/christophe-duc/lazypodman/pkg/config"
	"github.com/christophe-duc/lazypodman/pkg/i18n"
	"github.com/christophe-duc/lazypodman/pkg/utils"
	"github.com/sasha-s/go-deadlock"
	"github.com/sirupsen/logrus"
)

const (
	containerHostEnvKey = "CONTAINER_HOST"
)

// PodmanCommand is our main podman interface
type PodmanCommand struct {
	Log                    *logrus.Entry
	OSCommand              *OSCommand
	Tr                     *i18n.TranslationSet
	Config                 *config.AppConfig
	Runtime                ContainerRuntime
	InComposeProject bool
	ErrorChan              chan error
	ContainerMutex         deadlock.Mutex
	ServiceMutex           deadlock.Mutex

	Closers []io.Closer
}

var _ io.Closer = &PodmanCommand{}

// LimitedPodmanCommand is a stripped-down PodmanCommand with just the methods the container/service/image might need
type LimitedPodmanCommand interface {
	NewCommandObject(CommandObject) CommandObject
}

// CommandObject is what we pass to our template resolvers when we are running a custom command.
// We do not guarantee that all fields will be populated: just the ones that make sense for the current context
type CommandObject struct {
	PodmanCompose string
	Service       *Service
	Container     *Container
	Image         *Image
	Volume        *Volume
	Network       *Network
}

// NewCommandObject takes a command object and returns a default command object with the passed command object merged in
func (c *PodmanCommand) NewCommandObject(obj CommandObject) CommandObject {
	defaultObj := CommandObject{PodmanCompose: c.Config.UserConfig.CommandTemplates.PodmanCompose}
	_ = mergo.Merge(&defaultObj, obj)
	return defaultObj
}

// NewPodmanCommand creates a new PodmanCommand with the appropriate runtime.
// It tries socket mode first, then falls back to libpod if available.
func NewPodmanCommand(log *logrus.Entry, osCommand *OSCommand, tr *i18n.TranslationSet, config *config.AppConfig, errorChan chan error) (*PodmanCommand, error) {
	var runtime ContainerRuntime
	var err error
	var closers []io.Closer

	// Determine the socket path
	socketPath := detectSocketPath()

	// Handle SSH tunneling for remote hosts
	if strings.HasPrefix(socketPath, "ssh://") {
		os.Setenv(containerHostEnvKey, socketPath)
		tunnelCloser, tunnelErr := ssh.NewSSHHandler(osCommand).HandleSSHDockerHost()
		if tunnelErr != nil {
			ogLog.Printf("> SSH tunnel setup failed: %v", tunnelErr)
		} else {
			closers = append(closers, tunnelCloser)
		}
		// Re-check for updated socket path after SSH setup
		if updatedPath := os.Getenv(containerHostEnvKey); updatedPath != "" {
			socketPath = updatedPath
		}
	}

	// Try socket mode first
	if socketPath != "" {
		runtime, err = NewSocketRuntime(socketPath)
		if err != nil {
			log.Warnf("Socket connection failed (%s): %v, trying libpod", socketPath, err)
		}
	}

	// Fall back to libpod if socket mode failed
	if runtime == nil {
		runtime, err = NewLibpodRuntime()
		if err != nil {
			return nil, fmt.Errorf("no Podman runtime available: socket error and libpod unavailable: %v", err)
		}
		log.Infof("Using libpod runtime (socket-less mode)")
	} else {
		log.Infof("Using socket runtime at %s", socketPath)
	}

	podmanCommand := &PodmanCommand{
		Log:                    log,
		OSCommand:              osCommand,
		Tr:                     tr,
		Config:           config,
		Runtime:          runtime,
		ErrorChan:        errorChan,
		InComposeProject: true,
		Closers:          closers,
	}

	podmanCommand.setComposeCommand(config)

	err = osCommand.RunCommand(
		utils.ApplyTemplate(
			config.UserConfig.CommandTemplates.CheckComposeConfig,
			podmanCommand.NewCommandObject(CommandObject{}),
		),
	)
	if err != nil {
		podmanCommand.InComposeProject = false
		log.Warn(err.Error())
	}

	return podmanCommand, nil
}

// setComposeCommand detects and sets the appropriate compose command
func (c *PodmanCommand) setComposeCommand(config *config.AppConfig) {
	// If user has explicitly set a compose command, respect it
	if config.UserConfig.CommandTemplates.PodmanCompose != "podman-compose" &&
		config.UserConfig.CommandTemplates.PodmanCompose != "" {
		return
	}

	// Try podman-compose first
	if err := c.OSCommand.RunCommand("podman-compose version"); err == nil {
		config.UserConfig.CommandTemplates.PodmanCompose = "podman-compose"
		return
	}

	// Try podman compose (built-in, if available)
	if err := c.OSCommand.RunCommand("podman compose version"); err == nil {
		config.UserConfig.CommandTemplates.PodmanCompose = "podman compose"
		return
	}

	// Fall back to docker-compose for compatibility
	if err := c.OSCommand.RunCommand("docker-compose version"); err == nil {
		config.UserConfig.CommandTemplates.PodmanCompose = "docker-compose"
		return
	}

	// Default to podman-compose
	config.UserConfig.CommandTemplates.PodmanCompose = "podman-compose"
}

func (c *PodmanCommand) Close() error {
	var errs []error
	if c.Runtime != nil {
		if err := c.Runtime.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := utils.CloseMany(c.Closers); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// CreateClientStatMonitor starts monitoring stats for a container
func (c *PodmanCommand) CreateClientStatMonitor(container *Container) {
	container.MonitoringStats = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	statsChan, errChan := c.Runtime.ContainerStats(ctx, container.ID, true)

	go func() {
		for err := range errChan {
			if err != nil {
				c.Log.Error(err)
			}
		}
	}()

	for stats := range statsChan {
		recordedStats := &RecordedStats{
			ClientStats: convertStatsEntryToContainerStats(stats),
			DerivedStats: DerivedStats{
				CPUPercentage:    calculateCPUPercentageFromEntry(stats),
				MemoryPercentage: calculateMemoryPercentageFromEntry(stats),
			},
			RecordedAt: time.Now(),
		}

		container.appendStats(recordedStats, c.Config.UserConfig.Stats.MaxDuration)
	}

	container.MonitoringStats = false
}

// convertStatsEntryToContainerStats converts the new stats format to the legacy format
func convertStatsEntryToContainerStats(entry ContainerStatsEntry) ContainerStats {
	return ContainerStats{
		Read:    entry.Read,
		Preread: entry.PreRead,
		PidsStats: struct {
			Current int `json:"current"`
		}{
			Current: entry.PidsStats.Current,
		},
		CPUStats: struct {
			CPUUsage struct {
				TotalUsage        int64   `json:"total_usage"`
				PercpuUsage       []int64 `json:"percpu_usage"`
				UsageInKernelmode int64   `json:"usage_in_kernelmode"`
				UsageInUsermode   int64   `json:"usage_in_usermode"`
			} `json:"cpu_usage"`
			SystemCPUUsage int64 `json:"system_cpu_usage"`
			OnlineCpus     int   `json:"online_cpus"`
			ThrottlingData struct {
				Periods          int `json:"periods"`
				ThrottledPeriods int `json:"throttled_periods"`
				ThrottledTime    int `json:"throttled_time"`
			} `json:"throttling_data"`
		}{
			CPUUsage: struct {
				TotalUsage        int64   `json:"total_usage"`
				PercpuUsage       []int64 `json:"percpu_usage"`
				UsageInKernelmode int64   `json:"usage_in_kernelmode"`
				UsageInUsermode   int64   `json:"usage_in_usermode"`
			}{
				TotalUsage:        entry.CPUStats.CPUUsage.TotalUsage,
				PercpuUsage:       entry.CPUStats.CPUUsage.PercpuUsage,
				UsageInKernelmode: entry.CPUStats.CPUUsage.UsageInKernelmode,
				UsageInUsermode:   entry.CPUStats.CPUUsage.UsageInUsermode,
			},
			SystemCPUUsage: entry.CPUStats.SystemCPUUsage,
			OnlineCpus:     entry.CPUStats.OnlineCpus,
		},
		PrecpuStats: struct {
			CPUUsage struct {
				TotalUsage        int64   `json:"total_usage"`
				PercpuUsage       []int64 `json:"percpu_usage"`
				UsageInKernelmode int64   `json:"usage_in_kernelmode"`
				UsageInUsermode   int64   `json:"usage_in_usermode"`
			} `json:"cpu_usage"`
			SystemCPUUsage int64 `json:"system_cpu_usage"`
			OnlineCpus     int   `json:"online_cpus"`
			ThrottlingData struct {
				Periods          int `json:"periods"`
				ThrottledPeriods int `json:"throttled_periods"`
				ThrottledTime    int `json:"throttled_time"`
			} `json:"throttling_data"`
		}{
			CPUUsage: struct {
				TotalUsage        int64   `json:"total_usage"`
				PercpuUsage       []int64 `json:"percpu_usage"`
				UsageInKernelmode int64   `json:"usage_in_kernelmode"`
				UsageInUsermode   int64   `json:"usage_in_usermode"`
			}{
				TotalUsage:        entry.PreCPUStats.CPUUsage.TotalUsage,
				PercpuUsage:       entry.PreCPUStats.CPUUsage.PercpuUsage,
				UsageInKernelmode: entry.PreCPUStats.CPUUsage.UsageInKernelmode,
				UsageInUsermode:   entry.PreCPUStats.CPUUsage.UsageInUsermode,
			},
			SystemCPUUsage: entry.PreCPUStats.SystemCPUUsage,
			OnlineCpus:     entry.PreCPUStats.OnlineCpus,
		},
		MemoryStats: struct {
			Usage    int `json:"usage"`
			MaxUsage int `json:"max_usage"`
			Stats    struct {
				ActiveAnon              int   `json:"active_anon"`
				ActiveFile              int   `json:"active_file"`
				Cache                   int   `json:"cache"`
				Dirty                   int   `json:"dirty"`
				HierarchicalMemoryLimit int64 `json:"hierarchical_memory_limit"`
				HierarchicalMemswLimit  int64 `json:"hierarchical_memsw_limit"`
				InactiveAnon            int   `json:"inactive_anon"`
				InactiveFile            int   `json:"inactive_file"`
				MappedFile              int   `json:"mapped_file"`
				Pgfault                 int   `json:"pgfault"`
				Pgmajfault              int   `json:"pgmajfault"`
				Pgpgin                  int   `json:"pgpgin"`
				Pgpgout                 int   `json:"pgpgout"`
				Rss                     int   `json:"rss"`
				RssHuge                 int   `json:"rss_huge"`
				TotalActiveAnon         int   `json:"total_active_anon"`
				TotalActiveFile         int   `json:"total_active_file"`
				TotalCache              int   `json:"total_cache"`
				TotalDirty              int   `json:"total_dirty"`
				TotalInactiveAnon       int   `json:"total_inactive_anon"`
				TotalInactiveFile       int   `json:"total_inactive_file"`
				TotalMappedFile         int   `json:"total_mapped_file"`
				TotalPgfault            int   `json:"total_pgfault"`
				TotalPgmajfault         int   `json:"total_pgmajfault"`
				TotalPgpgin             int   `json:"total_pgpgin"`
				TotalPgpgout            int   `json:"total_pgpgout"`
				TotalRss                int   `json:"total_rss"`
				TotalRssHuge            int   `json:"total_rss_huge"`
				TotalUnevictable        int   `json:"total_unevictable"`
				TotalWriteback          int   `json:"total_writeback"`
				Unevictable             int   `json:"unevictable"`
				Writeback               int   `json:"writeback"`
			} `json:"stats"`
			Limit int64 `json:"limit"`
		}{
			Usage:    int(entry.MemoryStats.Usage),
			MaxUsage: int(entry.MemoryStats.MaxUsage),
			Limit:    entry.MemoryStats.Limit,
		},
		Name: entry.Name,
		ID:   entry.ID,
	}
}

func calculateCPUPercentageFromEntry(stats ContainerStatsEntry) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemCPUUsage - stats.PreCPUStats.SystemCPUUsage)

	if systemDelta > 0 && cpuDelta > 0 {
		return (cpuDelta / systemDelta) * 100.0
	}
	return 0.0
}

func calculateMemoryPercentageFromEntry(stats ContainerStatsEntry) float64 {
	if stats.MemoryStats.Limit > 0 {
		return float64(stats.MemoryStats.Usage) / float64(stats.MemoryStats.Limit) * 100.0
	}
	return 0.0
}

func (c *PodmanCommand) RefreshContainersAndServices(currentServices []*Service, currentItems []*ContainerListItem) ([]*ContainerListItem, []*Service, error) {
	c.ServiceMutex.Lock()
	defer c.ServiceMutex.Unlock()

	// Extract existing containers from current items
	var currentContainers []*Container
	for _, item := range currentItems {
		if !item.IsPod && item.Container != nil {
			currentContainers = append(currentContainers, item.Container)
		}
	}

	containers, err := c.GetContainers(currentContainers)
	if err != nil {
		return nil, nil, err
	}

	// Get pods
	ctx := context.Background()
	podSummaries, err := c.Runtime.ListPods(ctx)
	if err != nil {
		// Don't fail if pods can't be listed, just log and continue without pods
		c.Log.Warnf("Failed to list pods: %v", err)
		podSummaries = nil
	}

	// Build the unified list
	items := c.buildContainerListItems(containers, podSummaries)

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

	// Extract just containers for service assignment
	var containersForServices []*Container
	for _, item := range items {
		if !item.IsPod && item.Container != nil {
			containersForServices = append(containersForServices, item.Container)
		}
	}
	c.assignContainersToServices(containersForServices, services)

	return items, services, nil
}

// buildContainerListItems creates a unified list of pods and containers
func (c *PodmanCommand) buildContainerListItems(containers []*Container, podSummaries []PodSummary) []*ContainerListItem {
	var items []*ContainerListItem

	// Create a map of pod ID -> pod summary
	podMap := make(map[string]PodSummary)
	for _, ps := range podSummaries {
		podMap[ps.ID] = ps
	}

	// Create a map of pod ID -> containers in that pod
	podContainers := make(map[string][]*Container)
	var standaloneContainers []*Container

	for _, ctr := range containers {
		// Skip infra containers
		if ctr.Summary.IsInfra {
			continue
		}

		if ctr.Summary.Pod != "" {
			podContainers[ctr.Summary.Pod] = append(podContainers[ctr.Summary.Pod], ctr)
		} else {
			standaloneContainers = append(standaloneContainers, ctr)
		}
	}

	// Add pods and their containers
	for podID, ps := range podMap {
		// Create pod object
		pod := &Pod{
			ID:         ps.ID,
			Name:       ps.Name,
			Summary:    ps,
			Containers: podContainers[podID],
			OSCommand:  c.OSCommand,
			Log:        c.Log,
		}

		// Add pod item
		items = append(items, &ContainerListItem{
			IsPod:  true,
			Pod:    pod,
			Indent: 0,
		})

		// Add containers in this pod with indent
		for _, ctr := range podContainers[podID] {
			// Set pod name on container if not already set
			if ctr.Summary.PodName == "" {
				ctr.Summary.PodName = ps.Name
			}
			items = append(items, &ContainerListItem{
				IsPod:     false,
				Container: ctr,
				Indent:    2,
			})
		}
	}

	// Add standalone containers
	for _, ctr := range standaloneContainers {
		items = append(items, &ContainerListItem{
			IsPod:     false,
			Container: ctr,
			Indent:    0,
		})
	}

	return items
}

func (c *PodmanCommand) assignContainersToServices(containers []*Container, services []*Service) {
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

// GetContainers gets the podman containers
func (c *PodmanCommand) GetContainers(existingContainers []*Container) ([]*Container, error) {
	c.ContainerMutex.Lock()
	defer c.ContainerMutex.Unlock()

	ctx := context.Background()
	containers, err := c.Runtime.ListContainers(ctx)
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
				Runtime:       c.Runtime,
				OSCommand:     c.OSCommand,
				Log:           c.Log,
				PodmanCommand: c,
				Tr:            c.Tr,
			}
		}

		newContainer.Summary = ctr
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
		// Compose labels work the same way with podman-compose
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
func (c *PodmanCommand) GetServices() ([]*Service, error) {
	if !c.InComposeProject {
		return nil, nil
	}

	composeCommand := c.Config.UserConfig.CommandTemplates.PodmanCompose
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
			PodmanCommand: c,
		}
	}

	return services, nil
}

func (c *PodmanCommand) RefreshContainerDetails(containers []*Container) error {
	c.ContainerMutex.Lock()
	defer c.ContainerMutex.Unlock()

	c.SetContainerDetails(containers)

	return nil
}

// SetContainerDetails attaches the details returned from container inspect to each of the containers
func (c *PodmanCommand) SetContainerDetails(containers []*Container) {
	ctx := context.Background()
	wg := sync.WaitGroup{}
	for _, ctr := range containers {
		ctr := ctr
		wg.Add(1)
		go func() {
			defer wg.Done()
			details, err := c.Runtime.InspectContainer(ctx, ctr.ID)
			if err != nil {
				c.Log.Error(err)
			} else {
				ctr.Details = details
			}
		}()
	}
	wg.Wait()
}

// ViewAllLogs attaches to a subprocess viewing all the logs from compose
func (c *PodmanCommand) ViewAllLogs() (*exec.Cmd, error) {
	cmd := c.OSCommand.ExecutableFromString(
		utils.ApplyTemplate(
			c.OSCommand.Config.UserConfig.CommandTemplates.ViewAllLogs,
			c.NewCommandObject(CommandObject{}),
		),
	)

	c.OSCommand.PrepareForChildren(cmd)

	return cmd, nil
}

// ComposeConfig returns the result of 'compose config'
func (c *PodmanCommand) ComposeConfig() string {
	output, err := c.OSCommand.RunCommandWithOutput(
		utils.ApplyTemplate(
			c.OSCommand.Config.UserConfig.CommandTemplates.ComposeConfig,
			c.NewCommandObject(CommandObject{}),
		),
	)
	if err != nil {
		output = err.Error()
	}
	return output
}

// PruneContainers prunes containers using the runtime
func (c *PodmanCommand) PruneContainers() error {
	return c.Runtime.PruneContainers(context.Background())
}

// PruneImages prunes images using the runtime
func (c *PodmanCommand) PruneImages() error {
	return c.Runtime.PruneImages(context.Background())
}

// PruneVolumes prunes volumes using the runtime
func (c *PodmanCommand) PruneVolumes() error {
	return c.Runtime.PruneVolumes(context.Background())
}

// PruneNetworks prunes networks using the runtime
func (c *PodmanCommand) PruneNetworks() error {
	return c.Runtime.PruneNetworks(context.Background())
}

// RefreshImages returns a slice of podman images
func (c *PodmanCommand) RefreshImages() ([]*Image, error) {
	ctx := context.Background()
	images, err := c.Runtime.ListImages(ctx)
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
			Summary:       img,
			Runtime:       c.Runtime,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			PodmanCommand: c,
		}
	}

	return ownImages, nil
}

// RefreshVolumes gets the volumes and stores them
func (c *PodmanCommand) RefreshVolumes() ([]*Volume, error) {
	ctx := context.Background()
	volumes, err := c.Runtime.ListVolumes(ctx)
	if err != nil {
		return nil, err
	}

	ownVolumes := make([]*Volume, len(volumes))

	for i, vol := range volumes {
		ownVolumes[i] = &Volume{
			Name:          vol.Name,
			Summary:       vol,
			Runtime:       c.Runtime,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			PodmanCommand: c,
		}
	}

	return ownVolumes, nil
}

// RefreshNetworks gets the networks and stores them
func (c *PodmanCommand) RefreshNetworks() ([]*Network, error) {
	ctx := context.Background()
	networks, err := c.Runtime.ListNetworks(ctx)
	if err != nil {
		return nil, err
	}

	ownNetworks := make([]*Network, len(networks))

	for i, nw := range networks {
		ownNetworks[i] = &Network{
			Name:          nw.Name,
			Summary:       nw,
			Runtime:       c.Runtime,
			OSCommand:     c.OSCommand,
			Log:           c.Log,
			PodmanCommand: c,
		}
	}

	return ownNetworks, nil
}
