package commands

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/go-errors/errors"
	"github.com/jesseduffield/lazydocker/pkg/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
)

// Container : A docker Container
type Container struct {
	Name            string
	ServiceName     string
	ServiceID       string
	ContainerNumber string // might make this an int in the future if need be
	ProjectName     string
	ID              string
	Container       types.Container
	DisplayString   string
	Client          *client.Client
	OSCommand       *OSCommand
	Log             *logrus.Entry
	Stats           ContainerCliStat
	Details         Details
}

type Details struct {
	ID      string    `json:"Id"`
	Created time.Time `json:"Created"`
	Path    string    `json:"Path"`
	Args    []string  `json:"Args"`
	State   struct {
		Status     string    `json:"Status"`
		Running    bool      `json:"Running"`
		Paused     bool      `json:"Paused"`
		Restarting bool      `json:"Restarting"`
		OOMKilled  bool      `json:"OOMKilled"`
		Dead       bool      `json:"Dead"`
		Pid        int       `json:"Pid"`
		ExitCode   int       `json:"ExitCode"`
		Error      string    `json:"Error"`
		StartedAt  time.Time `json:"StartedAt"`
		FinishedAt time.Time `json:"FinishedAt"`
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
		WorkingDir string      `json:"WorkingDir"`
		Entrypoint interface{} `json:"Entrypoint"`
		OnBuild    interface{} `json:"OnBuild"`
		Labels     struct {
			ComDockerComposeOneoff  string `json:"com.docker.compose.oneoff"`
			ComDockerComposeProject string `json:"com.docker.compose.project"`
			ComDockerComposeService string `json:"com.docker.compose.service"`
			ComDockerComposeSlug    string `json:"com.docker.compose.slug"`
			ComDockerComposeVersion string `json:"com.docker.compose.version"`
		} `json:"Labels"`
	} `json:"Config"`
	NetworkSettings struct {
		Bridge                 string `json:"Bridge"`
		SandboxID              string `json:"SandboxID"`
		HairpinMode            bool   `json:"HairpinMode"`
		LinkLocalIPv6Address   string `json:"LinkLocalIPv6Address"`
		LinkLocalIPv6PrefixLen int    `json:"LinkLocalIPv6PrefixLen"`
		Ports                  struct {
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
		Networks               struct {
			ApdevDefault struct {
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
			} `json:"apdev_default"`
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
	return []string{utils.ColoredString(c.Container.State, c.GetColor()), utils.ColoredString(c.Name, color.FgWhite), c.Stats.CPUPerc}
}

// GetColor Container color
func (c *Container) GetColor() color.Attribute {
	switch c.Container.State {
	case "exited":
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
	default:
		return color.FgWhite
	}
}

// MustStopContainer tells us that we must stop the container before removing it
const MustStopContainer = iota

// Remove removes the container
func (c *Container) Remove(options types.ContainerRemoveOptions) error {
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
	return c.Client.ContainerStop(context.Background(), c.ID, nil)
}

// Restart restarts the container
func (c *Container) Restart() error {
	return c.Client.ContainerRestart(context.Background(), c.ID, nil)
}

// RestartService restarts the container
func (c *Container) RestartService() error {
	templateString := c.OSCommand.Config.UserConfig.CommandTemplates.RestartService
	command := utils.ApplyTemplate(templateString, c)
	return c.OSCommand.RunCommand(command)
}

// Attach attaches the container
func (c *Container) Attach() (*exec.Cmd, error) {
	// verify that we can in fact attach to this container
	if !c.Details.Config.AttachStdin {
		return nil, errors.New("Container does not support attaching. You must either run the service with the '-it' flag or use `stdin_open: true, tty: true` in the docker-compose.yml file")
	}

	cmd := c.OSCommand.PrepareSubProcess("docker", "attach", "--sig-proxy=false", c.ID)
	return cmd, nil
}

// Top returns process information
func (c *Container) Top() (types.ContainerProcessList, error) {
	return c.Client.ContainerTop(context.Background(), c.ID, []string{})
}
