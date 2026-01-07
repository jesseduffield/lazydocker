package commands

import "time"

// ContainerSummary provides runtime-agnostic container information.
// This replaces Docker's container.Summary type.
type ContainerSummary struct {
	ID         string
	Names      []string
	Image      string
	ImageID    string
	Command    string
	Created    int64
	State      string
	Status     string
	Ports      []PortMapping
	Labels     map[string]string
	SizeRw     int64
	SizeRootFs int64
	// Podman-specific fields
	Pod     string
	PodName string
}

// PortMapping represents a container port mapping.
type PortMapping struct {
	IP          string
	PrivatePort uint16
	PublicPort  uint16
	Type        string
}

// ContainerDetails provides full container inspection data.
// This replaces Docker's container.InspectResponse type.
type ContainerDetails struct {
	ID              string
	Name            string
	Created         time.Time
	Path            string
	Args            []string
	State           *ContainerState
	Image           string
	ImageID         string
	ResolvConfPath  string
	HostnamePath    string
	HostsPath       string
	LogPath         string
	RestartCount    int
	Driver          string
	Platform        string
	MountLabel      string
	ProcessLabel    string
	AppArmorProfile string
	Config          *ContainerConfig
	NetworkSettings *NetworkSettings
	Mounts          []Mount
}

// ContainerState represents the state of a container.
type ContainerState struct {
	Status     string
	Running    bool
	Paused     bool
	Restarting bool
	OOMKilled  bool
	Dead       bool
	Pid        int
	ExitCode   int
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
	Health     *HealthState
}

// HealthState represents the health check state of a container.
type HealthState struct {
	Status        string
	FailingStreak int
	Log           []HealthLog
}

// HealthLog represents a health check log entry.
type HealthLog struct {
	Start    time.Time
	End      time.Time
	ExitCode int
	Output   string
}

// ContainerConfig represents container configuration.
type ContainerConfig struct {
	Hostname        string
	Domainname      string
	User            string
	AttachStdin     bool
	AttachStdout    bool
	AttachStderr    bool
	ExposedPorts    map[string]struct{}
	Tty             bool
	OpenStdin       bool
	StdinOnce       bool
	Env             []string
	Cmd             []string
	Image           string
	Volumes         map[string]struct{}
	WorkingDir      string
	Entrypoint      []string
	OnBuild         []string
	Labels          map[string]string
	StopSignal      string
	StopTimeout     *int
	Shell           []string
}

// NetworkSettings represents container network settings.
type NetworkSettings struct {
	Bridge                 string
	SandboxID              string
	HairpinMode            bool
	LinkLocalIPv6Address   string
	LinkLocalIPv6PrefixLen int
	Ports                  map[string][]PortBinding
	SandboxKey             string
	Networks               map[string]*EndpointSettings
}

// PortBinding represents a port binding.
type PortBinding struct {
	HostIP   string
	HostPort string
}

// EndpointSettings represents network endpoint settings.
type EndpointSettings struct {
	IPAMConfig          *EndpointIPAMConfig
	Links               []string
	Aliases             []string
	NetworkID           string
	EndpointID          string
	Gateway             string
	IPAddress           string
	IPPrefixLen         int
	IPv6Gateway         string
	GlobalIPv6Address   string
	GlobalIPv6PrefixLen int
	MacAddress          string
	DriverOpts          map[string]string
}

// EndpointIPAMConfig represents IPAM configuration for an endpoint.
type EndpointIPAMConfig struct {
	IPv4Address  string
	IPv6Address  string
	LinkLocalIPs []string
}

// Mount represents a container mount.
type Mount struct {
	Type        string
	Name        string
	Source      string
	Destination string
	Driver      string
	Mode        string
	RW          bool
	Propagation string
}

// ContainerStatsEntry provides runtime-agnostic container stats.
// This is used for streaming stats from the runtime.
type ContainerStatsEntry struct {
	Read        time.Time
	PreRead     time.Time
	CPUStats    CPUStats
	PreCPUStats CPUStats
	MemoryStats MemoryStats
	PidsStats   PidsStats
	Networks    map[string]NetworkStats
	BlkioStats  BlkioStats
	Name        string
	ID          string
}

// CPUStats represents CPU usage statistics.
type CPUStats struct {
	CPUUsage       CPUUsage
	SystemCPUUsage int64
	OnlineCpus     int
	ThrottlingData ThrottlingData
}

// CPUUsage represents CPU usage breakdown.
type CPUUsage struct {
	TotalUsage        int64
	PercpuUsage       []int64
	UsageInKernelmode int64
	UsageInUsermode   int64
}

// ThrottlingData represents CPU throttling data.
type ThrottlingData struct {
	Periods          int
	ThrottledPeriods int
	ThrottledTime    int64
}

// MemoryStats represents memory usage statistics.
type MemoryStats struct {
	Usage    int64
	MaxUsage int64
	Limit    int64
	Stats    MemoryStatsDetails
}

// MemoryStatsDetails provides detailed memory statistics.
type MemoryStatsDetails struct {
	ActiveAnon              int64
	ActiveFile              int64
	Cache                   int64
	Dirty                   int64
	HierarchicalMemoryLimit int64
	HierarchicalMemswLimit  int64
	InactiveAnon            int64
	InactiveFile            int64
	MappedFile              int64
	Pgfault                 int64
	Pgmajfault              int64
	Pgpgin                  int64
	Pgpgout                 int64
	Rss                     int64
	RssHuge                 int64
	TotalActiveAnon         int64
	TotalActiveFile         int64
	TotalCache              int64
	TotalDirty              int64
	TotalInactiveAnon       int64
	TotalInactiveFile       int64
	TotalMappedFile         int64
	TotalPgfault            int64
	TotalPgmajfault         int64
	TotalPgpgin             int64
	TotalPgpgout            int64
	TotalRss                int64
	TotalRssHuge            int64
	TotalUnevictable        int64
	TotalWriteback          int64
	Unevictable             int64
	Writeback               int64
}

// PidsStats represents PID statistics.
type PidsStats struct {
	Current int
	Limit   int64
}

// NetworkStats represents network I/O statistics.
type NetworkStats struct {
	RxBytes   int64
	RxPackets int64
	RxErrors  int64
	RxDropped int64
	TxBytes   int64
	TxPackets int64
	TxErrors  int64
	TxDropped int64
}

// BlkioStats represents block I/O statistics.
type BlkioStats struct {
	IoServiceBytesRecursive []BlkioStatEntry
	IoServicedRecursive     []BlkioStatEntry
	IoQueueRecursive        []BlkioStatEntry
	IoServiceTimeRecursive  []BlkioStatEntry
	IoWaitTimeRecursive     []BlkioStatEntry
	IoMergedRecursive       []BlkioStatEntry
	IoTimeRecursive         []BlkioStatEntry
	SectorsRecursive        []BlkioStatEntry
}

// BlkioStatEntry represents a single block I/O stat entry.
type BlkioStatEntry struct {
	Major int64
	Minor int64
	Op    string
	Value int64
}

// ImageSummary provides runtime-agnostic image information.
// This replaces Docker's image.Summary type.
type ImageSummary struct {
	ID          string
	ParentID    string
	RepoTags    []string
	RepoDigests []string
	Created     int64
	Size        int64
	SharedSize  int64
	VirtualSize int64
	Labels      map[string]string
	Containers  int64
}

// ImageDetails provides full image inspection data.
type ImageDetails struct {
	ID              string
	RepoTags        []string
	RepoDigests     []string
	Parent          string
	Comment         string
	Created         time.Time
	Container       string
	DockerVersion   string
	Author          string
	Config          *ContainerConfig
	Architecture    string
	Os              string
	OsVersion       string
	Size            int64
	VirtualSize     int64
	RootFS          RootFS
	Metadata        ImageMetadata
}

// RootFS represents the root filesystem of an image.
type RootFS struct {
	Type   string
	Layers []string
}

// ImageMetadata represents image metadata.
type ImageMetadata struct {
	LastTagTime time.Time
}

// ImageHistoryEntry represents a layer in image history.
type ImageHistoryEntry struct {
	ID        string
	Created   int64
	CreatedBy string
	Tags      []string
	Size      int64
	Comment   string
}

// VolumeSummary provides runtime-agnostic volume information.
// This replaces Docker's volume.Volume type.
type VolumeSummary struct {
	Name       string
	Driver     string
	Mountpoint string
	CreatedAt  time.Time
	Status     map[string]interface{}
	Labels     map[string]string
	Scope      string
	Options    map[string]string
	UsageData  *VolumeUsageData
}

// VolumeUsageData represents volume usage statistics.
type VolumeUsageData struct {
	Size     int64
	RefCount int64
}

// NetworkSummary provides runtime-agnostic network information.
// This replaces Docker's network.Inspect type.
type NetworkSummary struct {
	Name       string
	ID         string
	Created    time.Time
	Scope      string
	Driver     string
	EnableIPv6 bool
	IPAM       IPAM
	Internal   bool
	Attachable bool
	Ingress    bool
	Containers map[string]EndpointResource
	Options    map[string]string
	Labels     map[string]string
}

// IPAM represents IP Address Management configuration.
type IPAM struct {
	Driver  string
	Options map[string]string
	Config  []IPAMConfig
}

// IPAMConfig represents IPAM configuration for a network.
type IPAMConfig struct {
	Subnet     string
	IPRange    string
	Gateway    string
	AuxAddress map[string]string
}

// EndpointResource represents a container endpoint on a network.
type EndpointResource struct {
	Name        string
	EndpointID  string
	MacAddress  string
	IPv4Address string
	IPv6Address string
}

// TopResponse represents the response from ContainerTop.
type TopResponse struct {
	Titles    []string
	Processes [][]string
}

// Event represents a container runtime event (container start/stop, image pull, etc.)
type Event struct {
	Type   string // "container", "image", "volume", "network"
	Action string // "start", "stop", "create", "delete", etc.
	Actor  EventActor
	Time   int64
}

// EventActor represents the actor (container, image, etc.) that triggered an event.
type EventActor struct {
	ID         string
	Attributes map[string]string
}
