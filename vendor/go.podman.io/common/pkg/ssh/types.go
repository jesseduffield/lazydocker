package ssh

import (
	"net/url"
	"time"

	"go.podman.io/storage/pkg/idtools"
	"golang.org/x/crypto/ssh"
)

type EngineMode string

const (
	NativeMode  = EngineMode("native")
	GolangMode  = EngineMode("golang")
	InvalidMode = EngineMode("invalid")
)

type ConnectionCreateOptions struct {
	Name     string
	Path     string
	User     *url.Userinfo
	Port     int
	Identity string
	Socket   string
	Default  bool
	Farm     string
}

type ConnectionDialOptions struct {
	Host                        string
	Identity                    string
	User                        *url.Userinfo
	Port                        int
	Auth                        []string
	Timeout                     time.Duration
	InsecureIsMachineConnection bool
}

type ConnectionDialReport struct {
	Client *ssh.Client
}

type ConnectionExecOptions struct {
	Host     string
	Identity string
	User     *url.Userinfo
	Port     int
	Auth     []string
	Args     []string
	Timeout  time.Duration
}

type ConnectionExecReport struct {
	Response string
}

type ConnectionScpOptions struct {
	User        *url.Userinfo
	Source      string
	Destination string
	Identity    string
	Port        int
}

type ConnectionScpReport struct {
	Response string
}

// Info is the overall struct that describes the host system
// running libpod/podman.
type Info struct {
	Host       *HostInfo      `json:"host"`
	Store      *StoreInfo     `json:"store"`
	Registries map[string]any `json:"registries"`
	Plugins    Plugins        `json:"plugins"`
	Version    Version        `json:"version"`
}

// Version is an output struct for API.
type Version struct {
	APIVersion string
	Version    string
	GoVersion  string
	GitCommit  string
	BuiltTime  string
	Built      int64
	OsArch     string
	Os         string
}

// SecurityInfo describes the libpod host.
type SecurityInfo struct {
	AppArmorEnabled     bool   `json:"apparmorEnabled"`
	DefaultCapabilities string `json:"capabilities"`
	Rootless            bool   `json:"rootless"`
	SECCOMPEnabled      bool   `json:"seccompEnabled"`
	SECCOMPProfilePath  string `json:"seccompProfilePath"`
	SELinuxEnabled      bool   `json:"selinuxEnabled"`
}

// HostInfo describes the libpod host.
type HostInfo struct {
	Arch              string           `json:"arch"`
	BuildahVersion    string           `json:"buildahVersion"`
	CgroupManager     string           `json:"cgroupManager"`
	CgroupsVersion    string           `json:"cgroupVersion"`
	CgroupControllers []string         `json:"cgroupControllers"`
	Conmon            *ConmonInfo      `json:"conmon"`
	CPUs              int              `json:"cpus"`
	CPUUtilization    *CPUUsage        `json:"cpuUtilization"`
	Distribution      DistributionInfo `json:"distribution"`
	EventLogger       string           `json:"eventLogger"`
	Hostname          string           `json:"hostname"`
	IDMappings        IDMappings       `json:"idMappings,omitempty"`
	Kernel            string           `json:"kernel"`
	LogDriver         string           `json:"logDriver"`
	MemFree           int64            `json:"memFree"`
	MemTotal          int64            `json:"memTotal"`
	NetworkBackend    string           `json:"networkBackend"`
	OCIRuntime        *OCIRuntimeInfo  `json:"ociRuntime"`
	OS                string           `json:"os"`
	// RemoteSocket returns the UNIX domain socket the Podman service is listening on
	RemoteSocket *RemoteSocket  `json:"remoteSocket,omitempty"`
	RuntimeInfo  map[string]any `json:"runtimeInfo,omitempty"`
	// ServiceIsRemote is true when the podman/libpod service is remote to the client
	ServiceIsRemote bool         `json:"serviceIsRemote"`
	Security        SecurityInfo `json:"security"`
	Slirp4NetNS     SlirpInfo    `json:"slirp4netns,omitempty"`
	SwapFree        int64        `json:"swapFree"`
	SwapTotal       int64        `json:"swapTotal"`
	Uptime          string       `json:"uptime"`
	Linkmode        string       `json:"linkmode"`
}

// RemoteSocket describes information about the API socket.
type RemoteSocket struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists,omitempty"`
}

// SlirpInfo describes the slirp executable that is being used.
type SlirpInfo struct {
	Executable string `json:"executable"`
	Package    string `json:"package"`
	Version    string `json:"version"`
}

// IDMappings describe the GID and UID mappings.
type IDMappings struct {
	GIDMap []idtools.IDMap `json:"gidmap"`
	UIDMap []idtools.IDMap `json:"uidmap"`
}

// DistributionInfo describes the host distribution for libpod.
type DistributionInfo struct {
	Distribution string `json:"distribution"`
	Variant      string `json:"variant,omitempty"`
	Version      string `json:"version"`
	Codename     string `json:"codename,omitempty"`
}

// ConmonInfo describes the conmon executable being used.
type ConmonInfo struct {
	Package string `json:"package"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// OCIRuntimeInfo describes the runtime (crun or runc) being
// used with podman.
type OCIRuntimeInfo struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// StoreInfo describes the container storage and its
// attributes.
type StoreInfo struct {
	ConfigFile      string         `json:"configFile"`
	ContainerStore  ContainerStore `json:"containerStore"`
	GraphDriverName string         `json:"graphDriverName"`
	GraphOptions    map[string]any `json:"graphOptions"`
	GraphRoot       string         `json:"graphRoot"`
	// GraphRootAllocated is how much space the graphroot has in bytes
	GraphRootAllocated uint64 `json:"graphRootAllocated"`
	// GraphRootUsed is how much of graphroot is used in bytes
	GraphRootUsed   uint64            `json:"graphRootUsed"`
	GraphStatus     map[string]string `json:"graphStatus"`
	ImageCopyTmpDir string            `json:"imageCopyTmpDir"`
	ImageStore      ImageStore        `json:"imageStore"`
	RunRoot         string            `json:"runRoot"`
	VolumePath      string            `json:"volumePath"`
}

// ImageStore describes the image store.  Right now only the number
// of images present.
type ImageStore struct {
	Number int `json:"number"`
}

// ContainerStore describes the quantity of containers in the
// store by status.
type ContainerStore struct {
	Number  int `json:"number"`
	Paused  int `json:"paused"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
}

type Plugins struct {
	Volume  []string `json:"volume"`
	Network []string `json:"network"`
	Log     []string `json:"log"`
	// Authorization is provided for compatibility, will always be nil as Podman has no daemon
	Authorization []string `json:"authorization"`
}

type CPUUsage struct {
	UserPercent   float64 `json:"userPercent"`
	SystemPercent float64 `json:"systemPercent"`
	IdlePercent   float64 `json:"idlePercent"`
}
