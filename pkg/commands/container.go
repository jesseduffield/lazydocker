package commands

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/lazydocker/pkg/config"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
)

// Container : A docker Container
type Container struct {
	Name            string
	ServiceName     string
	ContainerNumber string // might make this an int in the future if need be

	// OneOff tells us if the container is just a job container or is actually bound to the service
	OneOff          bool
	ProjectName     string
	ID              string
	Container       types.Container
	DisplayString   string
	Client          *client.Client
	OSCommand       *OSCommand
	Config          *config.AppConfig
	Log             *logrus.Entry
	CLIStats        ContainerCliStat // for realtime we use the CLI, for long-term we use the client
	StatHistory     []RecordedStats
	Details         Details
	MonitoringStats bool
	DockerCommand   LimitedDockerCommand
	Tr              *i18n.TranslationSet
}

// Details is a struct containing what we get back from `docker inspect` on a container
type Details struct {
	ID      string    `json:"Id"`
	Created time.Time `json:"Created"`
	Path    string    `json:"Path"`
	Args    []string  `json:"Args"`
	State   struct {
		Status     string       `json:"Status"`
		Running    bool         `json:"Running"`
		Paused     bool         `json:"Paused"`
		Restarting bool         `json:"Restarting"`
		OOMKilled  bool         `json:"OOMKilled"`
		Dead       bool         `json:"Dead"`
		Pid        int          `json:"Pid"`
		ExitCode   int          `json:"ExitCode"`
		Error      string       `json:"Error"`
		StartedAt  time.Time    `json:"StartedAt"`
		FinishedAt time.Time    `json:"FinishedAt"`
		Health     types.Health `json:"Health"`
	} `json:"State"`
	Image           string      `json:"Image"`
	ResolvConfPath  string      `json:"ResolvConfPath"`
	HostnamePath    string      `json:"HostnamePath"`
	HostsPath       string      `json:"HostsPath"`
	LogPath         string      `json:"LogPath"`
	Name            string      `json:"Name"`
	RestartCount    int         `json:"RestartCount"`
	Driver          string      `json:"Driver"`
	Platform        string      `json:"Platform"`
	MountLabel      string      `json:"MountLabel"`
	ProcessLabel    string      `json:"ProcessLabel"`
	AppArmorProfile string      `json:"AppArmorProfile"`
	ExecIDs         interface{} `json:"ExecIDs"`
	HostConfig      struct {
		Binds           []string `json:"Binds"`
		ContainerIDFile string   `json:"ContainerIDFile"`
		LogConfig       struct {
			Type   string `json:"Type"`
			Config struct {
			} `json:"Config"`
		} `json:"LogConfig"`
		NetworkMode  string `json:"NetworkMode"`
		PortBindings struct {
		} `json:"PortBindings"`
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
		AutoRemove           bool          `json:"AutoRemove"`
		VolumeDriver         string        `json:"VolumeDriver"`
		VolumesFrom          []interface{} `json:"VolumesFrom"`
		CapAdd               interface{}   `json:"CapAdd"`
		CapDrop              interface{}   `json:"CapDrop"`
		DNS                  interface{}   `json:"Dns"`
		DNSOptions           interface{}   `json:"DnsOptions"`
		DNSSearch            interface{}   `json:"DnsSearch"`
		ExtraHosts           interface{}   `json:"ExtraHosts"`
		GroupAdd             interface{}   `json:"GroupAdd"`
		IpcMode              string        `json:"IpcMode"`
		Cgroup               string        `json:"Cgroup"`
		Links                interface{}   `json:"Links"`
		OomScoreAdj          int           `json:"OomScoreAdj"`
		PidMode              string        `json:"PidMode"`
		Privileged           bool          `json:"Privileged"`
		PublishAllPorts      bool          `json:"PublishAllPorts"`
		ReadonlyRootfs       bool          `json:"ReadonlyRootfs"`
		SecurityOpt          interface{}   `json:"SecurityOpt"`
		UTSMode              string        `json:"UTSMode"`
		UsernsMode           string        `json:"UsernsMode"`
		ShmSize              int           `json:"ShmSize"`
		Runtime              string        `json:"Runtime"`
		ConsoleSize          []int         `json:"ConsoleSize"`
		Isolation            string        `json:"Isolation"`
		CPUShares            int           `json:"CpuShares"`
		Memory               int           `json:"Memory"`
		NanoCpus             int           `json:"NanoCpus"`
		CgroupParent         string        `json:"CgroupParent"`
		BlkioWeight          int           `json:"BlkioWeight"`
		BlkioWeightDevice    interface{}   `json:"BlkioWeightDevice"`
		BlkioDeviceReadBps   interface{}   `json:"BlkioDeviceReadBps"`
		BlkioDeviceWriteBps  interface{}   `json:"BlkioDeviceWriteBps"`
		BlkioDeviceReadIOps  interface{}   `json:"BlkioDeviceReadIOps"`
		BlkioDeviceWriteIOps interface{}   `json:"BlkioDeviceWriteIOps"`
		CPUPeriod            int           `json:"CpuPeriod"`
		CPUQuota             int           `json:"CpuQuota"`
		CPURealtimePeriod    int           `json:"CpuRealtimePeriod"`
		CPURealtimeRuntime   int           `json:"CpuRealtimeRuntime"`
		CpusetCpus           string        `json:"CpusetCpus"`
		CpusetMems           string        `json:"CpusetMems"`
		Devices              interface{}   `json:"Devices"`
		DeviceCgroupRules    interface{}   `json:"DeviceCgroupRules"`
		DiskQuota            int           `json:"DiskQuota"`
		KernelMemory         int           `json:"KernelMemory"`
		MemoryReservation    int           `json:"MemoryReservation"`
		MemorySwap           int           `json:"MemorySwap"`
		MemorySwappiness     interface{}   `json:"MemorySwappiness"`
		OomKillDisable       bool          `json:"OomKillDisable"`
		PidsLimit            int           `json:"PidsLimit"`
		Ulimits              interface{}   `json:"Ulimits"`
		CPUCount             int           `json:"CpuCount"`
		CPUPercent           int           `json:"CpuPercent"`
		IOMaximumIOps        int           `json:"IOMaximumIOps"`
		IOMaximumBandwidth   int           `json:"IOMaximumBandwidth"`
		MaskedPaths          []string      `json:"MaskedPaths"`
		ReadonlyPaths        []string      `json:"ReadonlyPaths"`
	} `json:"HostConfig"`
	GraphDriver struct {
		Data struct {
			LowerDir  string `json:"LowerDir"`
			MergedDir string `json:"MergedDir"`
			UpperDir  string `json:"UpperDir"`
			WorkDir   string `json:"WorkDir"`
		} `json:"Data"`
		Name string `json:"Name"`
	} `json:"GraphDriver"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name,omitempty"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Driver      string `json:"Driver,omitempty"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
		Propagation string `json:"Propagation"`
	} `json:"Mounts"`
	Config struct {
		Hostname     string   `json:"Hostname"`
		Domainname   string   `json:"Domainname"`
		User         string   `json:"User"`
		AttachStdin  bool     `json:"AttachStdin"`
		AttachStdout bool     `json:"AttachStdout"`
		AttachStderr bool     `json:"AttachStderr"`
		Tty          bool     `json:"Tty"`
		OpenStdin    bool     `json:"OpenStdin"`
		StdinOnce    bool     `json:"StdinOnce"`
		Env          []string `json:"Env"`
		Cmd          []string `json:"Cmd"`
		Image        string   `json:"Image"`
		Volumes      struct {
			APIBundle struct {
			} `json:"/api-bundle"`
			App struct {
			} `json:"/app"`
		} `json:"Volumes"`
		WorkingDir string            `json:"WorkingDir"`
		Entrypoint interface{}       `json:"Entrypoint"`
		OnBuild    interface{}       `json:"OnBuild"`
		Labels     map[string]string `json:"Labels"`
	} `json:"Config"`
	NetworkSettings struct {
		Bridge                 string `json:"Bridge"`
		SandboxID              string `json:"SandboxID"`
		HairpinMode            bool   `json:"HairpinMode"`
		LinkLocalIPv6Address   string `json:"LinkLocalIPv6Address"`
		LinkLocalIPv6PrefixLen int    `json:"LinkLocalIPv6PrefixLen"`
		Ports                  map[string][]struct {
			HostIP   string `json:"HostIP"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
		SandboxKey             string      `json:"SandboxKey"`
		SecondaryIPAddresses   interface{} `json:"SecondaryIPAddresses"`
		SecondaryIPv6Addresses interface{} `json:"SecondaryIPv6Addresses"`
		EndpointID             string      `json:"EndpointID"`
		Gateway                string      `json:"Gateway"`
		GlobalIPv6Address      string      `json:"GlobalIPv6Address"`
		GlobalIPv6PrefixLen    int         `json:"GlobalIPv6PrefixLen"`
		IPAddress              string      `json:"IPAddress"`
		IPPrefixLen            int         `json:"IPPrefixLen"`
		IPv6Gateway            string      `json:"IPv6Gateway"`
		MacAddress             string      `json:"MacAddress"`
		Networks               map[string]struct {
			IPAMConfig          interface{} `json:"IPAMConfig"`
			Links               interface{} `json:"Links"`
			Aliases             []string    `json:"Aliases"`
			NetworkID           string      `json:"NetworkID"`
			EndpointID          string      `json:"EndpointID"`
			Gateway             string      `json:"Gateway"`
			IPAddress           string      `json:"IPAddress"`
			IPPrefixLen         int         `json:"IPPrefixLen"`
			IPv6Gateway         string      `json:"IPv6Gateway"`
			GlobalIPv6Address   string      `json:"GlobalIPv6Address"`
			GlobalIPv6PrefixLen int         `json:"GlobalIPv6PrefixLen"`
			MacAddress          string      `json:"MacAddress"`
			DriverOpts          interface{} `json:"DriverOpts"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// ContainerCliStat is a stat object returned by the CLI docker stat command
type ContainerCliStat struct {
	BlockIO   string `json:"BlockIO"`
	CPUPerc   string `json:"CPUPerc"`
	Container string `json:"Container"`
	ID        string `json:"ID"`
	MemPerc   string `json:"MemPerc"`
	MemUsage  string `json:"MemUsage"`
	Name      string `json:"Name"`
	NetIO     string `json:"NetIO"`
	PIDs      string `json:"PIDs"`
}

// GetDisplayStrings returns the dispaly string of Container
func (c *Container) GetDisplayStrings(isFocused bool) []string {
	image := strings.TrimPrefix(c.Container.Image, "sha256:")

	return []string{c.GetDisplayStatus(), c.GetDisplaySubstatus(), c.Name, c.GetDisplayCPUPerc(), utils.ColoredString(image, color.FgMagenta)}
}

// GetDisplayStatus returns the colored status of the container
func (c *Container) GetDisplayStatus() string {
	return utils.ColoredString(c.Container.State, c.GetColor())
}

// GetDisplayStatus returns the exit code if the container has exited, and the health status if the container is running (and has a health check)
func (c *Container) GetDisplaySubstatus() string {
	switch c.Container.State {
	case "exited":
		return utils.ColoredString(
			fmt.Sprintf("(%s)", strconv.Itoa(c.Details.State.ExitCode)), c.GetColor(),
		)
	case "running":
		return c.getHealthStatus()
	default:
		return ""
	}
}

func (c *Container) getHealthStatus() string {
	healthStatusColorMap := map[string]color.Attribute{
		"healthy":   color.FgGreen,
		"unhealthy": color.FgRed,
		"starting":  color.FgYellow,
	}

	healthStatus := c.Details.State.Health.Status
	if healthStatusColor, ok := healthStatusColorMap[healthStatus]; ok {
		return utils.ColoredString(fmt.Sprintf("(%s)", healthStatus), healthStatusColor)
	}
	return ""
}

// GetDisplayCPUPerc colors the cpu percentage based on how extreme it is
func (c *Container) GetDisplayCPUPerc() string {
	stats := c.CLIStats

	if stats.CPUPerc == "" {
		return ""
	}

	percentage, err := strconv.ParseFloat(strings.TrimSuffix(stats.CPUPerc, "%"), 32)
	if err != nil {
		// probably complaining about not being able to convert '--'
		return ""
	}

	var clr color.Attribute
	if percentage > 90 {
		clr = color.FgRed
	} else if percentage > 50 {
		clr = color.FgYellow
	} else {
		clr = color.FgWhite
	}

	return utils.ColoredString(stats.CPUPerc, clr)
}

// ProducingLogs tells us whether we should bother checking a container's logs
func (c *Container) ProducingLogs() bool {
	return c.Container.State == "running" && !(c.Details.HostConfig.LogConfig.Type == "none")
}

// GetColor Container color
func (c *Container) GetColor() color.Attribute {
	switch c.Container.State {
	case "exited":
		if c.Details.State.ExitCode == 0 {
			return color.FgYellow
		}
		return color.FgRed
	case "created":
		return color.FgCyan
	case "running":
		return color.FgGreen
	case "paused":
		return color.FgYellow
	case "dead":
		return color.FgRed
	case "restarting":
		return color.FgBlue
	case "removing":
		return color.FgMagenta
	default:
		return color.FgWhite
	}
}

// Remove removes the container
func (c *Container) Remove(options types.ContainerRemoveOptions) error {
	c.Log.Warn(fmt.Sprintf("removing container %s", c.Name))
	if err := c.Client.ContainerRemove(context.Background(), c.ID, options); err != nil {
		if strings.Contains(err.Error(), "Stop the container before attempting removal or force remove") {
			return ComplexError{
				Code:    MustStopContainer,
				Message: err.Error(),
				frame:   xerrors.Caller(1),
			}
		}
		return err
	}

	return nil
}

// Stop stops the container
func (c *Container) Stop() error {
	c.Log.Warn(fmt.Sprintf("stopping container %s", c.Name))
	return c.Client.ContainerStop(context.Background(), c.ID, nil)
}

// Restart restarts the container
func (c *Container) Restart() error {
	c.Log.Warn(fmt.Sprintf("restarting container %s", c.Name))
	return c.Client.ContainerRestart(context.Background(), c.ID, nil)
}

// Attach attaches the container
func (c *Container) Attach() (*exec.Cmd, error) {
	c.Log.Warn(fmt.Sprintf("attaching to container %s", c.Name))

	// verify that we can in fact attach to this container
	if !c.Details.Config.OpenStdin {
		return nil, errors.New(c.Tr.UnattachableContainerError)
	}

	if c.Container.State == "exited" {
		return nil, errors.New(c.Tr.CannotAttachStoppedContainerError)
	}

	cmd := c.OSCommand.PrepareSubProcess("docker", "attach", "--sig-proxy=false", c.ID)
	return cmd, nil
}

// Top returns process information
func (c *Container) Top() (container.ContainerTopOKBody, error) {
	detail, err := c.Inspect()
	if err != nil {
		return container.ContainerTopOKBody{}, err
	}

	// check container status
	if !detail.State.Running {
		return container.ContainerTopOKBody{}, errors.New("container is not running")
	}

	return c.Client.ContainerTop(context.Background(), c.ID, []string{})
}

// EraseOldHistory removes any history before the user-specified max duration
func (c *Container) EraseOldHistory() {
	if c.Config.UserConfig.Stats.MaxDuration == 0 {
		return
	}

	for i, stat := range c.StatHistory {
		if time.Since(stat.RecordedAt) < c.Config.UserConfig.Stats.MaxDuration {
			c.StatHistory = c.StatHistory[i:]
			return
		}
	}
}

// ViewLogs attaches to a subprocess viewing the container's logs
func (c *Container) ViewLogs() (*exec.Cmd, error) {
	templateString := c.OSCommand.Config.UserConfig.CommandTemplates.ViewContainerLogs
	command := utils.ApplyTemplate(
		templateString,
		c.DockerCommand.NewCommandObject(CommandObject{Container: c}),
	)

	cmd := c.OSCommand.ExecutableFromString(command)
	c.OSCommand.PrepareForChildren(cmd)

	return cmd, nil
}

// PruneContainers prunes containers
func (c *DockerCommand) PruneContainers() error {
	_, err := c.Client.ContainersPrune(context.Background(), filters.Args{})
	return err
}

// Inspect returns details about the container
func (c *Container) Inspect() (types.ContainerJSON, error) {
	return c.Client.ContainerInspect(context.Background(), c.ID)
}

// RenderTop returns details about the container
func (c *Container) RenderTop() (string, error) {
	result, err := c.Top()
	if err != nil {
		return "", err
	}

	return utils.RenderTable(append([][]string{result.Titles}, result.Processes...))
}

// DetailsLoaded tells us whether we have yet loaded the details for a container. Because this is an asynchronous operation, sometimes we have the container before we have its details. Details is a struct, not a pointer to a struct, so it starts off with heaps of zero values. One of which is the container Image, which starts as a blank string. Given that every container should have an image, this is a good proxy to use
func (c *Container) DetailsLoaded() bool {
	return c.Details.Image != ""
}
