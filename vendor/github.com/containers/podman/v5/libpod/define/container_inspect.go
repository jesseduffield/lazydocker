package define

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/containers/podman/v5/pkg/signal"
	"go.podman.io/image/v5/manifest"
)

type InspectIDMappings struct {
	UIDMap []string `json:"UidMap"`
	GIDMap []string `json:"GidMap"`
}

// InspectContainerConfig holds further data about how a container was initially
// configured.
type InspectContainerConfig struct {
	// Container hostname
	Hostname string `json:"Hostname"`
	// Container domain name - unused at present
	DomainName string `json:"Domainname"`
	// User the container was launched with
	User string `json:"User"`
	// Unused, at present
	AttachStdin bool `json:"AttachStdin"`
	// Unused, at present
	AttachStdout bool `json:"AttachStdout"`
	// Unused, at present
	AttachStderr bool `json:"AttachStderr"`
	// Whether the container creates a TTY
	Tty bool `json:"Tty"`
	// Whether the container leaves STDIN open
	OpenStdin bool `json:"OpenStdin"`
	// Whether STDIN is only left open once.
	// Presently not supported by Podman, unused.
	StdinOnce bool `json:"StdinOnce"`
	// Container environment variables
	Env []string `json:"Env"`
	// Container command
	Cmd []string `json:"Cmd"`
	// Container image
	Image string `json:"Image"`
	// Unused, at present. I've never seen this field populated.
	Volumes map[string]struct{} `json:"Volumes"`
	// Container working directory
	WorkingDir string `json:"WorkingDir"`
	// Container entrypoint
	Entrypoint []string `json:"Entrypoint"`
	// On-build arguments - presently unused. More of Buildah's domain.
	OnBuild *string `json:"OnBuild"`
	// Container labels
	Labels map[string]string `json:"Labels"`
	// Container annotations
	Annotations map[string]string `json:"Annotations"`
	// Container stop signal
	StopSignal string `json:"StopSignal"`
	// Configured startup healthcheck for the container
	StartupHealthCheck *StartupHealthCheck `json:"StartupHealthCheck,omitempty"`
	// Configured healthcheck for the container
	Healthcheck *manifest.Schema2HealthConfig `json:"Healthcheck,omitempty"`
	// HealthcheckOnFailureAction defines an action to take once the container turns unhealthy.
	HealthcheckOnFailureAction string `json:"HealthcheckOnFailureAction,omitempty"`
	// HealthLogDestination defines the destination where the log is stored
	HealthLogDestination string `json:"HealthLogDestination,omitempty"`
	// HealthMaxLogCount is maximum number of attempts in the HealthCheck log file.
	// ('0' value means an infinite number of attempts in the log file)
	HealthMaxLogCount uint `json:"HealthcheckMaxLogCount,omitempty"`
	// HealthMaxLogSize is the maximum length in characters of stored HealthCheck log
	// ("0" value means an infinite log length)
	HealthMaxLogSize uint `json:"HealthcheckMaxLogSize,omitempty"`
	// CreateCommand is the full command plus arguments of the process the
	// container has been created with.
	CreateCommand []string `json:"CreateCommand,omitempty"`
	// Timezone is the timezone inside the container.
	// Local means it has the same timezone as the host machine
	Timezone string `json:"Timezone,omitempty"`
	// SystemdMode is whether the container is running in systemd mode. In
	// systemd mode, the container configuration is customized to optimize
	// running systemd in the container.
	SystemdMode bool `json:"SystemdMode,omitempty"`
	// Umask is the umask inside the container.
	Umask string `json:"Umask,omitempty"`
	// Secrets are the secrets mounted in the container
	Secrets []*InspectSecret `json:"Secrets,omitempty"`
	// Timeout is time before container is killed by conmon
	Timeout uint `json:"Timeout"`
	// StopTimeout is time before container is stopped when calling stop
	StopTimeout uint `json:"StopTimeout"`
	// Passwd determines whether or not podman can add entries to /etc/passwd and /etc/group
	Passwd *bool `json:"Passwd,omitempty"`
	// ChrootDirs is an additional set of directories that need to be
	// treated as root directories. Standard bind mounts will be mounted
	// into paths relative to these directories.
	ChrootDirs []string `json:"ChrootDirs,omitempty"`
	// SdNotifyMode is the sd-notify mode of the container.
	SdNotifyMode string `json:"sdNotifyMode,omitempty"`
	// SdNotifySocket is the NOTIFY_SOCKET in use by/configured for the container.
	SdNotifySocket string `json:"sdNotifySocket,omitempty"`
	// ExposedPorts includes ports the container has exposed.
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`

	// V4PodmanCompatMarshal indicates that the json marshaller should
	// use the old v4 inspect format to keep API compatibility.
	V4PodmanCompatMarshal bool `json:"-"`
}

// UnmarshalJSON allow compatibility with podman V4 API
func (insp *InspectContainerConfig) UnmarshalJSON(data []byte) error {
	type Alias InspectContainerConfig
	aux := &struct {
		Entrypoint any `json:"Entrypoint"`
		StopSignal any `json:"StopSignal"`
		*Alias
	}{
		Alias: (*Alias)(insp),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	switch entrypoint := aux.Entrypoint.(type) {
	case string:
		insp.Entrypoint = strings.Split(entrypoint, " ")
	case []string:
		insp.Entrypoint = entrypoint
	case []any:
		insp.Entrypoint = []string{}
		for _, entry := range entrypoint {
			if str, ok := entry.(string); ok {
				insp.Entrypoint = append(insp.Entrypoint, str)
			}
		}
	case nil:
		insp.Entrypoint = []string{}
	default:
		return fmt.Errorf("cannot unmarshal Config.Entrypoint of type  %T", entrypoint)
	}

	switch stopsignal := aux.StopSignal.(type) {
	case string:
		insp.StopSignal = stopsignal
	case float64:
		insp.StopSignal = signal.ToDockerFormat(uint(stopsignal))
	case nil:
		break
	default:
		return fmt.Errorf("cannot unmarshal Config.StopSignal of type  %T", stopsignal)
	}
	return nil
}

func (insp *InspectContainerConfig) MarshalJSON() ([]byte, error) {
	// the alias is needed otherwise MarshalJSON will
	type Alias InspectContainerConfig
	conf := (*Alias)(insp)
	if !insp.V4PodmanCompatMarshal {
		return json.Marshal(conf)
	}

	type v4InspectContainerConfig struct {
		Entrypoint string `json:"Entrypoint"`
		StopSignal uint   `json:"StopSignal"`
		*Alias
	}
	stopSignal, _ := signal.ParseSignal(insp.StopSignal)
	newConf := &v4InspectContainerConfig{
		Entrypoint: strings.Join(insp.Entrypoint, " "),
		StopSignal: uint(stopSignal),
		Alias:      conf,
	}
	return json.Marshal(newConf)
}

// InspectRestartPolicy holds information about the container's restart policy.
type InspectRestartPolicy struct {
	// Name contains the container's restart policy.
	// Allowable values are "no" or "" (take no action),
	// "on-failure" (restart on non-zero exit code, with an optional max
	// retry count), and "always" (always restart on container stop, unless
	// explicitly requested by API).
	// Note that this is NOT actually a name of any sort - the poor naming
	// is for Docker compatibility.
	Name string `json:"Name"`
	// MaximumRetryCount is the maximum number of retries allowed if the
	// "on-failure" restart policy is in use. Not used if "on-failure" is
	// not set.
	MaximumRetryCount uint `json:"MaximumRetryCount"`
}

// InspectLogConfig holds information about a container's configured log driver
type InspectLogConfig struct {
	Type   string            `json:"Type"`
	Config map[string]string `json:"Config"`
	// Path specifies a path to the log file
	Path string `json:"Path"`
	// Tag specifies a custom log tag for the container
	Tag string `json:"Tag"`
	// Size specifies a maximum size of the container log
	Size string `json:"Size"`
}

// InspectBlkioWeightDevice holds information about the relative weight
// of an individual device node. Weights are used in the I/O scheduler to give
// relative priority to some accesses.
type InspectBlkioWeightDevice struct {
	// Path is the path to the device this applies to.
	Path string `json:"Path"`
	// Weight is the relative weight the scheduler will use when scheduling
	// I/O.
	Weight uint16 `json:"Weight"`
}

// InspectBlkioThrottleDevice holds information about a speed cap for a device
// node. This cap applies to a specific operation (read, write, etc) on the given
// node.
type InspectBlkioThrottleDevice struct {
	// Path is the path to the device this applies to.
	Path string `json:"Path"`
	// Rate is the maximum rate. It is in either bytes per second or iops
	// per second, determined by where it is used - documentation will
	// indicate which is appropriate.
	Rate uint64 `json:"Rate"`
}

// InspectUlimit is a ulimit that will be applied to the container.
type InspectUlimit struct {
	// Name is the name (type) of the ulimit.
	Name string `json:"Name"`
	// Soft is the soft limit that will be applied.
	Soft int64 `json:"Soft"`
	// Hard is the hard limit that will be applied.
	Hard int64 `json:"Hard"`
}

// InspectDevice is a single device that will be mounted into the container.
type InspectDevice struct {
	// PathOnHost is the path of the device on the host.
	PathOnHost string `json:"PathOnHost"`
	// PathInContainer is the path of the device within the container.
	PathInContainer string `json:"PathInContainer"`
	// CgroupPermissions is the permissions of the mounted device.
	// Presently not populated.
	// TODO.
	CgroupPermissions string `json:"CgroupPermissions"`
}

// InspectHostPort provides information on a port on the host that a container's
// port is bound to.
type InspectHostPort struct {
	// IP on the host we are bound to. "" if not specified (binding to all
	// IPs).
	HostIP string `json:"HostIp"`
	// Port on the host we are bound to. No special formatting - just an
	// integer stuffed into a string.
	HostPort string `json:"HostPort"`
}

// InspectMount provides a record of a single mount in a container. It contains
// fields for both named and normal volumes. Only user-specified volumes will be
// included, and tmpfs volumes are not included even if the user specified them.
type InspectMount struct {
	// Whether the mount is a volume or bind mount. Allowed values are
	// "volume" and "bind".
	Type string `json:"Type"`
	// The name of the volume. Empty for bind mounts.
	Name string `json:"Name,omitempty"`
	// The source directory for the volume.
	Source string `json:"Source"`
	// The destination directory for the volume. Specified as a path within
	// the container, as it would be passed into the OCI runtime.
	Destination string `json:"Destination"`
	// The driver used for the named volume. Empty for bind mounts.
	Driver string `json:"Driver"`
	// Contains SELinux :z/:Z mount options. Unclear what, if anything, else
	// goes in here.
	Mode string `json:"Mode"`
	// All remaining mount options. Additional data, not present in the
	// original output.
	Options []string `json:"Options"`
	// Whether the volume is read-write
	RW bool `json:"RW"`
	// Mount propagation for the mount. Can be empty if not specified, but
	// is always printed - no omitempty.
	Propagation string `json:"Propagation"`
	// SubPath object from the volume. Specified as a path within
	// the source volume to be mounted at the Destination.
	SubPath string `json:"SubPath,omitempty"`
}

// InspectContainerState provides a detailed record of a container's current
// state. It is returned as part of InspectContainerData.
// As with InspectContainerData, many portions of this struct are matched to
// Docker, but here we see more fields that are unused (nonsensical in the
// context of Libpod).
type InspectContainerState struct {
	OciVersion     string              `json:"OciVersion"`
	Status         string              `json:"Status"`
	Running        bool                `json:"Running"`
	Paused         bool                `json:"Paused"`
	Restarting     bool                `json:"Restarting"` // TODO
	OOMKilled      bool                `json:"OOMKilled"`
	Dead           bool                `json:"Dead"`
	Pid            int                 `json:"Pid"`
	ConmonPid      int                 `json:"ConmonPid,omitempty"`
	ExitCode       int32               `json:"ExitCode"`
	Error          string              `json:"Error"` // TODO
	StartedAt      time.Time           `json:"StartedAt"`
	FinishedAt     time.Time           `json:"FinishedAt"`
	Health         *HealthCheckResults `json:"Health,omitempty"`
	Checkpointed   bool                `json:"Checkpointed,omitempty"`
	CgroupPath     string              `json:"CgroupPath,omitempty"`
	CheckpointedAt time.Time           `json:"CheckpointedAt"`
	RestoredAt     time.Time           `json:"RestoredAt"`
	CheckpointLog  string              `json:"CheckpointLog,omitempty"`
	CheckpointPath string              `json:"CheckpointPath,omitempty"`
	RestoreLog     string              `json:"RestoreLog,omitempty"`
	Restored       bool                `json:"Restored,omitempty"`
	StoppedByUser  bool                `json:"StoppedByUser,omitempty"`
}

// Healthcheck returns the HealthCheckResults. This is used for old podman compat
// to make the "Healthcheck" key available in the go template.
func (s *InspectContainerState) Healthcheck() *HealthCheckResults {
	return s.Health
}

// HealthCheckResults describes the results/logs from a healthcheck
type HealthCheckResults struct {
	// Status starting, healthy or unhealthy
	Status string `json:"Status"`
	// FailingStreak is the number of consecutive failed healthchecks
	FailingStreak int `json:"FailingStreak"`
	// Log describes healthcheck attempts and results
	Log []HealthCheckLog `json:"Log"`
}

// HealthCheckLog describes the results of a single healthcheck
type HealthCheckLog struct {
	// Start time as string
	Start string `json:"Start"`
	// End time as a string
	End string `json:"End"`
	// Exitcode is 0 or 1
	ExitCode int `json:"ExitCode"`
	// Output is the stdout/stderr from the healthcheck command
	Output string `json:"Output"`
}

// InspectContainerHostConfig holds information used when the container was
// created.
// It's very much a Docker-specific struct, retained (mostly) as-is for
// compatibility. We fill individual fields as best as we can, inferring as much
// as possible from the spec and container config.
// Some things cannot be inferred. These will be populated by spec annotations
// (if available).
type InspectContainerHostConfig struct {
	// Binds contains an array of user-added mounts.
	// Both volume mounts and named volumes are included.
	// Tmpfs mounts are NOT included.
	// In 'docker inspect' this is separated into 'Binds' and 'Mounts' based
	// on how a mount was added. We do not make this distinction and do not
	// include a Mounts field in inspect.
	// Format: <src>:<destination>[:<comma-separated options>]
	Binds []string `json:"Binds"`
	// CgroupManager is the cgroup manager used by the container.
	// At present, allowed values are either "cgroupfs" or "systemd".
	CgroupManager string `json:"CgroupManager,omitempty"`
	// CgroupMode is the configuration of the container's cgroup namespace.
	// Populated as follows:
	// private - a cgroup namespace has been created
	// host - No cgroup namespace created
	// container:<id> - Using another container's cgroup namespace
	// ns:<path> - A path to a cgroup namespace has been specified
	CgroupMode string `json:"CgroupMode"`
	// ContainerIDFile is a file created during container creation to hold
	// the ID of the created container.
	// This is not handled within libpod and is stored in an annotation.
	ContainerIDFile string `json:"ContainerIDFile"`
	// LogConfig contains information on the container's logging backend
	LogConfig *InspectLogConfig `json:"LogConfig"`
	// NetworkMode is the configuration of the container's network
	// namespace.
	// Populated as follows:
	// default - A network namespace is being created and configured via CNI
	// none - A network namespace is being created, not configured via CNI
	// host - No network namespace created
	// container:<id> - Using another container's network namespace
	// ns:<path> - A path to a network namespace has been specified
	NetworkMode string `json:"NetworkMode"`
	// PortBindings contains the container's port bindings.
	// It is formatted as map[string][]InspectHostPort.
	// The string key here is formatted as <integer port number>/<protocol>
	// and represents the container port. A single container port may be
	// bound to multiple host ports (on different IPs).
	PortBindings map[string][]InspectHostPort `json:"PortBindings"`
	// RestartPolicy contains the container's restart policy.
	RestartPolicy *InspectRestartPolicy `json:"RestartPolicy"`
	// AutoRemove is whether the container will be automatically removed on
	// exiting.
	// It is not handled directly within libpod and is stored in an
	// annotation.
	AutoRemove bool `json:"AutoRemove"`
	// AutoRemoveImage is whether the container's image will be
	// automatically removed on exiting.
	// It is not handled directly within libpod and is stored in an
	// annotation.
	AutoRemoveImage bool `json:"AutoRemoveImage"`
	// Annotations are provided to the runtime when the container is
	// started.
	Annotations map[string]string `json:"Annotations"`
	// VolumeDriver is presently unused and is retained for Docker
	// compatibility.
	VolumeDriver string `json:"VolumeDriver"`
	// VolumesFrom is a list of containers which this container uses volumes
	// from. This is not handled directly within libpod and is stored in an
	// annotation.
	// It is formatted as an array of container names and IDs.
	VolumesFrom []string `json:"VolumesFrom"`
	// CapAdd is a list of capabilities added to the container.
	// It is not directly stored by Libpod, and instead computed from the
	// capabilities listed in the container's spec, compared against a set
	// of default capabilities.
	CapAdd []string `json:"CapAdd"`
	// CapDrop is a list of capabilities removed from the container.
	// It is not directly stored by libpod, and instead computed from the
	// capabilities listed in the container's spec, compared against a set
	// of default capabilities.
	CapDrop []string `json:"CapDrop"`
	// Dns is a list of DNS nameservers that will be added to the
	// container's resolv.conf
	Dns []string `json:"Dns"`
	// DnsOptions is a list of DNS options that will be set in the
	// container's resolv.conf
	DnsOptions []string `json:"DnsOptions"`
	// DnsSearch is a list of DNS search domains that will be set in the
	// container's resolv.conf
	DnsSearch []string `json:"DnsSearch"`
	// ExtraHosts contains hosts that will be added to the container's
	// /etc/hosts.
	ExtraHosts []string `json:"ExtraHosts"`
	// HostsFile is the base file to create the `/etc/hosts` file inside the container.
	HostsFile string `json:"HostsFile"`
	// GroupAdd contains groups that the user inside the container will be
	// added to.
	GroupAdd []string `json:"GroupAdd"`
	// IpcMode represents the configuration of the container's IPC
	// namespace.
	// Populated as follows:
	// "" (empty string) - Default, an IPC namespace will be created
	// host - No IPC namespace created
	// container:<id> - Using another container's IPC namespace
	// ns:<path> - A path to an IPC namespace has been specified
	IpcMode string `json:"IpcMode"`
	// Cgroup contains the container's cgroup. It is presently not
	// populated.
	// TODO.
	Cgroup string `json:"Cgroup"`
	// Cgroups contains the container's Cgroup mode.
	// Allowed values are "default" (container is creating Cgroups) and
	// "disabled" (container is not creating Cgroups).
	// This is Libpod-specific and not included in `docker inspect`.
	Cgroups string `json:"Cgroups"`
	// Links is unused, and provided purely for Docker compatibility.
	Links []string `json:"Links"`
	// OOMScoreAdj is an adjustment that will be made to the container's OOM
	// score.
	OomScoreAdj int `json:"OomScoreAdj"`
	// PidMode represents the configuration of the container's PID
	// namespace.
	// Populated as follows:
	// "" (empty string) - Default, a PID namespace will be created
	// host - No PID namespace created
	// container:<id> - Using another container's PID namespace
	// ns:<path> - A path to a PID namespace has been specified
	PidMode string `json:"PidMode"`
	// Privileged indicates whether the container is running with elevated
	// privileges.
	// This has a very specific meaning in the Docker sense, so it's very
	// difficult to decode from the spec and config, and so is stored as an
	// annotation.
	Privileged bool `json:"Privileged"`
	// PublishAllPorts indicates whether image ports are being published.
	// This is not directly stored in libpod and is saved as an annotation.
	PublishAllPorts bool `json:"PublishAllPorts"`
	// ReadonlyRootfs is whether the container will be mounted read-only.
	ReadonlyRootfs bool `json:"ReadonlyRootfs"`
	// SecurityOpt is a list of security-related options that are set in the
	// container.
	SecurityOpt []string `json:"SecurityOpt"`
	// Tmpfs is a list of tmpfs filesystems that will be mounted into the
	// container.
	// It is a map of destination path to options for the mount.
	Tmpfs map[string]string `json:"Tmpfs"`
	// UTSMode represents the configuration of the container's UID
	// namespace.
	// Populated as follows:
	// "" (empty string) - Default, a UTS namespace will be created
	// host - no UTS namespace created
	// container:<id> - Using another container's UTS namespace
	// ns:<path> - A path to a UTS namespace has been specified
	UTSMode string `json:"UTSMode"`
	// UsernsMode represents the configuration of the container's user
	// namespace.
	// When running rootless, a user namespace is created outside of libpod
	// to allow some privileged operations. This will not be reflected here.
	// Populated as follows:
	// "" (empty string) - No user namespace will be created
	// private - The container will be run in a user namespace
	// container:<id> - Using another container's user namespace
	// ns:<path> - A path to a user namespace has been specified
	// TODO Rootless has an additional 'keep-id' option, presently not
	// reflected here.
	UsernsMode string `json:"UsernsMode"`
	// IDMappings is the UIDMapping and GIDMapping used within the container
	IDMappings *InspectIDMappings `json:"IDMappings,omitempty"`
	// ShmSize is the size of the container's SHM device.

	ShmSize int64 `json:"ShmSize"`
	// Runtime is provided purely for Docker compatibility.
	// It is set unconditionally to "oci" as Podman does not presently
	// support non-OCI runtimes.
	Runtime string `json:"Runtime"`
	// ConsoleSize is an array of 2 integers showing the size of the
	// container's console.
	// It is only set if the container is creating a terminal.
	// TODO.
	ConsoleSize []uint `json:"ConsoleSize"`
	// Isolation is presently unused and provided solely for Docker
	// compatibility.
	Isolation string `json:"Isolation"`
	// CpuShares indicates the CPU resources allocated to the container.
	// It is a relative weight in the scheduler for assigning CPU time
	// versus other Cgroups.
	CpuShares uint64 `json:"CpuShares"`
	// Memory indicates the memory resources allocated to the container.
	// This is the limit (in bytes) of RAM the container may use.
	Memory int64 `json:"Memory"`
	// NanoCpus indicates number of CPUs allocated to the container.
	// It is an integer where one full CPU is indicated by 1000000000 (one
	// billion).
	// Thus, 2.5 CPUs (fractional portions of CPUs are allowed) would be
	// 2500000000 (2.5 billion).
	// In 'docker inspect' this is set exclusively of two further options in
	// the output (CpuPeriod and CpuQuota) which are both used to implement
	// this functionality.
	// We can't distinguish here, so if CpuQuota is set to the default of
	// 100000, we will set both CpuQuota, CpuPeriod, and NanoCpus. If
	// CpuQuota is not the default, we will not set NanoCpus.
	NanoCpus int64 `json:"NanoCpus"`
	// CgroupParent is the Cgroup parent of the container.
	// Only set if not default.
	CgroupParent string `json:"CgroupParent"`
	// BlkioWeight indicates the I/O resources allocated to the container.
	// It is a relative weight in the scheduler for assigning I/O time
	// versus other Cgroups.
	BlkioWeight uint16 `json:"BlkioWeight"`
	// BlkioWeightDevice is an array of I/O resource priorities for
	// individual device nodes.
	// Unfortunately, the spec only stores the device's Major/Minor numbers
	// and not the path, which is used here.
	// Fortunately, the kernel provides an interface for retrieving the path
	// of a given node by major:minor at /sys/dev/. However, the exact path
	// in use may not be what was used in the original CLI invocation -
	// though it is guaranteed that the device node will be the same, and
	// using the given path will be functionally identical.
	BlkioWeightDevice []InspectBlkioWeightDevice `json:"BlkioWeightDevice"`
	// BlkioDeviceReadBps is an array of I/O throttle parameters for
	// individual device nodes.
	// This specifically sets read rate cap in bytes per second for device
	// nodes.
	// As with BlkioWeightDevice, we pull the path from /sys/dev, and we
	// don't guarantee the path will be identical to the original (though
	// the node will be).
	BlkioDeviceReadBps []InspectBlkioThrottleDevice `json:"BlkioDeviceReadBps"`
	// BlkioDeviceWriteBps is an array of I/O throttle parameters for
	// individual device nodes.
	// this specifically sets write rate cap in bytes per second for device
	// nodes.
	// as with BlkioWeightDevice, we pull the path from /sys/dev, and we
	// don't guarantee the path will be identical to the original (though
	// the node will be).
	BlkioDeviceWriteBps []InspectBlkioThrottleDevice `json:"BlkioDeviceWriteBps"`
	// BlkioDeviceReadIOps is an array of I/O throttle parameters for
	// individual device nodes.
	// This specifically sets the read rate cap in iops per second for
	// device nodes.
	// As with BlkioWeightDevice, we pull the path from /sys/dev, and we
	// don't guarantee the path will be identical to the original (though
	// the node will be).
	BlkioDeviceReadIOps []InspectBlkioThrottleDevice `json:"BlkioDeviceReadIOps"`
	// BlkioDeviceWriteIOps is an array of I/O throttle parameters for
	// individual device nodes.
	// This specifically sets the write rate cap in iops per second for
	// device nodes.
	// As with BlkioWeightDevice, we pull the path from /sys/dev, and we
	// don't guarantee the path will be identical to the original (though
	// the node will be).
	BlkioDeviceWriteIOps []InspectBlkioThrottleDevice `json:"BlkioDeviceWriteIOps"`
	// CpuPeriod is the length of a CPU period in microseconds.
	// It relates directly to CpuQuota.
	CpuPeriod uint64 `json:"CpuPeriod"`
	// CpuPeriod is the amount of time (in microseconds) that a container
	// can use the CPU in every CpuPeriod.
	CpuQuota int64 `json:"CpuQuota"`
	// CpuRealtimePeriod is the length of time (in microseconds) of the CPU
	// realtime period. If set to 0, no time will be allocated to realtime
	// tasks.
	CpuRealtimePeriod uint64 `json:"CpuRealtimePeriod"`
	// CpuRealtimeRuntime is the length of time (in microseconds) allocated
	// for realtime tasks within every CpuRealtimePeriod.
	CpuRealtimeRuntime int64 `json:"CpuRealtimeRuntime"`
	// CpusetCpus is the set of CPUs that the container will execute on.
	// Formatted as `0-3` or `0,2`. Default (if unset) is all CPUs.
	CpusetCpus string `json:"CpusetCpus"`
	// CpusetMems is the set of memory nodes the container will use.
	// Formatted as `0-3` or `0,2`. Default (if unset) is all memory nodes.
	CpusetMems string `json:"CpusetMems"`
	// Devices is a list of device nodes that will be added to the
	// container.
	// These are stored in the OCI spec only as type, major, minor while we
	// display the host path. We convert this with /sys/dev, but we cannot
	// guarantee that the host path will be identical - only that the actual
	// device will be.
	Devices []InspectDevice `json:"Devices"`
	// DiskQuota is the maximum amount of disk space the container may use
	// (in bytes).
	// Presently not populated.
	// TODO.
	DiskQuota uint64 `json:"DiskQuota"`
	// KernelMemory is the maximum amount of memory the kernel will devote
	// to the container.
	KernelMemory int64 `json:"KernelMemory"`
	// MemoryReservation is the reservation (soft limit) of memory available
	// to the container. Soft limits are warnings only and can be exceeded.
	MemoryReservation int64 `json:"MemoryReservation"`
	// MemorySwap is the total limit for all memory available to the
	// container, including swap. 0 indicates that there is no limit to the
	// amount of memory available.
	MemorySwap int64 `json:"MemorySwap"`
	// MemorySwappiness is the willingness of the kernel to page container
	// memory to swap. It is an integer from 0 to 100, with low numbers
	// being more likely to be put into swap.
	// -1, the default, will not set swappiness and use the system defaults.
	MemorySwappiness int64 `json:"MemorySwappiness"`
	// OomKillDisable indicates whether the kernel OOM killer is disabled
	// for the container.
	OomKillDisable bool `json:"OomKillDisable"`
	// Init indicates whether the container has an init mounted into it.
	Init bool `json:"Init,omitempty"`
	// PidsLimit is the maximum number of PIDs that may be created within
	// the container. 0, the default, indicates no limit.
	PidsLimit int64 `json:"PidsLimit"`
	// Ulimits is a set of ulimits that will be set within the container.
	Ulimits []InspectUlimit `json:"Ulimits"`
	// CpuCount is Windows-only and not presently implemented.
	CpuCount uint64 `json:"CpuCount"`
	// CpuPercent is Windows-only and not presently implemented.
	CpuPercent uint64 `json:"CpuPercent"`
	// IOMaximumIOps is Windows-only and not presently implemented.
	IOMaximumIOps uint64 `json:"IOMaximumIOps"`
	// IOMaximumBandwidth is Windows-only and not presently implemented.
	IOMaximumBandwidth uint64 `json:"IOMaximumBandwidth"`
	// CgroupConf is the configuration for cgroup v2.
	CgroupConf map[string]string `json:"CgroupConf"`
	// IntelRdtClosID defines the Intel RDT CAT Class Of Service (COS) that
	// all processes of the container should run in.
	IntelRdtClosID string `json:"IntelRdtClosID,omitempty"`
}

// Address represents an IP address.
type Address struct {
	Addr         string
	PrefixLength int
}

// InspectBasicNetworkConfig holds basic configuration information (e.g. IP
// addresses, MAC address, subnet masks, etc) that are common for all networks
// (both additional and main).
type InspectBasicNetworkConfig struct {
	// EndpointID is unused, maintained exclusively for compatibility.
	EndpointID string `json:"EndpointID"`
	// Gateway is the IP address of the gateway this network will use.
	Gateway string `json:"Gateway"`
	// IPAddress is the IP address for this network.
	IPAddress string `json:"IPAddress"`
	// IPPrefixLen is the length of the subnet mask of this network.
	IPPrefixLen int `json:"IPPrefixLen"`
	// SecondaryIPAddresses is a list of extra IP Addresses that the
	// container has been assigned in this network.
	SecondaryIPAddresses []Address `json:"SecondaryIPAddresses,omitempty"`
	// IPv6Gateway is the IPv6 gateway this network will use.
	IPv6Gateway string `json:"IPv6Gateway"`
	// GlobalIPv6Address is the global-scope IPv6 Address for this network.
	GlobalIPv6Address string `json:"GlobalIPv6Address"`
	// GlobalIPv6PrefixLen is the length of the subnet mask of this network.
	GlobalIPv6PrefixLen int `json:"GlobalIPv6PrefixLen"`
	// SecondaryIPv6Addresses is a list of extra IPv6 Addresses that the
	// container has been assigned in this network.
	SecondaryIPv6Addresses []Address `json:"SecondaryIPv6Addresses,omitempty"`
	// MacAddress is the MAC address for the interface in this network.
	MacAddress string `json:"MacAddress"`
	// AdditionalMacAddresses is a set of additional MAC Addresses beyond
	// the first. CNI may configure more than one interface for a single
	// network, which can cause this.
	AdditionalMacAddresses []string `json:"AdditionalMACAddresses,omitempty"`
}

// InspectAdditionalNetwork holds information about non-default networks the
// container has been connected to.
// As with InspectNetworkSettings, many fields are unused and maintained only
// for compatibility with Docker.
type InspectAdditionalNetwork struct {
	InspectBasicNetworkConfig

	// Name of the network we're connecting to.
	NetworkID string `json:"NetworkID,omitempty"`
	// DriverOpts is presently unused and maintained exclusively for
	// compatibility.
	DriverOpts map[string]string `json:"DriverOpts"`
	// IPAMConfig is presently unused and maintained exclusively for
	// compatibility.
	IPAMConfig map[string]string `json:"IPAMConfig"`
	// Links is presently unused and maintained exclusively for
	// compatibility.
	Links []string `json:"Links"`
	// Aliases are any network aliases the container has in this network.
	Aliases []string `json:"Aliases,omitempty"`
}

// InspectNetworkSettings holds information about the network settings of the
// container.
// Many fields are maintained only for compatibility with `docker inspect` and
// are unused within Libpod.
type InspectNetworkSettings struct {
	InspectBasicNetworkConfig

	Bridge                 string                       `json:"Bridge"`
	SandboxID              string                       `json:"SandboxID"`
	HairpinMode            bool                         `json:"HairpinMode"`
	LinkLocalIPv6Address   string                       `json:"LinkLocalIPv6Address"`
	LinkLocalIPv6PrefixLen int                          `json:"LinkLocalIPv6PrefixLen"`
	Ports                  map[string][]InspectHostPort `json:"Ports"`
	SandboxKey             string                       `json:"SandboxKey"`
	// Networks contains information on non-default networks this
	// container has joined.
	// It is a map of network name to network information.
	Networks map[string]*InspectAdditionalNetwork `json:"Networks,omitempty"`
}

// InspectContainerData provides a detailed record of a container's configuration
// and state as viewed by Libpod.
// Large portions of this structure are defined such that the output is
// compatible with `docker inspect` JSON, but additional fields have been added
// as required to share information not in the original output.
type InspectContainerData struct {
	ID                      string                      `json:"Id"`
	Created                 time.Time                   `json:"Created"`
	Path                    string                      `json:"Path"`
	Args                    []string                    `json:"Args"`
	State                   *InspectContainerState      `json:"State"`
	Image                   string                      `json:"Image"`
	ImageDigest             string                      `json:"ImageDigest"`
	ImageName               string                      `json:"ImageName"`
	Rootfs                  string                      `json:"Rootfs"`
	Pod                     string                      `json:"Pod"`
	ResolvConfPath          string                      `json:"ResolvConfPath"`
	HostnamePath            string                      `json:"HostnamePath"`
	HostsPath               string                      `json:"HostsPath"`
	StaticDir               string                      `json:"StaticDir"`
	OCIConfigPath           string                      `json:"OCIConfigPath,omitempty"`
	OCIRuntime              string                      `json:"OCIRuntime,omitempty"`
	ConmonPidFile           string                      `json:"ConmonPidFile"`
	PidFile                 string                      `json:"PidFile"`
	Name                    string                      `json:"Name"`
	RestartCount            int32                       `json:"RestartCount"`
	Driver                  string                      `json:"Driver"`
	MountLabel              string                      `json:"MountLabel"`
	ProcessLabel            string                      `json:"ProcessLabel"`
	AppArmorProfile         string                      `json:"AppArmorProfile"`
	EffectiveCaps           []string                    `json:"EffectiveCaps"`
	BoundingCaps            []string                    `json:"BoundingCaps"`
	ExecIDs                 []string                    `json:"ExecIDs"`
	GraphDriver             *DriverData                 `json:"GraphDriver"`
	SizeRw                  *int64                      `json:"SizeRw,omitempty"`
	SizeRootFs              int64                       `json:"SizeRootFs,omitempty"`
	Mounts                  []InspectMount              `json:"Mounts"`
	Dependencies            []string                    `json:"Dependencies"`
	NetworkSettings         *InspectNetworkSettings     `json:"NetworkSettings"`
	Namespace               string                      `json:"Namespace"`
	IsInfra                 bool                        `json:"IsInfra"`
	IsService               bool                        `json:"IsService"`
	KubeExitCodePropagation string                      `json:"KubeExitCodePropagation"`
	LockNumber              uint32                      `json:"lockNumber"`
	Config                  *InspectContainerConfig     `json:"Config"`
	HostConfig              *InspectContainerHostConfig `json:"HostConfig"`
	UseImageHosts           bool                        `json:"UseImageHosts"`
	UseImageHostname        bool                        `json:"UseImageHostname"`
}

// InspectExecSession contains information about a given exec session.
type InspectExecSession struct {
	// CanRemove is legacy and used purely for compatibility reasons.
	// Will always be set to true, unless the exec session is running.
	CanRemove bool `json:"CanRemove"`
	// ContainerID is the ID of the container this exec session is attached
	// to.
	ContainerID string `json:"ContainerID"`
	// DetachKeys are the detach keys used by the exec session.
	// If set to "" the default keys are being used.
	// Will show "<none>" if no detach keys are set.
	DetachKeys string `json:"DetachKeys"`
	// ExitCode is the exit code of the exec session. Will be set to 0 if
	// the exec session has not yet exited.
	ExitCode int `json:"ExitCode"`
	// ID is the ID of the exec session.
	ID string `json:"ID"`
	// OpenStderr is whether the container's STDERR stream will be attached.
	// Always set to true if the exec session created a TTY.
	OpenStderr bool `json:"OpenStderr"`
	// OpenStdin is whether the container's STDIN stream will be attached
	// to.
	OpenStdin bool `json:"OpenStdin"`
	// OpenStdout is whether the container's STDOUT stream will be attached.
	// Always set to true if the exec session created a TTY.
	OpenStdout bool `json:"OpenStdout"`
	// Running is whether the exec session is running.
	Running bool `json:"Running"`
	// Pid is the PID of the exec session's process.
	// Will be set to 0 if the exec session is not running.
	Pid int `json:"Pid"`
	// ProcessConfig contains information about the exec session's process.
	ProcessConfig *InspectExecProcess `json:"ProcessConfig"`
}

// InspectExecProcess contains information about the process in a given exec
// session.
type InspectExecProcess struct {
	// Arguments are the arguments to the entrypoint command of the exec
	// session.
	Arguments []string `json:"arguments"`
	// Entrypoint is the entrypoint for the exec session (the command that
	// will be executed in the container).
	Entrypoint string `json:"entrypoint"`
	// Privileged is whether the exec session will be started with elevated
	// privileges.
	Privileged bool `json:"privileged"`
	// Tty is whether the exec session created a terminal.
	Tty bool `json:"tty"`
	// User is the user the exec session was started as.
	User string `json:"user"`
}

// DriverData handles the data for a storage driver
type DriverData struct {
	Name string            `json:"Name"`
	Data map[string]string `json:"Data"`
}

// InspectSecret contains information on secrets mounted inside the container
type InspectSecret struct {
	// Name is the name of the secret
	Name string `json:"Name"`
	// ID is the ID of the secret
	ID string `json:"ID"`
	// ID is the UID of the mounted secret file
	UID uint32 `json:"UID"`
	// ID is the GID of the mounted secret file
	GID uint32 `json:"GID"`
	// ID is the ID of the mode of the mounted secret file
	Mode uint32 `json:"Mode"`
}
