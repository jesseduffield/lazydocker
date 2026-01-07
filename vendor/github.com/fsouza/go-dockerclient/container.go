// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	units "github.com/docker/go-units"
)

// APIPort is a type that represents a port mapping returned by the Docker API
type APIPort struct {
	PrivatePort int64  `json:"PrivatePort,omitempty" yaml:"PrivatePort,omitempty" toml:"PrivatePort,omitempty"`
	PublicPort  int64  `json:"PublicPort,omitempty" yaml:"PublicPort,omitempty" toml:"PublicPort,omitempty"`
	Type        string `json:"Type,omitempty" yaml:"Type,omitempty" toml:"Type,omitempty"`
	IP          string `json:"IP,omitempty" yaml:"IP,omitempty" toml:"IP,omitempty"`
}

// APIMount represents a mount point for a container.
type APIMount struct {
	Name        string `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Source      string `json:"Source,omitempty" yaml:"Source,omitempty" toml:"Source,omitempty"`
	Destination string `json:"Destination,omitempty" yaml:"Destination,omitempty" toml:"Destination,omitempty"`
	Driver      string `json:"Driver,omitempty" yaml:"Driver,omitempty" toml:"Driver,omitempty"`
	Mode        string `json:"Mode,omitempty" yaml:"Mode,omitempty" toml:"Mode,omitempty"`
	RW          bool   `json:"RW,omitempty" yaml:"RW,omitempty" toml:"RW,omitempty"`
	Propagation string `json:"Propagation,omitempty" yaml:"Propagation,omitempty" toml:"Propagation,omitempty"`
	Type        string `json:"Type,omitempty" yaml:"Type,omitempty" toml:"Type,omitempty"`
}

// APIContainers represents each container in the list returned by
// ListContainers.
type APIContainers struct {
	ID         string            `json:"Id" yaml:"Id" toml:"Id"`
	Image      string            `json:"Image,omitempty" yaml:"Image,omitempty" toml:"Image,omitempty"`
	Command    string            `json:"Command,omitempty" yaml:"Command,omitempty" toml:"Command,omitempty"`
	Created    int64             `json:"Created,omitempty" yaml:"Created,omitempty" toml:"Created,omitempty"`
	State      string            `json:"State,omitempty" yaml:"State,omitempty" toml:"State,omitempty"`
	Status     string            `json:"Status,omitempty" yaml:"Status,omitempty" toml:"Status,omitempty"`
	Ports      []APIPort         `json:"Ports,omitempty" yaml:"Ports,omitempty" toml:"Ports,omitempty"`
	SizeRw     int64             `json:"SizeRw,omitempty" yaml:"SizeRw,omitempty" toml:"SizeRw,omitempty"`
	SizeRootFs int64             `json:"SizeRootFs,omitempty" yaml:"SizeRootFs,omitempty" toml:"SizeRootFs,omitempty"`
	Names      []string          `json:"Names,omitempty" yaml:"Names,omitempty" toml:"Names,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty" yaml:"Labels,omitempty" toml:"Labels,omitempty"`
	Networks   NetworkList       `json:"NetworkSettings,omitempty" yaml:"NetworkSettings,omitempty" toml:"NetworkSettings,omitempty"`
	Mounts     []APIMount        `json:"Mounts,omitempty" yaml:"Mounts,omitempty" toml:"Mounts,omitempty"`
}

// NetworkList encapsulates a map of networks, as returned by the Docker API in
// ListContainers.
type NetworkList struct {
	Networks map[string]ContainerNetwork `json:"Networks" yaml:"Networks,omitempty" toml:"Networks,omitempty"`
}

// Port represents the port number and the protocol, in the form
// <number>/<protocol>. For example: 80/tcp.
type Port string

// Port returns the number of the port.
func (p Port) Port() string {
	return strings.Split(string(p), "/")[0]
}

// Proto returns the name of the protocol.
func (p Port) Proto() string {
	parts := strings.Split(string(p), "/")
	if len(parts) == 1 {
		return "tcp"
	}
	return parts[1]
}

// HealthCheck represents one check of health.
type HealthCheck struct {
	Start    time.Time `json:"Start,omitempty" yaml:"Start,omitempty" toml:"Start,omitempty"`
	End      time.Time `json:"End,omitempty" yaml:"End,omitempty" toml:"End,omitempty"`
	ExitCode int       `json:"ExitCode,omitempty" yaml:"ExitCode,omitempty" toml:"ExitCode,omitempty"`
	Output   string    `json:"Output,omitempty" yaml:"Output,omitempty" toml:"Output,omitempty"`
}

// Health represents the health of a container.
type Health struct {
	Status        string        `json:"Status,omitempty" yaml:"Status,omitempty" toml:"Status,omitempty"`
	FailingStreak int           `json:"FailingStreak,omitempty" yaml:"FailingStreak,omitempty" toml:"FailingStreak,omitempty"`
	Log           []HealthCheck `json:"Log,omitempty" yaml:"Log,omitempty" toml:"Log,omitempty"`
}

// State represents the state of a container.
type State struct {
	Status            string    `json:"Status,omitempty" yaml:"Status,omitempty" toml:"Status,omitempty"`
	Running           bool      `json:"Running,omitempty" yaml:"Running,omitempty" toml:"Running,omitempty"`
	Paused            bool      `json:"Paused,omitempty" yaml:"Paused,omitempty" toml:"Paused,omitempty"`
	Restarting        bool      `json:"Restarting,omitempty" yaml:"Restarting,omitempty" toml:"Restarting,omitempty"`
	OOMKilled         bool      `json:"OOMKilled,omitempty" yaml:"OOMKilled,omitempty" toml:"OOMKilled,omitempty"`
	RemovalInProgress bool      `json:"RemovalInProgress,omitempty" yaml:"RemovalInProgress,omitempty" toml:"RemovalInProgress,omitempty"`
	Dead              bool      `json:"Dead,omitempty" yaml:"Dead,omitempty" toml:"Dead,omitempty"`
	Pid               int       `json:"Pid,omitempty" yaml:"Pid,omitempty" toml:"Pid,omitempty"`
	ExitCode          int       `json:"ExitCode,omitempty" yaml:"ExitCode,omitempty" toml:"ExitCode,omitempty"`
	Error             string    `json:"Error,omitempty" yaml:"Error,omitempty" toml:"Error,omitempty"`
	StartedAt         time.Time `json:"StartedAt,omitempty" yaml:"StartedAt,omitempty" toml:"StartedAt,omitempty"`
	FinishedAt        time.Time `json:"FinishedAt,omitempty" yaml:"FinishedAt,omitempty" toml:"FinishedAt,omitempty"`
	Health            Health    `json:"Health,omitempty" yaml:"Health,omitempty" toml:"Health,omitempty"`
}

// String returns a human-readable description of the state
func (s *State) String() string {
	if s.Running {
		if s.Paused {
			return fmt.Sprintf("Up %s (Paused)", units.HumanDuration(time.Now().UTC().Sub(s.StartedAt)))
		}
		if s.Restarting {
			return fmt.Sprintf("Restarting (%d) %s ago", s.ExitCode, units.HumanDuration(time.Now().UTC().Sub(s.FinishedAt)))
		}

		return fmt.Sprintf("Up %s", units.HumanDuration(time.Now().UTC().Sub(s.StartedAt)))
	}

	if s.RemovalInProgress {
		return "Removal In Progress"
	}

	if s.Dead {
		return "Dead"
	}

	if s.StartedAt.IsZero() {
		return "Created"
	}

	if s.FinishedAt.IsZero() {
		return ""
	}

	return fmt.Sprintf("Exited (%d) %s ago", s.ExitCode, units.HumanDuration(time.Now().UTC().Sub(s.FinishedAt)))
}

// StateString returns a single string to describe state
func (s *State) StateString() string {
	if s.Running {
		if s.Paused {
			return "paused"
		}
		if s.Restarting {
			return "restarting"
		}
		return "running"
	}

	if s.Dead {
		return "dead"
	}

	if s.StartedAt.IsZero() {
		return "created"
	}

	return "exited"
}

// PortBinding represents the host/container port mapping as returned in the
// `docker inspect` json
type PortBinding struct {
	HostIP   string `json:"HostIp,omitempty" yaml:"HostIp,omitempty" toml:"HostIp,omitempty"`
	HostPort string `json:"HostPort,omitempty" yaml:"HostPort,omitempty" toml:"HostPort,omitempty"`
}

// PortMapping represents a deprecated field in the `docker inspect` output,
// and its value as found in NetworkSettings should always be nil
type PortMapping map[string]string

// ContainerNetwork represents the networking settings of a container per network.
type ContainerNetwork struct {
	Aliases             []string `json:"Aliases,omitempty" yaml:"Aliases,omitempty" toml:"Aliases,omitempty"`
	MacAddress          string   `json:"MacAddress,omitempty" yaml:"MacAddress,omitempty" toml:"MacAddress,omitempty"`
	GlobalIPv6PrefixLen int      `json:"GlobalIPv6PrefixLen,omitempty" yaml:"GlobalIPv6PrefixLen,omitempty" toml:"GlobalIPv6PrefixLen,omitempty"`
	GlobalIPv6Address   string   `json:"GlobalIPv6Address,omitempty" yaml:"GlobalIPv6Address,omitempty" toml:"GlobalIPv6Address,omitempty"`
	IPv6Gateway         string   `json:"IPv6Gateway,omitempty" yaml:"IPv6Gateway,omitempty" toml:"IPv6Gateway,omitempty"`
	IPPrefixLen         int      `json:"IPPrefixLen,omitempty" yaml:"IPPrefixLen,omitempty" toml:"IPPrefixLen,omitempty"`
	IPAddress           string   `json:"IPAddress,omitempty" yaml:"IPAddress,omitempty" toml:"IPAddress,omitempty"`
	Gateway             string   `json:"Gateway,omitempty" yaml:"Gateway,omitempty" toml:"Gateway,omitempty"`
	EndpointID          string   `json:"EndpointID,omitempty" yaml:"EndpointID,omitempty" toml:"EndpointID,omitempty"`
	NetworkID           string   `json:"NetworkID,omitempty" yaml:"NetworkID,omitempty" toml:"NetworkID,omitempty"`
}

// NetworkSettings contains network-related information about a container
type NetworkSettings struct {
	Networks               map[string]ContainerNetwork `json:"Networks,omitempty" yaml:"Networks,omitempty" toml:"Networks,omitempty"`
	IPAddress              string                      `json:"IPAddress,omitempty" yaml:"IPAddress,omitempty" toml:"IPAddress,omitempty"`
	IPPrefixLen            int                         `json:"IPPrefixLen,omitempty" yaml:"IPPrefixLen,omitempty" toml:"IPPrefixLen,omitempty"`
	MacAddress             string                      `json:"MacAddress,omitempty" yaml:"MacAddress,omitempty" toml:"MacAddress,omitempty"`
	Gateway                string                      `json:"Gateway,omitempty" yaml:"Gateway,omitempty" toml:"Gateway,omitempty"`
	Bridge                 string                      `json:"Bridge,omitempty" yaml:"Bridge,omitempty" toml:"Bridge,omitempty"`
	PortMapping            map[string]PortMapping      `json:"PortMapping,omitempty" yaml:"PortMapping,omitempty" toml:"PortMapping,omitempty"`
	Ports                  map[Port][]PortBinding      `json:"Ports,omitempty" yaml:"Ports,omitempty" toml:"Ports,omitempty"`
	NetworkID              string                      `json:"NetworkID,omitempty" yaml:"NetworkID,omitempty" toml:"NetworkID,omitempty"`
	EndpointID             string                      `json:"EndpointID,omitempty" yaml:"EndpointID,omitempty" toml:"EndpointID,omitempty"`
	SandboxKey             string                      `json:"SandboxKey,omitempty" yaml:"SandboxKey,omitempty" toml:"SandboxKey,omitempty"`
	GlobalIPv6Address      string                      `json:"GlobalIPv6Address,omitempty" yaml:"GlobalIPv6Address,omitempty" toml:"GlobalIPv6Address,omitempty"`
	GlobalIPv6PrefixLen    int                         `json:"GlobalIPv6PrefixLen,omitempty" yaml:"GlobalIPv6PrefixLen,omitempty" toml:"GlobalIPv6PrefixLen,omitempty"`
	IPv6Gateway            string                      `json:"IPv6Gateway,omitempty" yaml:"IPv6Gateway,omitempty" toml:"IPv6Gateway,omitempty"`
	LinkLocalIPv6Address   string                      `json:"LinkLocalIPv6Address,omitempty" yaml:"LinkLocalIPv6Address,omitempty" toml:"LinkLocalIPv6Address,omitempty"`
	LinkLocalIPv6PrefixLen int                         `json:"LinkLocalIPv6PrefixLen,omitempty" yaml:"LinkLocalIPv6PrefixLen,omitempty" toml:"LinkLocalIPv6PrefixLen,omitempty"`
	SecondaryIPAddresses   []string                    `json:"SecondaryIPAddresses,omitempty" yaml:"SecondaryIPAddresses,omitempty" toml:"SecondaryIPAddresses,omitempty"`
	SecondaryIPv6Addresses []string                    `json:"SecondaryIPv6Addresses,omitempty" yaml:"SecondaryIPv6Addresses,omitempty" toml:"SecondaryIPv6Addresses,omitempty"`
}

// PortMappingAPI translates the port mappings as contained in NetworkSettings
// into the format in which they would appear when returned by the API
func (settings *NetworkSettings) PortMappingAPI() []APIPort {
	var mapping []APIPort
	for port, bindings := range settings.Ports {
		p, _ := parsePort(port.Port())
		if len(bindings) == 0 {
			mapping = append(mapping, APIPort{
				PrivatePort: int64(p),
				Type:        port.Proto(),
			})
			continue
		}
		for _, binding := range bindings {
			p, _ := parsePort(port.Port())
			h, _ := parsePort(binding.HostPort)
			mapping = append(mapping, APIPort{
				PrivatePort: int64(p),
				PublicPort:  int64(h),
				Type:        port.Proto(),
				IP:          binding.HostIP,
			})
		}
	}
	return mapping
}

func parsePort(rawPort string) (int, error) {
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		return 0, err
	}
	return int(port), nil
}

// Config is the list of configuration options used when creating a container.
// Config does not contain the options that are specific to starting a container on a
// given host.  Those are contained in HostConfig
type Config struct {
	Hostname          string              `json:"Hostname,omitempty" yaml:"Hostname,omitempty" toml:"Hostname,omitempty"`
	Domainname        string              `json:"Domainname,omitempty" yaml:"Domainname,omitempty" toml:"Domainname,omitempty"`
	User              string              `json:"User,omitempty" yaml:"User,omitempty" toml:"User,omitempty"`
	Memory            int64               `json:"Memory,omitempty" yaml:"Memory,omitempty" toml:"Memory,omitempty"`
	MemorySwap        int64               `json:"MemorySwap,omitempty" yaml:"MemorySwap,omitempty" toml:"MemorySwap,omitempty"`
	MemoryReservation int64               `json:"MemoryReservation,omitempty" yaml:"MemoryReservation,omitempty" toml:"MemoryReservation,omitempty"`
	KernelMemory      int64               `json:"KernelMemory,omitempty" yaml:"KernelMemory,omitempty" toml:"KernelMemory,omitempty"`
	CPUShares         int64               `json:"CpuShares,omitempty" yaml:"CpuShares,omitempty" toml:"CpuShares,omitempty"`
	CPUSet            string              `json:"Cpuset,omitempty" yaml:"Cpuset,omitempty" toml:"Cpuset,omitempty"`
	PortSpecs         []string            `json:"PortSpecs,omitempty" yaml:"PortSpecs,omitempty" toml:"PortSpecs,omitempty"`
	ExposedPorts      map[Port]struct{}   `json:"ExposedPorts,omitempty" yaml:"ExposedPorts,omitempty" toml:"ExposedPorts,omitempty"`
	PublishService    string              `json:"PublishService,omitempty" yaml:"PublishService,omitempty" toml:"PublishService,omitempty"`
	StopSignal        string              `json:"StopSignal,omitempty" yaml:"StopSignal,omitempty" toml:"StopSignal,omitempty"`
	StopTimeout       int                 `json:"StopTimeout,omitempty" yaml:"StopTimeout,omitempty" toml:"StopTimeout,omitempty"`
	Env               []string            `json:"Env,omitempty" yaml:"Env,omitempty" toml:"Env,omitempty"`
	Cmd               []string            `json:"Cmd" yaml:"Cmd" toml:"Cmd"`
	Shell             []string            `json:"Shell,omitempty" yaml:"Shell,omitempty" toml:"Shell,omitempty"`
	Healthcheck       *HealthConfig       `json:"Healthcheck,omitempty" yaml:"Healthcheck,omitempty" toml:"Healthcheck,omitempty"`
	DNS               []string            `json:"Dns,omitempty" yaml:"Dns,omitempty" toml:"Dns,omitempty"` // For Docker API v1.9 and below only
	Image             string              `json:"Image,omitempty" yaml:"Image,omitempty" toml:"Image,omitempty"`
	Volumes           map[string]struct{} `json:"Volumes,omitempty" yaml:"Volumes,omitempty" toml:"Volumes,omitempty"`
	VolumeDriver      string              `json:"VolumeDriver,omitempty" yaml:"VolumeDriver,omitempty" toml:"VolumeDriver,omitempty"`
	WorkingDir        string              `json:"WorkingDir,omitempty" yaml:"WorkingDir,omitempty" toml:"WorkingDir,omitempty"`
	MacAddress        string              `json:"MacAddress,omitempty" yaml:"MacAddress,omitempty" toml:"MacAddress,omitempty"`
	Entrypoint        []string            `json:"Entrypoint" yaml:"Entrypoint" toml:"Entrypoint"`
	SecurityOpts      []string            `json:"SecurityOpts,omitempty" yaml:"SecurityOpts,omitempty" toml:"SecurityOpts,omitempty"`
	OnBuild           []string            `json:"OnBuild,omitempty" yaml:"OnBuild,omitempty" toml:"OnBuild,omitempty"`
	Mounts            []Mount             `json:"Mounts,omitempty" yaml:"Mounts,omitempty" toml:"Mounts,omitempty"`
	Labels            map[string]string   `json:"Labels,omitempty" yaml:"Labels,omitempty" toml:"Labels,omitempty"`
	AttachStdin       bool                `json:"AttachStdin,omitempty" yaml:"AttachStdin,omitempty" toml:"AttachStdin,omitempty"`
	AttachStdout      bool                `json:"AttachStdout,omitempty" yaml:"AttachStdout,omitempty" toml:"AttachStdout,omitempty"`
	AttachStderr      bool                `json:"AttachStderr,omitempty" yaml:"AttachStderr,omitempty" toml:"AttachStderr,omitempty"`
	ArgsEscaped       bool                `json:"ArgsEscaped,omitempty" yaml:"ArgsEscaped,omitempty" toml:"ArgsEscaped,omitempty"`
	Tty               bool                `json:"Tty,omitempty" yaml:"Tty,omitempty" toml:"Tty,omitempty"`
	OpenStdin         bool                `json:"OpenStdin,omitempty" yaml:"OpenStdin,omitempty" toml:"OpenStdin,omitempty"`
	StdinOnce         bool                `json:"StdinOnce,omitempty" yaml:"StdinOnce,omitempty" toml:"StdinOnce,omitempty"`
	NetworkDisabled   bool                `json:"NetworkDisabled,omitempty" yaml:"NetworkDisabled,omitempty" toml:"NetworkDisabled,omitempty"`

	// This is no longer used and has been kept here for backward
	// compatibility, please use HostConfig.VolumesFrom.
	VolumesFrom string `json:"VolumesFrom,omitempty" yaml:"VolumesFrom,omitempty" toml:"VolumesFrom,omitempty"`
}

// HostMount represents a mount point in the container in HostConfig.
//
// It has been added in the version 1.25 of the Docker API
type HostMount struct {
	Target        string         `json:"Target,omitempty" yaml:"Target,omitempty" toml:"Target,omitempty"`
	Source        string         `json:"Source,omitempty" yaml:"Source,omitempty" toml:"Source,omitempty"`
	Type          string         `json:"Type,omitempty" yaml:"Type,omitempty" toml:"Type,omitempty"`
	ReadOnly      bool           `json:"ReadOnly,omitempty" yaml:"ReadOnly,omitempty" toml:"ReadOnly,omitempty"`
	BindOptions   *BindOptions   `json:"BindOptions,omitempty" yaml:"BindOptions,omitempty" toml:"BindOptions,omitempty"`
	VolumeOptions *VolumeOptions `json:"VolumeOptions,omitempty" yaml:"VolumeOptions,omitempty" toml:"VolumeOptions,omitempty"`
	TempfsOptions *TempfsOptions `json:"TmpfsOptions,omitempty" yaml:"TmpfsOptions,omitempty" toml:"TmpfsOptions,omitempty"`
}

// BindOptions contains optional configuration for the bind type
type BindOptions struct {
	Propagation string `json:"Propagation,omitempty" yaml:"Propagation,omitempty" toml:"Propagation,omitempty"`
}

// VolumeOptions contains optional configuration for the volume type
type VolumeOptions struct {
	NoCopy       bool               `json:"NoCopy,omitempty" yaml:"NoCopy,omitempty" toml:"NoCopy,omitempty"`
	Labels       map[string]string  `json:"Labels,omitempty" yaml:"Labels,omitempty" toml:"Labels,omitempty"`
	DriverConfig VolumeDriverConfig `json:"DriverConfig,omitempty" yaml:"DriverConfig,omitempty" toml:"DriverConfig,omitempty"`
}

// TempfsOptions contains optional configuration for the tempfs type
type TempfsOptions struct {
	SizeBytes int64      `json:"SizeBytes,omitempty" yaml:"SizeBytes,omitempty" toml:"SizeBytes,omitempty"`
	Mode      int        `json:"Mode,omitempty" yaml:"Mode,omitempty" toml:"Mode,omitempty"`
	Options   [][]string `json:"Options,omitempty" yaml:"Options,omitempty" toml:"Options,omitempty"`
}

// VolumeDriverConfig holds a map of volume driver specific options
type VolumeDriverConfig struct {
	Name    string            `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Options map[string]string `json:"Options,omitempty" yaml:"Options,omitempty" toml:"Options,omitempty"`
}

// Mount represents a mount point in the container.
//
// It has been added in the version 1.20 of the Docker API, available since
// Docker 1.8.
type Mount struct {
	Name        string
	Source      string
	Destination string
	Driver      string
	Mode        string
	RW          bool
}

// LogConfig defines the log driver type and the configuration for it.
type LogConfig struct {
	Type   string            `json:"Type,omitempty" yaml:"Type,omitempty" toml:"Type,omitempty"`
	Config map[string]string `json:"Config,omitempty" yaml:"Config,omitempty" toml:"Config,omitempty"`
}

// ULimit defines system-wide resource limitations This can help a lot in
// system administration, e.g. when a user starts too many processes and
// therefore makes the system unresponsive for other users.
type ULimit struct {
	Name string `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Soft int64  `json:"Soft,omitempty" yaml:"Soft,omitempty" toml:"Soft,omitempty"`
	Hard int64  `json:"Hard,omitempty" yaml:"Hard,omitempty" toml:"Hard,omitempty"`
}

// SwarmNode containers information about which Swarm node the container is on.
type SwarmNode struct {
	ID     string            `json:"ID,omitempty" yaml:"ID,omitempty" toml:"ID,omitempty"`
	IP     string            `json:"IP,omitempty" yaml:"IP,omitempty" toml:"IP,omitempty"`
	Addr   string            `json:"Addr,omitempty" yaml:"Addr,omitempty" toml:"Addr,omitempty"`
	Name   string            `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	CPUs   int64             `json:"CPUs,omitempty" yaml:"CPUs,omitempty" toml:"CPUs,omitempty"`
	Memory int64             `json:"Memory,omitempty" yaml:"Memory,omitempty" toml:"Memory,omitempty"`
	Labels map[string]string `json:"Labels,omitempty" yaml:"Labels,omitempty" toml:"Labels,omitempty"`
}

// GraphDriver contains information about the GraphDriver used by the
// container.
type GraphDriver struct {
	Name string            `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Data map[string]string `json:"Data,omitempty" yaml:"Data,omitempty" toml:"Data,omitempty"`
}

// HealthConfig holds configuration settings for the HEALTHCHECK feature
//
// It has been added in the version 1.24 of the Docker API, available since
// Docker 1.12.
type HealthConfig struct {
	// Test is the test to perform to check that the container is healthy.
	// An empty slice means to inherit the default.
	// The options are:
	// {} : inherit healthcheck
	// {"NONE"} : disable healthcheck
	// {"CMD", args...} : exec arguments directly
	// {"CMD-SHELL", command} : run command with system's default shell
	Test []string `json:"Test,omitempty" yaml:"Test,omitempty" toml:"Test,omitempty"`

	// Zero means to inherit. Durations are expressed as integer nanoseconds.
	Interval      time.Duration `json:"Interval,omitempty" yaml:"Interval,omitempty" toml:"Interval,omitempty"`                // Interval is the time to wait between checks.
	Timeout       time.Duration `json:"Timeout,omitempty" yaml:"Timeout,omitempty" toml:"Timeout,omitempty"`                   // Timeout is the time to wait before considering the check to have hung.
	StartPeriod   time.Duration `json:"StartPeriod,omitempty" yaml:"StartPeriod,omitempty" toml:"StartPeriod,omitempty"`       // The start period for the container to initialize before the retries starts to count down.
	StartInterval time.Duration `json:"StartInterval,omitempty" yaml:"StartInterval,omitempty" toml:"StartInterval,omitempty"` // The start interval is the time to wait between checks during the start period.

	// Retries is the number of consecutive failures needed to consider a container as unhealthy.
	// Zero means inherit.
	Retries int `json:"Retries,omitempty" yaml:"Retries,omitempty" toml:"Retries,omitempty"`
}

// Container is the type encompasing everything about a container - its config,
// hostconfig, etc.
type Container struct {
	ID string `json:"Id" yaml:"Id" toml:"Id"`

	Created time.Time `json:"Created,omitempty" yaml:"Created,omitempty" toml:"Created,omitempty"`

	Path string   `json:"Path,omitempty" yaml:"Path,omitempty" toml:"Path,omitempty"`
	Args []string `json:"Args,omitempty" yaml:"Args,omitempty" toml:"Args,omitempty"`

	Config *Config `json:"Config,omitempty" yaml:"Config,omitempty" toml:"Config,omitempty"`
	State  State   `json:"State,omitempty" yaml:"State,omitempty" toml:"State,omitempty"`
	Image  string  `json:"Image,omitempty" yaml:"Image,omitempty" toml:"Image,omitempty"`

	Node *SwarmNode `json:"Node,omitempty" yaml:"Node,omitempty" toml:"Node,omitempty"`

	NetworkSettings *NetworkSettings `json:"NetworkSettings,omitempty" yaml:"NetworkSettings,omitempty" toml:"NetworkSettings,omitempty"`

	SysInitPath    string  `json:"SysInitPath,omitempty" yaml:"SysInitPath,omitempty" toml:"SysInitPath,omitempty"`
	ResolvConfPath string  `json:"ResolvConfPath,omitempty" yaml:"ResolvConfPath,omitempty" toml:"ResolvConfPath,omitempty"`
	HostnamePath   string  `json:"HostnamePath,omitempty" yaml:"HostnamePath,omitempty" toml:"HostnamePath,omitempty"`
	HostsPath      string  `json:"HostsPath,omitempty" yaml:"HostsPath,omitempty" toml:"HostsPath,omitempty"`
	LogPath        string  `json:"LogPath,omitempty" yaml:"LogPath,omitempty" toml:"LogPath,omitempty"`
	Name           string  `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Driver         string  `json:"Driver,omitempty" yaml:"Driver,omitempty" toml:"Driver,omitempty"`
	Mounts         []Mount `json:"Mounts,omitempty" yaml:"Mounts,omitempty" toml:"Mounts,omitempty"`

	Volumes     map[string]string `json:"Volumes,omitempty" yaml:"Volumes,omitempty" toml:"Volumes,omitempty"`
	VolumesRW   map[string]bool   `json:"VolumesRW,omitempty" yaml:"VolumesRW,omitempty" toml:"VolumesRW,omitempty"`
	HostConfig  *HostConfig       `json:"HostConfig,omitempty" yaml:"HostConfig,omitempty" toml:"HostConfig,omitempty"`
	ExecIDs     []string          `json:"ExecIDs,omitempty" yaml:"ExecIDs,omitempty" toml:"ExecIDs,omitempty"`
	GraphDriver *GraphDriver      `json:"GraphDriver,omitempty" yaml:"GraphDriver,omitempty" toml:"GraphDriver,omitempty"`

	RestartCount int `json:"RestartCount,omitempty" yaml:"RestartCount,omitempty" toml:"RestartCount,omitempty"`

	AppArmorProfile string `json:"AppArmorProfile,omitempty" yaml:"AppArmorProfile,omitempty" toml:"AppArmorProfile,omitempty"`

	MountLabel   string `json:"MountLabel,omitempty" yaml:"MountLabel,omitempty" toml:"MountLabel,omitempty"`
	ProcessLabel string `json:"ProcessLabel,omitempty" yaml:"ProcessLabel,omitempty" toml:"ProcessLabel,omitempty"`
	Platform     string `json:"Platform,omitempty" yaml:"Platform,omitempty" toml:"Platform,omitempty"`
	SizeRw       int64  `json:"SizeRw,omitempty" yaml:"SizeRw,omitempty" toml:"SizeRw,omitempty"`
	SizeRootFs   int64  `json:"SizeRootFs,omitempty" yaml:"SizeRootFs,omitempty" toml:"SizeRootFs,omitempty"`
}

// KeyValuePair is a type for generic key/value pairs as used in the Lxc
// configuration
type KeyValuePair struct {
	Key   string `json:"Key,omitempty" yaml:"Key,omitempty" toml:"Key,omitempty"`
	Value string `json:"Value,omitempty" yaml:"Value,omitempty" toml:"Value,omitempty"`
}

// Device represents a device mapping between the Docker host and the
// container.
type Device struct {
	PathOnHost        string `json:"PathOnHost,omitempty" yaml:"PathOnHost,omitempty" toml:"PathOnHost,omitempty"`
	PathInContainer   string `json:"PathInContainer,omitempty" yaml:"PathInContainer,omitempty" toml:"PathInContainer,omitempty"`
	CgroupPermissions string `json:"CgroupPermissions,omitempty" yaml:"CgroupPermissions,omitempty" toml:"CgroupPermissions,omitempty"`
}

// DeviceRequest represents a request for device that's sent to device drivers.
type DeviceRequest struct {
	Driver       string            `json:"Driver,omitempty" yaml:"Driver,omitempty" toml:"Driver,omitempty"`
	Count        int               `json:"Count,omitempty" yaml:"Count,omitempty" toml:"Count,omitempty"`
	DeviceIDs    []string          `json:"DeviceIDs,omitempty" yaml:"DeviceIDs,omitempty" toml:"DeviceIDs,omitempty"`
	Capabilities [][]string        `json:"Capabilities,omitempty" yaml:"Capabilities,omitempty" toml:"Capabilities,omitempty"`
	Options      map[string]string `json:"Options,omitempty" yaml:"Options,omitempty" toml:"Options,omitempty"`
}

// BlockWeight represents a relative device weight for an individual device inside
// of a container
type BlockWeight struct {
	Path   string `json:"Path,omitempty"`
	Weight string `json:"Weight,omitempty"`
}

// BlockLimit represents a read/write limit in IOPS or Bandwidth for a device
// inside of a container
type BlockLimit struct {
	Path string `json:"Path,omitempty"`
	Rate int64  `json:"Rate,omitempty"`
}

// HostConfig contains the container options related to starting a container on
// a given host
type HostConfig struct {
	Binds                []string               `json:"Binds,omitempty" yaml:"Binds,omitempty" toml:"Binds,omitempty"`
	CapAdd               []string               `json:"CapAdd,omitempty" yaml:"CapAdd,omitempty" toml:"CapAdd,omitempty"`
	CapDrop              []string               `json:"CapDrop,omitempty" yaml:"CapDrop,omitempty" toml:"CapDrop,omitempty"`
	Capabilities         []string               `json:"Capabilities,omitempty" yaml:"Capabilities,omitempty" toml:"Capabilities,omitempty"` // Mutually exclusive w.r.t. CapAdd and CapDrop API v1.40
	GroupAdd             []string               `json:"GroupAdd,omitempty" yaml:"GroupAdd,omitempty" toml:"GroupAdd,omitempty"`
	ContainerIDFile      string                 `json:"ContainerIDFile,omitempty" yaml:"ContainerIDFile,omitempty" toml:"ContainerIDFile,omitempty"`
	LxcConf              []KeyValuePair         `json:"LxcConf,omitempty" yaml:"LxcConf,omitempty" toml:"LxcConf,omitempty"`
	PortBindings         map[Port][]PortBinding `json:"PortBindings,omitempty" yaml:"PortBindings,omitempty" toml:"PortBindings,omitempty"`
	Links                []string               `json:"Links,omitempty" yaml:"Links,omitempty" toml:"Links,omitempty"`
	DNS                  []string               `json:"Dns,omitempty" yaml:"Dns,omitempty" toml:"Dns,omitempty"` // For Docker API v1.10 and above only
	DNSOptions           []string               `json:"DnsOptions,omitempty" yaml:"DnsOptions,omitempty" toml:"DnsOptions,omitempty"`
	DNSSearch            []string               `json:"DnsSearch,omitempty" yaml:"DnsSearch,omitempty" toml:"DnsSearch,omitempty"`
	ExtraHosts           []string               `json:"ExtraHosts,omitempty" yaml:"ExtraHosts,omitempty" toml:"ExtraHosts,omitempty"`
	VolumesFrom          []string               `json:"VolumesFrom,omitempty" yaml:"VolumesFrom,omitempty" toml:"VolumesFrom,omitempty"`
	UsernsMode           string                 `json:"UsernsMode,omitempty" yaml:"UsernsMode,omitempty" toml:"UsernsMode,omitempty"`
	NetworkMode          string                 `json:"NetworkMode,omitempty" yaml:"NetworkMode,omitempty" toml:"NetworkMode,omitempty"`
	IpcMode              string                 `json:"IpcMode,omitempty" yaml:"IpcMode,omitempty" toml:"IpcMode,omitempty"`
	Isolation            string                 `json:"Isolation,omitempty" yaml:"Isolation,omitempty" toml:"Isolation,omitempty"`       // Windows only
	ConsoleSize          [2]int                 `json:"ConsoleSize,omitempty" yaml:"ConsoleSize,omitempty" toml:"ConsoleSize,omitempty"` // Windows only height x width
	PidMode              string                 `json:"PidMode,omitempty" yaml:"PidMode,omitempty" toml:"PidMode,omitempty"`
	UTSMode              string                 `json:"UTSMode,omitempty" yaml:"UTSMode,omitempty" toml:"UTSMode,omitempty"`
	RestartPolicy        RestartPolicy          `json:"RestartPolicy,omitempty" yaml:"RestartPolicy,omitempty" toml:"RestartPolicy,omitempty"`
	Devices              []Device               `json:"Devices,omitempty" yaml:"Devices,omitempty" toml:"Devices,omitempty"`
	DeviceCgroupRules    []string               `json:"DeviceCgroupRules,omitempty" yaml:"DeviceCgroupRules,omitempty" toml:"DeviceCgroupRules,omitempty"`
	DeviceRequests       []DeviceRequest        `json:"DeviceRequests,omitempty" yaml:"DeviceRequests,omitempty" toml:"DeviceRequests,omitempty"`
	LogConfig            LogConfig              `json:"LogConfig,omitempty" yaml:"LogConfig,omitempty" toml:"LogConfig,omitempty"`
	SecurityOpt          []string               `json:"SecurityOpt,omitempty" yaml:"SecurityOpt,omitempty" toml:"SecurityOpt,omitempty"`
	CgroupnsMode         string                 `json:"CgroupnsMode,omitempty" yaml:"CgroupnsMode,omitempty" toml:"CgroupnsMode,omitempty"` // v1.40+
	Cgroup               string                 `json:"Cgroup,omitempty" yaml:"Cgroup,omitempty" toml:"Cgroup,omitempty"`
	CgroupParent         string                 `json:"CgroupParent,omitempty" yaml:"CgroupParent,omitempty" toml:"CgroupParent,omitempty"`
	Memory               int64                  `json:"Memory,omitempty" yaml:"Memory,omitempty" toml:"Memory,omitempty"`
	MemoryReservation    int64                  `json:"MemoryReservation,omitempty" yaml:"MemoryReservation,omitempty" toml:"MemoryReservation,omitempty"`
	KernelMemory         int64                  `json:"KernelMemory,omitempty" yaml:"KernelMemory,omitempty" toml:"KernelMemory,omitempty"`
	MemorySwap           int64                  `json:"MemorySwap,omitempty" yaml:"MemorySwap,omitempty" toml:"MemorySwap,omitempty"`
	CPUShares            int64                  `json:"CpuShares,omitempty" yaml:"CpuShares,omitempty" toml:"CpuShares,omitempty"`
	CPUSet               string                 `json:"Cpuset,omitempty" yaml:"Cpuset,omitempty" toml:"Cpuset,omitempty"`
	CPUSetCPUs           string                 `json:"CpusetCpus,omitempty" yaml:"CpusetCpus,omitempty" toml:"CpusetCpus,omitempty"`
	CPUSetMEMs           string                 `json:"CpusetMems,omitempty" yaml:"CpusetMems,omitempty" toml:"CpusetMems,omitempty"`
	CPUQuota             int64                  `json:"CpuQuota,omitempty" yaml:"CpuQuota,omitempty" toml:"CpuQuota,omitempty"`
	CPUPeriod            int64                  `json:"CpuPeriod,omitempty" yaml:"CpuPeriod,omitempty" toml:"CpuPeriod,omitempty"`
	CPURealtimePeriod    int64                  `json:"CpuRealtimePeriod,omitempty" yaml:"CpuRealtimePeriod,omitempty" toml:"CpuRealtimePeriod,omitempty"`
	CPURealtimeRuntime   int64                  `json:"CpuRealtimeRuntime,omitempty" yaml:"CpuRealtimeRuntime,omitempty" toml:"CpuRealtimeRuntime,omitempty"`
	NanoCPUs             int64                  `json:"NanoCpus,omitempty" yaml:"NanoCpus,omitempty" toml:"NanoCpus,omitempty"`
	BlkioWeight          int64                  `json:"BlkioWeight,omitempty" yaml:"BlkioWeight,omitempty" toml:"BlkioWeight,omitempty"`
	BlkioWeightDevice    []BlockWeight          `json:"BlkioWeightDevice,omitempty" yaml:"BlkioWeightDevice,omitempty" toml:"BlkioWeightDevice,omitempty"`
	BlkioDeviceReadBps   []BlockLimit           `json:"BlkioDeviceReadBps,omitempty" yaml:"BlkioDeviceReadBps,omitempty" toml:"BlkioDeviceReadBps,omitempty"`
	BlkioDeviceReadIOps  []BlockLimit           `json:"BlkioDeviceReadIOps,omitempty" yaml:"BlkioDeviceReadIOps,omitempty" toml:"BlkioDeviceReadIOps,omitempty"`
	BlkioDeviceWriteBps  []BlockLimit           `json:"BlkioDeviceWriteBps,omitempty" yaml:"BlkioDeviceWriteBps,omitempty" toml:"BlkioDeviceWriteBps,omitempty"`
	BlkioDeviceWriteIOps []BlockLimit           `json:"BlkioDeviceWriteIOps,omitempty" yaml:"BlkioDeviceWriteIOps,omitempty" toml:"BlkioDeviceWriteIOps,omitempty"`
	Ulimits              []ULimit               `json:"Ulimits,omitempty" yaml:"Ulimits,omitempty" toml:"Ulimits,omitempty"`
	VolumeDriver         string                 `json:"VolumeDriver,omitempty" yaml:"VolumeDriver,omitempty" toml:"VolumeDriver,omitempty"`
	OomScoreAdj          int                    `json:"OomScoreAdj,omitempty" yaml:"OomScoreAdj,omitempty" toml:"OomScoreAdj,omitempty"`
	MemorySwappiness     *int64                 `json:"MemorySwappiness,omitempty" yaml:"MemorySwappiness,omitempty" toml:"MemorySwappiness,omitempty"`
	PidsLimit            *int64                 `json:"PidsLimit,omitempty" yaml:"PidsLimit,omitempty" toml:"PidsLimit,omitempty"`
	OOMKillDisable       *bool                  `json:"OomKillDisable,omitempty" yaml:"OomKillDisable,omitempty" toml:"OomKillDisable,omitempty"`
	ShmSize              int64                  `json:"ShmSize,omitempty" yaml:"ShmSize,omitempty" toml:"ShmSize,omitempty"`
	Tmpfs                map[string]string      `json:"Tmpfs,omitempty" yaml:"Tmpfs,omitempty" toml:"Tmpfs,omitempty"`
	StorageOpt           map[string]string      `json:"StorageOpt,omitempty" yaml:"StorageOpt,omitempty" toml:"StorageOpt,omitempty"`
	Sysctls              map[string]string      `json:"Sysctls,omitempty" yaml:"Sysctls,omitempty" toml:"Sysctls,omitempty"`
	CPUCount             int64                  `json:"CpuCount,omitempty" yaml:"CpuCount,omitempty"`
	CPUPercent           int64                  `json:"CpuPercent,omitempty" yaml:"CpuPercent,omitempty"`
	IOMaximumBandwidth   int64                  `json:"IOMaximumBandwidth,omitempty" yaml:"IOMaximumBandwidth,omitempty"`
	IOMaximumIOps        int64                  `json:"IOMaximumIOps,omitempty" yaml:"IOMaximumIOps,omitempty"`
	Mounts               []HostMount            `json:"Mounts,omitempty" yaml:"Mounts,omitempty" toml:"Mounts,omitempty"`
	MaskedPaths          []string               `json:"MaskedPaths,omitempty" yaml:"MaskedPaths,omitempty" toml:"MaskedPaths,omitempty"`
	ReadonlyPaths        []string               `json:"ReadonlyPaths,omitempty" yaml:"ReadonlyPaths,omitempty" toml:"ReadonlyPaths,omitempty"`
	Runtime              string                 `json:"Runtime,omitempty" yaml:"Runtime,omitempty" toml:"Runtime,omitempty"`
	Init                 bool                   `json:",omitempty" yaml:",omitempty"`
	Privileged           bool                   `json:"Privileged,omitempty" yaml:"Privileged,omitempty" toml:"Privileged,omitempty"`
	PublishAllPorts      bool                   `json:"PublishAllPorts,omitempty" yaml:"PublishAllPorts,omitempty" toml:"PublishAllPorts,omitempty"`
	ReadonlyRootfs       bool                   `json:"ReadonlyRootfs,omitempty" yaml:"ReadonlyRootfs,omitempty" toml:"ReadonlyRootfs,omitempty"`
	AutoRemove           bool                   `json:"AutoRemove,omitempty" yaml:"AutoRemove,omitempty" toml:"AutoRemove,omitempty"`
	Annotations          map[string]string      `json:"Annotations,omitempty" yaml:"Annotations,omitempty" toml:"Annotations,omitempty"`
}

// NetworkingConfig represents the container's networking configuration for each of its interfaces
// Carries the networking configs specified in the `docker run` and `docker network connect` commands
type NetworkingConfig struct {
	EndpointsConfig map[string]*EndpointConfig `json:"EndpointsConfig" yaml:"EndpointsConfig" toml:"EndpointsConfig"` // Endpoint configs for each connecting network
}

// NoSuchContainer is the error returned when a given container does not exist.
type NoSuchContainer struct {
	ID  string
	Err error
}

func (err *NoSuchContainer) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}
	return "No such container: " + err.ID
}

// ContainerAlreadyRunning is the error returned when a given container is
// already running.
type ContainerAlreadyRunning struct {
	ID string
}

func (err *ContainerAlreadyRunning) Error() string {
	return "Container already running: " + err.ID
}

// ContainerNotRunning is the error returned when a given container is not
// running.
type ContainerNotRunning struct {
	ID string
}

func (err *ContainerNotRunning) Error() string {
	return "Container not running: " + err.ID
}
