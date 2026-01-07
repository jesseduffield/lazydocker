//go:build !remote

package libpod

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/lock"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/pasta"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/secrets"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/storage"
	"golang.org/x/sys/unix"
)

// CgroupfsDefaultCgroupParent is the cgroup parent for CgroupFS in libpod
const CgroupfsDefaultCgroupParent = "/libpod_parent"

// SystemdDefaultCgroupParent is the cgroup parent for the systemd cgroup
// manager in libpod
const SystemdDefaultCgroupParent = "machine.slice"

// SystemdDefaultRootlessCgroupParent is the cgroup parent for the systemd cgroup
// manager in libpod when running as rootless
const SystemdDefaultRootlessCgroupParent = "user.slice"

// DefaultWaitInterval is the default interval between container status checks
// while waiting.
const DefaultWaitInterval = 250 * time.Millisecond

// LinuxNS represents a Linux namespace
type LinuxNS int

const (
	// InvalidNS is an invalid namespace
	InvalidNS LinuxNS = iota
	// IPCNS is the IPC namespace
	IPCNS
	// MountNS is the mount namespace
	MountNS
	// NetNS is the network namespace
	NetNS
	// PIDNS is the PID namespace
	PIDNS
	// UserNS is the user namespace
	UserNS
	// UTSNS is the UTS namespace
	UTSNS
	// CgroupNS is the Cgroup namespace
	CgroupNS
)

// String returns a string representation of a Linux namespace
// It is guaranteed to be the name of the namespace in /proc for valid ns types
func (ns LinuxNS) String() string {
	s := [...]string{
		InvalidNS: "invalid",
		IPCNS:     "ipc",
		MountNS:   "mnt",
		NetNS:     "net",
		PIDNS:     "pid",
		UserNS:    "user",
		UTSNS:     "uts",
		CgroupNS:  "cgroup",
	}
	if ns >= 0 && int(ns) < len(s) {
		return s[ns]
	}
	return "unknown"
}

// Container is a single OCI container.
// All operations on a Container that access state must begin with a call to
// syncContainer().
// There is no guarantee that state exists in a readable state before
// syncContainer() is run, and even if it does, its contents will be out of date
// and must be refreshed from the database.
// Generally, this requirement applies only to top-level functions; helpers can
// assume that their callers handled this requirement. Generally speaking, if a
// function takes the container lock and accesses any part of state, it should
// syncContainer() immediately after locking.
type Container struct {
	config *ContainerConfig

	state *ContainerState

	// Batched indicates that a container has been locked as part of a
	// Batch() operation
	// Functions called on a batched container will not lock or sync
	batched bool

	valid      bool
	lock       lock.Locker
	runtime    *Runtime
	ociRuntime OCIRuntime

	rootlessSlirpSyncR *os.File
	rootlessSlirpSyncW *os.File

	rootlessPortSyncR *os.File
	rootlessPortSyncW *os.File

	// reservedPorts contains the fds for the bound ports when using the
	// bridge network mode as root.
	reservedPorts []*os.File

	// perNetworkOpts should be set when you want to use special network
	// options when calling network setup/teardown. This should be used for
	// container restore or network reload for example. Leave this nil if
	// the settings from the container config should be used.
	perNetworkOpts map[string]types.PerNetworkOptions

	// This is true if a container is restored from a checkpoint.
	restoreFromCheckpoint bool

	slirp4netnsSubnet *net.IPNet
	pastaResult       *pasta.SetupResult
}

// ContainerState contains the current state of the container
// It is stored on disk in a tmpfs and recreated on reboot
type ContainerState struct {
	// The current state of the running container
	State define.ContainerStatus `json:"state"`
	// The path to the JSON OCI runtime spec for this container
	ConfigPath string `json:"configPath,omitempty"`
	// RunDir is a per-boot directory for container content
	RunDir string `json:"runDir,omitempty"`
	// Mounted indicates whether the container's storage has been mounted
	// for use
	Mounted bool `json:"mounted,omitempty"`
	// Mountpoint contains the path to the container's mounted storage as given
	// by containers/storage.
	Mountpoint string `json:"mountPoint,omitempty"`
	// StartedTime is the time the container was started
	StartedTime time.Time `json:"startedTime"`
	// FinishedTime is the time the container finished executing
	FinishedTime time.Time `json:"finishedTime"`
	// ExitCode is the exit code returned when the container stopped
	ExitCode int32 `json:"exitCode,omitempty"`
	// Exited is whether the container has exited
	Exited bool `json:"exited,omitempty"`
	// Error holds the last known error message during start, stop, or remove
	Error string `json:"error,omitempty"`
	// OOMKilled indicates that the container was killed as it ran out of
	// memory
	OOMKilled bool `json:"oomKilled,omitempty"`
	// Checkpointed indicates that the container was stopped by a checkpoint
	// operation.
	Checkpointed bool `json:"checkpointed,omitempty"`
	// PID is the PID of a running container
	PID int `json:"pid,omitempty"`
	// ConmonPID is the PID of the container's conmon
	ConmonPID int `json:"conmonPid,omitempty"`
	// ExecSessions contains all exec sessions that are associated with this
	// container.
	ExecSessions map[string]*ExecSession `json:"newExecSessions,omitempty"`
	// LegacyExecSessions are legacy exec sessions from older versions of
	// Podman.
	// These are DEPRECATED and will be removed in a future release.
	LegacyExecSessions map[string]*legacyExecSession `json:"execSessions,omitempty"`
	// NetNS is the path or name of the NetNS
	NetNS string `json:"netns,omitempty"`
	// NetworkStatus contains the network Status for all networks
	// the container is attached to. Only populated if we created a network
	// namespace for the container, and the network namespace is currently
	// active.
	// To read this field use container.getNetworkStatus() instead, this will
	// take care of migrating the old DEPRECATED network status to the new format.
	NetworkStatus map[string]types.StatusBlock `json:"networkStatus,omitempty"`
	// BindMounts contains files that will be bind-mounted into the
	// container when it is mounted.
	// These include /etc/hosts and /etc/resolv.conf
	// This maps the path the file will be mounted to in the container to
	// the path of the file on disk outside the container
	BindMounts map[string]string `json:"bindMounts,omitempty"`
	// StoppedByUser indicates whether the container was stopped by an
	// explicit call to the Stop() API.
	StoppedByUser bool `json:"stoppedByUser,omitempty"`
	// RestartPolicyMatch indicates whether the conditions for restart
	// policy have been met.
	RestartPolicyMatch bool `json:"restartPolicyMatch,omitempty"`
	// RestartCount is how many times the container was restarted by its
	// restart policy. This is NOT incremented by normal container restarts
	// (only by restart policy).
	RestartCount uint `json:"restartCount,omitempty"`
	// StartupHCPassed indicates that the startup healthcheck has
	// succeeded and the main healthcheck can begin.
	StartupHCPassed bool `json:"startupHCPassed,omitempty"`
	// StartupHCSuccessCount indicates the number of successes of the
	// startup healthcheck. A startup HC can require more than one success
	// to be marked as passed.
	StartupHCSuccessCount int `json:"startupHCSuccessCount,omitempty"`
	// StartupHCFailureCount indicates the number of failures of the startup
	// healthcheck. The container will be restarted if this exceed a set
	// number in the startup HC config.
	StartupHCFailureCount int `json:"startupHCFailureCount,omitempty"`
	// HCUnitName records the name of the healthcheck unit.
	// Automatically generated when the healthcheck is started.
	HCUnitName string `json:"hcUnitName,omitempty"`

	// ExtensionStageHooks holds hooks which will be executed by libpod
	// and not delegated to the OCI runtime.
	ExtensionStageHooks map[string][]spec.Hook `json:"extensionStageHooks,omitempty"`

	// NetInterfaceDescriptions describe the relationship between a CNI
	// network and an interface names
	NetInterfaceDescriptions ContainerNetworkDescriptions `json:"networkDescriptions,omitempty"`

	// Service indicates that container is the service container of a
	// service. A service consists of one or more pods.  The service
	// container is started before all pods and is stopped when the last
	// pod stops. The service container allows for tracking and managing
	// the entire life cycle of service which may be started via
	// `podman-play-kube`.
	Service Service

	// Following checkpoint/restore related information is displayed
	// if the container has been checkpointed or restored.
	CheckpointedTime time.Time `json:"checkpointedTime"`
	RestoredTime     time.Time `json:"restoredTime"`
	CheckpointLog    string    `json:"checkpointLog,omitempty"`
	CheckpointPath   string    `json:"checkpointPath,omitempty"`
	RestoreLog       string    `json:"restoreLog,omitempty"`
	Restored         bool      `json:"restored,omitempty"`
}

// ContainerNamedVolume is a named volume that will be mounted into the
// container. Each named volume is a libpod Volume present in the state.
type ContainerNamedVolume struct {
	// Name is the name of the volume to mount in.
	// Must resolve to a valid volume present in this Podman.
	Name string `json:"volumeName"`
	// Dest is the mount's destination
	Dest string `json:"dest"`
	// Options are fstab style mount options
	Options []string `json:"options,omitempty"`
	// IsAnonymous sets the named volume as anonymous even if it has a name
	// This is used for emptyDir volumes from a kube yaml
	IsAnonymous bool `json:"setAnonymous,omitempty"`
	// SubPath determines which part of the Source will be mounted in the container
	SubPath string `json:",omitempty"`
}

// ContainerOverlayVolume is an overlay volume that will be mounted into the
// container. Each volume is a libpod Volume present in the state.
type ContainerOverlayVolume struct {
	// Destination is the absolute path where the mount will be placed in the container.
	Dest string `json:"dest"`
	// Source specifies the source path of the mount.
	Source string `json:"source,omitempty"`
	// Options holds overlay volume options.
	Options []string `json:"options,omitempty"`
}

// ContainerImageVolume is a volume based on a container image.  The container
// image is first mounted on the host and is then bind-mounted into the
// container.
type ContainerImageVolume struct {
	// Source is the source of the image volume.  The image can be referred
	// to by name and by ID.
	Source string `json:"source"`
	// Dest is the absolute path of the mount in the container.
	Dest string `json:"dest"`
	// ReadWrite sets the volume writable.
	ReadWrite bool `json:"rw"`
	// SubPath determines which part of the image will be mounted into the container.
	SubPath string `json:"subPath,omitempty"`
}

// ContainerArtifactVolume is a volume based on a artifact. The artifact blobs will
// be bind mounted directly as files and must always be read only.
type ContainerArtifactVolume struct {
	// Source is the name or digest of the artifact that should be mounted
	Source string `json:"source"`
	// Dest is the absolute path of the mount in the container.
	// If path is a file in the container, then the artifact must consist of a single blob.
	// Otherwise if it is a directory or does not exists all artifact blobs will be mounted
	// into this path as files. As name the "org.opencontainers.image.title" will be used if
	// available otherwise the digest is used as name.
	Dest string `json:"dest"`
	// Title can be used for multi blob artifacts to only mount the one specific blob that
	// matches the "org.opencontainers.image.title" annotation.
	// Optional. Conflicts with Digest.
	Title string `json:"title"`
	// Digest can be used to filter a single blob from a multi blob artifact by the given digest.
	// When this option is set the file name in the container defaults to the digest even when
	// the title annotation exist.
	// Optional. Conflicts with Title.
	Digest string `json:"digest"`
	// Name is the name that should be used for the path inside the container. When a single blob
	// is mounted the name is used as is. If multiple blobs are mounted then mount them as
	// "<name>-x" where x is a 0 indexed integer based on the layer order.
	// Optional.
	Name string `json:"name,omitempty"`
}

// ContainerSecret is a secret that is mounted in a container
type ContainerSecret struct {
	// Secret is the secret
	*secrets.Secret
	// UID is the UID of the secret file
	UID uint32
	// GID is the GID of the secret file
	GID uint32
	// Mode is the mode of the secret file
	Mode uint32
	// Secret target inside container
	Target string
}

// ContainerNetworkDescriptions describes the relationship between the CNI
// network and the ethN where N is an integer
type ContainerNetworkDescriptions map[string]int

// Config accessors
// Unlocked

// Config returns the configuration used to create the container.
// Note that the returned config does not include the actual networks.
// Use ConfigWithNetworks() if you need them.
func (c *Container) Config() *ContainerConfig {
	returnConfig := new(ContainerConfig)
	if err := JSONDeepCopy(c.config, returnConfig); err != nil {
		return nil
	}
	return returnConfig
}

// Config returns the configuration used to create the container.
func (c *Container) ConfigWithNetworks() *ContainerConfig {
	returnConfig := c.Config()
	if returnConfig == nil {
		return nil
	}

	networks, err := c.networks()
	if err != nil {
		return nil
	}
	returnConfig.Networks = networks

	return returnConfig
}

// ConfigNoCopy returns the configuration used by the container.
// Note that the returned value is not a copy and must hence
// only be used in a reading fashion.
func (c *Container) ConfigNoCopy() *ContainerConfig {
	return c.config
}

// DeviceHostSrc returns the user supplied device to be passed down in the pod
func (c *Container) DeviceHostSrc() []spec.LinuxDevice {
	return c.config.DeviceHostSrc
}

// Runtime returns the container's Runtime.
func (c *Container) Runtime() *Runtime {
	return c.runtime
}

// Spec returns the container's OCI runtime spec
// The spec returned is the one used to create the container. The running
// spec may differ slightly as mounts are added based on the image
func (c *Container) Spec() *spec.Spec {
	returnSpec := new(spec.Spec)
	if err := JSONDeepCopy(c.config.Spec, returnSpec); err != nil {
		return nil
	}

	return returnSpec
}

// specFromState returns the unmarshalled json config of the container.  If the
// config does not exist (e.g., because the container was never started) return
// the spec from the config.
func (c *Container) specFromState() (*spec.Spec, error) {
	returnSpec := c.config.Spec

	if f, err := os.Open(c.state.ConfigPath); err == nil {
		returnSpec = new(spec.Spec)
		content, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("reading container config: %w", err)
		}
		if err := json.Unmarshal(content, &returnSpec); err != nil {
			// Malformed spec, just use c.config.Spec instead
			logrus.Warnf("Error unmarshalling container %s config: %v", c.ID(), err)
			return c.config.Spec, nil
		}
	} else if !os.IsNotExist(err) {
		// ignore when the file does not exist
		return nil, fmt.Errorf("opening container config: %w", err)
	}

	return returnSpec, nil
}

// ID returns the container's ID
func (c *Container) ID() string {
	return c.config.ID
}

// Name returns the container's name
func (c *Container) Name() string {
	return c.config.Name
}

// PodID returns the full ID of the pod the container belongs to, or "" if it
// does not belong to a pod
func (c *Container) PodID() string {
	return c.config.Pod
}

// Namespace returns the libpod namespace the container is in.
// Namespaces are used to logically separate containers and pods in the state.
func (c *Container) Namespace() string {
	return c.config.Namespace
}

// Image returns the ID and name of the image used as the container's rootfs.
func (c *Container) Image() (string, string) {
	return c.config.RootfsImageID, c.config.RootfsImageName
}

// RawImageName returns the unprocessed and not-normalized user-specified image
// name.
func (c *Container) RawImageName() string {
	return c.config.RawImageName
}

// ShmDir returns the sources path to be mounted on /dev/shm in container
func (c *Container) ShmDir() string {
	return c.config.ShmDir
}

// ShmSize returns the size of SHM device to be mounted into the container
func (c *Container) ShmSize() int64 {
	return c.config.ShmSize
}

// StaticDir returns the directory used to store persistent container files
func (c *Container) StaticDir() string {
	return c.config.StaticDir
}

// NamedVolumes returns the container's named volumes.
// The name of each is guaranteed to point to a valid libpod Volume present in
// the state.
func (c *Container) NamedVolumes() []*ContainerNamedVolume {
	volumes := []*ContainerNamedVolume{}
	for _, vol := range c.config.NamedVolumes {
		newVol := new(ContainerNamedVolume)
		newVol.Name = vol.Name
		newVol.Dest = vol.Dest
		newVol.Options = vol.Options
		newVol.SubPath = vol.SubPath
		volumes = append(volumes, newVol)
	}

	return volumes
}

// Privileged returns whether the container is privileged
func (c *Container) Privileged() bool {
	return c.config.Privileged
}

// ProcessLabel returns the selinux ProcessLabel of the container
func (c *Container) ProcessLabel() string {
	return c.config.ProcessLabel
}

// MountLabel returns the SELinux mount label of the container
func (c *Container) MountLabel() string {
	return c.config.MountLabel
}

// Systemd returns whether the container will be running in systemd mode
func (c *Container) Systemd() bool {
	if c.config.Systemd != nil {
		return *c.config.Systemd
	}
	return false
}

// User returns the user who the container is run as
func (c *Container) User() string {
	return c.config.User
}

// Dependencies gets the containers this container depends upon
func (c *Container) Dependencies() []string {
	// Collect in a map first to remove dupes
	dependsCtrs := map[string]bool{}

	// First add all namespace containers
	if c.config.IPCNsCtr != "" {
		dependsCtrs[c.config.IPCNsCtr] = true
	}
	if c.config.MountNsCtr != "" {
		dependsCtrs[c.config.MountNsCtr] = true
	}
	if c.config.NetNsCtr != "" {
		dependsCtrs[c.config.NetNsCtr] = true
	}
	if c.config.PIDNsCtr != "" {
		dependsCtrs[c.config.PIDNsCtr] = true
	}
	if c.config.UserNsCtr != "" {
		dependsCtrs[c.config.UserNsCtr] = true
	}
	if c.config.UTSNsCtr != "" {
		dependsCtrs[c.config.UTSNsCtr] = true
	}
	if c.config.CgroupNsCtr != "" {
		dependsCtrs[c.config.CgroupNsCtr] = true
	}

	// Add all generic dependencies
	for _, id := range c.config.Dependencies {
		dependsCtrs[id] = true
	}

	if len(dependsCtrs) == 0 {
		return []string{}
	}

	depends := make([]string, 0, len(dependsCtrs))
	for ctr := range dependsCtrs {
		depends = append(depends, ctr)
	}

	return depends
}

// NewNetNS returns whether the container will create a new network namespace
func (c *Container) NewNetNS() bool {
	return c.config.CreateNetNS
}

// PortMappings returns the ports that will be mapped into a container if
// a new network namespace is created
// If NewNetNS() is false, this value is unused
func (c *Container) PortMappings() ([]types.PortMapping, error) {
	// First check if the container belongs to a network namespace (like a pod)
	if len(c.config.NetNsCtr) > 0 {
		netNsCtr, err := c.runtime.GetContainer(c.config.NetNsCtr)
		if err != nil {
			return nil, fmt.Errorf("unable to look up network namespace for container %s: %w", c.ID(), err)
		}
		return netNsCtr.PortMappings()
	}
	return c.config.PortMappings, nil
}

// DNSServers returns DNS servers that will be used in the container's
// resolv.conf
// If empty, DNS server from the host's resolv.conf will be used instead
func (c *Container) DNSServers() []net.IP {
	return c.config.DNSServer
}

// DNSSearch returns the DNS search domains that will be used in the container's
// resolv.conf
// If empty, DNS Search domains from the host's resolv.conf will be used instead
func (c *Container) DNSSearch() []string {
	return c.config.DNSSearch
}

// DNSOption returns the DNS options that will be used in the container's
// resolv.conf
// If empty, options from the host's resolv.conf will be used instead
func (c *Container) DNSOption() []string {
	return c.config.DNSOption
}

// HostsAdd returns hosts that will be added to the container's hosts file
// The host system's hosts file is used as a base, and these are appended to it
func (c *Container) HostsAdd() []string {
	return c.config.HostAdd
}

// UserVolumes returns user-added volume mounts in the container.
// These are not added to the spec, but are used during image commit and to
// trigger some OCI hooks.
func (c *Container) UserVolumes() []string {
	volumes := make([]string, 0, len(c.config.UserVolumes))
	volumes = append(volumes, c.config.UserVolumes...)
	return volumes
}

// Entrypoint is the container's entrypoint.
// This is not added to the spec, but is instead used during image commit.
func (c *Container) Entrypoint() []string {
	entrypoint := make([]string, 0, len(c.config.Entrypoint))
	entrypoint = append(entrypoint, c.config.Entrypoint...)
	return entrypoint
}

// Command is the container's command
// This is not added to the spec, but is instead used during image commit
func (c *Container) Command() []string {
	command := make([]string, 0, len(c.config.Command))
	command = append(command, c.config.Command...)
	return command
}

// Stdin returns whether STDIN on the container will be kept open
func (c *Container) Stdin() bool {
	return c.config.Stdin
}

// Labels returns the container's labels
func (c *Container) Labels() map[string]string {
	labels := make(map[string]string)
	maps.Copy(labels, c.config.Labels)
	return labels
}

// StopSignal is the signal that will be used to stop the container
// If it fails to stop the container, SIGKILL will be used after a timeout
// If StopSignal is 0, the default signal of SIGTERM will be used
func (c *Container) StopSignal() uint {
	return c.config.StopSignal
}

// StopTimeout returns the container's stop timeout
// If the container's default stop signal fails to kill the container, SIGKILL
// will be used after this timeout
func (c *Container) StopTimeout() uint {
	return c.config.StopTimeout
}

// CreatedTime gets the time when the container was created
func (c *Container) CreatedTime() time.Time {
	return c.config.CreatedTime
}

// CgroupParent gets the container's Cgroup parent
func (c *Container) CgroupParent() string {
	return c.config.CgroupParent
}

// LogPath returns the path to the container's log file
// This file will only be present after Init() is called to create the container
// in the runtime
func (c *Container) LogPath() string {
	return c.config.LogPath
}

// LogTag returns the tag to the container's log file
func (c *Container) LogTag() string {
	return c.config.LogTag
}

// LogSizeMax returns the maximum size of the container's log file.
func (c *Container) LogSizeMax() int64 {
	if c.config.LogSize > 0 {
		return c.config.LogSize
	}
	return c.runtime.config.Containers.LogSizeMax
}

// RestartPolicy returns the container's restart policy.
func (c *Container) RestartPolicy() string {
	return c.config.RestartPolicy
}

// RestartRetries returns the number of retries that will be attempted when
// using the "on-failure" restart policy
func (c *Container) RestartRetries() uint {
	return c.config.RestartRetries
}

// LogDriver returns the log driver for this container
func (c *Container) LogDriver() string {
	return c.config.LogDriver
}

// RuntimeName returns the name of the runtime
func (c *Container) RuntimeName() string {
	return c.config.OCIRuntime
}

// Runtime spec accessors
// Unlocked

// hostname determines the container's hostname.
// If 'network' is true and the container isn't running in a
// private UTS namespoace, an empty string will be returned
// instead of the host's hostname because we never want to
// send the host's hostname to a DHCP or DNS server.
func (c *Container) hostname(network bool) string {
	if c.config.UTSNsCtr != "" {
		utsNsCtr, err := c.runtime.GetContainer(c.config.UTSNsCtr)
		if err != nil {
			// should we return an error here?
			logrus.Errorf("unable to look up uts namespace for container %s: %v", c.ID(), err)
			return ""
		}
		return utsNsCtr.Hostname()
	}

	if c.config.Spec.Hostname != "" {
		return c.config.Spec.Hostname
	}

	// If the container is not running in a private UTS namespace,
	// return the host's hostname unless 'network' is true in which
	// case we return an empty string.
	privateUTS := c.hasPrivateUTS()
	if !privateUTS {
		hostname, err := os.Hostname()
		if err == nil {
			if network {
				return ""
			}
			return hostname
		}
		logrus.Errorf("unable to get host's hostname for container %s: %v", c.ID(), err)
		return ""
	}

	// If container_name_as_hostname is set in the CONTAINERS table in
	// containers.conf, use a sanitized version of the container's name
	// as the hostname.  Since the container name must already match
	// the set '[a-zA-Z0-9][a-zA-Z0-9_.-]*', we can just remove any
	// underscores and limit it to 64 characters to make it a valid
	// hostname.
	if c.runtime.config.Containers.ContainerNameAsHostName {
		sanitizedHostname := strings.ReplaceAll(c.Name(), "_", "")
		if len(sanitizedHostname) <= 64 {
			return sanitizedHostname
		}
		return sanitizedHostname[:64]
	}

	// Otherwise use the container's short ID as the hostname.
	if len(c.ID()) < 11 {
		return c.ID()
	}
	return c.ID()[:12]
}

// Hostname gets the container's hostname
func (c *Container) Hostname() string {
	return c.hostname(false)
}

// If the container isn't running in a private UTS namespace, Hostname()
// will return the host's hostname as the container's hostname. If netavark
// were to try and obtain a DHCP lease with the host's hostname in an environment
// where DDNS was active, bad things could happen. NetworkHostname() on the
// other hand, will return an empty string if the container isn't running
// in a private UTS namespace.
//
// This function should only be used to populate the ContainerHostname member
// of the common.libnetwork.types.NetworkOptions struct.
func (c *Container) NetworkHostname() string {
	return c.hostname(true)
}

// WorkingDir returns the containers working dir
func (c *Container) WorkingDir() string {
	if c.config.Spec.Process != nil {
		return c.config.Spec.Process.Cwd
	}
	return "/"
}

// Terminal returns true if the container has a terminal
func (c *Container) Terminal() bool {
	if c.config.Spec != nil && c.config.Spec.Process != nil {
		return c.config.Spec.Process.Terminal
	}
	return false
}

// LinuxResources return the containers Linux Resources (if any)
func (c *Container) LinuxResources() *spec.LinuxResources {
	if c.config.Spec != nil && c.config.Spec.Linux != nil {
		return c.config.Spec.Linux.Resources
	}
	return nil
}

// Env returns the default environment variables defined for the container
func (c *Container) Env() []string {
	if c.config.Spec != nil && c.config.Spec.Process != nil {
		return c.config.Spec.Process.Env
	}
	return nil
}

// State Accessors
// Require locking

// State returns the current state of the container
func (c *Container) State() (define.ContainerStatus, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return define.ContainerStateUnknown, err
		}
	}
	return c.state.State, nil
}

func (c *Container) RestartCount() (uint, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return 0, err
		}
	}
	return c.state.RestartCount, nil
}

// Mounted returns whether the container is mounted and the path it is mounted
// at (if it is mounted).
// If the container is not mounted, no error is returned, and the mountpoint
// will be set to "".
func (c *Container) Mounted() (bool, string, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return false, "", fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	// We cannot directly return c.state.Mountpoint as it is not guaranteed
	// to be set if the container is mounted, only if the container has been
	// prepared with c.prepare().
	// Instead, let's call into c/storage
	mountedTimes, err := c.runtime.storageService.MountedContainerImage(c.ID())
	if err != nil {
		return false, "", err
	}

	if mountedTimes > 0 {
		mountPoint, err := c.runtime.storageService.GetMountpoint(c.ID())
		if err != nil {
			return false, "", err
		}

		return true, mountPoint, nil
	}

	return false, "", nil
}

// StartedTime is the time the container was started
func (c *Container) StartedTime() (time.Time, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return time.Time{}, fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	return c.state.StartedTime, nil
}

// FinishedTime is the time the container was stopped
func (c *Container) FinishedTime() (time.Time, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return time.Time{}, fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	return c.state.FinishedTime, nil
}

// ExitCode returns the exit code of the container as
// an int32, and whether the container has exited.
// If the container has not exited, exit code will always be 0.
// If the container restarts, the exit code is reset to 0.
func (c *Container) ExitCode() (int32, bool, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return 0, false, fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	return c.state.ExitCode, c.state.Exited, nil
}

// OOMKilled returns whether the container was killed by an OOM condition
func (c *Container) OOMKilled() (bool, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return false, fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	return c.state.OOMKilled, nil
}

// PID returns the PID of the container.
// If the container is not running, a pid of 0 will be returned. No error will
// occur.
func (c *Container) PID() (int, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return -1, err
		}
	}

	return c.state.PID, nil
}

// ConmonPID Returns the PID of the container's conmon process.
// If the container is not running, a PID of 0 will be returned. No error will
// occur.
func (c *Container) ConmonPID() (int, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return -1, err
		}
	}

	return c.state.ConmonPID, nil
}

// ExecSessions retrieves active exec sessions running in the container
func (c *Container) ExecSessions() ([]string, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	ids := make([]string, 0, len(c.state.ExecSessions))
	for id := range c.state.ExecSessions {
		ids = append(ids, id)
	}

	return ids, nil
}

// execSessionNoCopy returns the associated exec session to id.
// Note that the session is not a deep copy.
func (c *Container) execSessionNoCopy(id string) (*ExecSession, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	session, ok := c.state.ExecSessions[id]
	if !ok {
		return nil, fmt.Errorf("no exec session with ID %s found in container %s: %w", id, c.ID(), define.ErrNoSuchExecSession)
	}

	// make sure to update the exec session if needed #18424
	alive, err := c.ociRuntime.ExecUpdateStatus(c, id)
	if err != nil {
		return nil, err
	}
	if !alive {
		if err := retrieveAndWriteExecExitCode(c, session.ID()); err != nil {
			return nil, err
		}
	}

	return session, nil
}

// ExecSession retrieves detailed information on a single active exec session in
// a container
func (c *Container) ExecSession(id string) (*ExecSession, error) {
	session, err := c.execSessionNoCopy(id)
	if err != nil {
		return nil, err
	}

	returnSession := new(ExecSession)
	if err := JSONDeepCopy(session, returnSession); err != nil {
		return nil, fmt.Errorf("copying contents of container %s exec session %s: %w", c.ID(), session.ID(), err)
	}

	return returnSession, nil
}

// BindMounts retrieves bind mounts that were created by libpod and will be
// added to the container
// All these mounts except /dev/shm are ignored if a mount in the given spec has
// the same destination
// These mounts include /etc/resolv.conf, /etc/hosts, and /etc/hostname
// The return is formatted as a map from destination (mountpoint in the
// container) to source (path of the file that will be mounted into the
// container)
// If the container has not been started yet, an empty map will be returned, as
// the files in question are only created when the container is started.
func (c *Container) BindMounts() (map[string]string, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	newMap := make(map[string]string, len(c.state.BindMounts))

	maps.Copy(newMap, c.state.BindMounts)

	return newMap, nil
}

// StoppedByUser returns whether the container was last stopped by an explicit
// call to the Stop() API, or whether it exited naturally.
func (c *Container) StoppedByUser() (bool, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return false, err
		}
	}

	return c.state.StoppedByUser, nil
}

// StartupHCPassed returns whether the container's startup healthcheck passed.
func (c *Container) StartupHCPassed() (bool, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return false, err
		}
	}

	return c.state.StartupHCPassed, nil
}

// Misc Accessors
// Most will require locking

// NamespacePath returns the path of one of the container's namespaces
// If the container is not running, an error will be returned
func (c *Container) NamespacePath(linuxNS LinuxNS) (string, error) { //nolint:interfacer
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return "", fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}

	return c.namespacePath(linuxNS)
}

// namespacePath returns the path of one of the container's namespaces
// If the container is not running, an error will be returned
func (c *Container) namespacePath(linuxNS LinuxNS) (string, error) { //nolint:interfacer
	if c.state.State != define.ContainerStateRunning && c.state.State != define.ContainerStatePaused {
		return "", fmt.Errorf("cannot get namespace path unless container %s is running: %w", c.ID(), define.ErrCtrStopped)
	}

	if linuxNS == InvalidNS {
		return "", fmt.Errorf("invalid namespace requested from container %s: %w", c.ID(), define.ErrInvalidArg)
	}

	return fmt.Sprintf("/proc/%d/ns/%s", c.state.PID, linuxNS.String()), nil
}

// CgroupManager returns the cgroup manager used by the given container.
func (c *Container) CgroupManager() string {
	cgroupManager := c.config.CgroupManager
	if cgroupManager == "" {
		cgroupManager = c.runtime.config.Engine.CgroupManager
	}
	return cgroupManager
}

// CgroupPath returns a cgroups "path" for the given container.
// Note that the container must be running.  Otherwise, an error
// is returned.
func (c *Container) CgroupPath() (string, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return "", fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	return c.cGroupPath()
}

// cGroupPath returns a cgroups "path" for the given container.
// Note that the container must be running.  Otherwise, an error
// is returned.
// NOTE: only call this when owning the container's lock.
func (c *Container) cGroupPath() (string, error) {
	if c.config.NoCgroups || c.config.CgroupsMode == "disabled" {
		return "", fmt.Errorf("this container is not creating cgroups: %w", define.ErrNoCgroups)
	}
	if c.state.State != define.ContainerStateRunning && c.state.State != define.ContainerStatePaused {
		return "", fmt.Errorf("cannot get cgroup path unless container %s is running: %w", c.ID(), define.ErrCtrStopped)
	}

	// Read /proc/{PID}/cgroup and find the *longest* cgroup entry.  That's
	// needed to account for hacks in cgroups v1, where each line in the
	// file could potentially point to a cgroup.  The longest one, however,
	// is the libpod-specific one we're looking for.
	//
	// See #8397 on the need for the longest-path look up.
	//
	// And another workaround for containers running systemd as the payload.
	// containers running systemd moves themselves into a child subgroup of
	// the named systemd cgroup hierarchy.  Ignore any named cgroups during
	// the lookup.
	// See #10602 for more details.
	procPath := fmt.Sprintf("/proc/%d/cgroup", c.state.PID)
	lines, err := os.ReadFile(procPath)
	if err != nil {
		// If the file doesn't exist, it means the container could have been terminated
		// so report it.  Also check for ESRCH, which means the container could have been
		// terminated after the file under /proc was opened but before it was read.
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
			return "", fmt.Errorf("cannot get cgroup path unless container %s is running: %w", c.ID(), define.ErrCtrStopped)
		}
		return "", err
	}

	var cgroupPath string
	for line := range bytes.SplitSeq(lines, []byte("\n")) {
		// skip last empty line
		if len(line) == 0 {
			continue
		}
		// cgroups(7) nails it down to three fields with the 3rd
		// pointing to the cgroup's path which works both on v1 and v2.
		fields := bytes.Split(line, []byte(":"))
		if len(fields) != 3 {
			logrus.Debugf("Error parsing cgroup: expected 3 fields but got %d: %s", len(fields), procPath)
			continue
		}
		// Ignore named cgroups like name=systemd.
		if bytes.Contains(fields[1], []byte("=")) {
			continue
		}
		path := string(fields[2])
		if len(path) > len(cgroupPath) {
			cgroupPath = path
		}
	}

	if len(cgroupPath) == 0 {
		return "", fmt.Errorf("could not find any cgroup in %q", procPath)
	}

	cgroupManager := c.CgroupManager()
	switch {
	case c.config.CgroupsMode == cgroupSplit:
		name := fmt.Sprintf("/libpod-payload-%s/", c.ID())
		if index := strings.LastIndex(cgroupPath, name); index >= 0 {
			return cgroupPath[:index+len(name)-1], nil
		}
	case cgroupManager == config.CgroupfsCgroupsManager:
		name := fmt.Sprintf("/libpod-%s/", c.ID())
		if index := strings.LastIndex(cgroupPath, name); index >= 0 {
			return cgroupPath[:index+len(name)-1], nil
		}
	case cgroupManager == config.SystemdCgroupsManager:
		// When running under systemd, try to detect the scope that was requested
		// to be created.  It improves the heuristic since we report the first
		// cgroup that was created instead of the cgroup where PID 1 might have
		// moved to.
		name := fmt.Sprintf("/libpod-%s.scope/", c.ID())
		if index := strings.LastIndex(cgroupPath, name); index >= 0 {
			return cgroupPath[:index+len(name)-1], nil
		}
	}

	return cgroupPath, nil
}

// RootFsSize returns the root FS size of the container
func (c *Container) RootFsSize() (int64, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return -1, fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	return c.rootFsSize()
}

// RWSize returns the rw size of the container
func (c *Container) RWSize() (int64, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return -1, fmt.Errorf("updating container %s state: %w", c.ID(), err)
		}
	}
	return c.rwSize()
}

// IDMappings returns the UID/GID mapping used for the container
func (c *Container) IDMappings() storage.IDMappingOptions {
	return c.config.IDMappings
}

// RootUID returns the root user mapping from container
func (c *Container) RootUID() int {
	if len(c.config.IDMappings.UIDMap) == 1 && c.config.IDMappings.UIDMap[0].Size == 1 {
		return c.config.IDMappings.UIDMap[0].HostID
	}
	for _, uidmap := range c.config.IDMappings.UIDMap {
		if uidmap.ContainerID == 0 {
			return uidmap.HostID
		}
	}
	return 0
}

// RootGID returns the root user mapping from container
func (c *Container) RootGID() int {
	if len(c.config.IDMappings.GIDMap) == 1 && c.config.IDMappings.GIDMap[0].Size == 1 {
		return c.config.IDMappings.GIDMap[0].HostID
	}
	for _, gidmap := range c.config.IDMappings.GIDMap {
		if gidmap.ContainerID == 0 {
			return gidmap.HostID
		}
	}
	return 0
}

// IsInfra returns whether the container is an infra container
func (c *Container) IsInfra() bool {
	return c.config.IsInfra
}

// IsDefaultInfra returns whether the container is a default infra container generated directly by podman
func (c *Container) IsDefaultInfra() bool {
	return c.config.IsDefaultInfra
}

// IsInitCtr returns whether the container is an init container
func (c *Container) IsInitCtr() bool {
	return len(c.config.InitContainerType) > 0
}

// IsReadOnly returns whether the container is running in read-only mode
func (c *Container) IsReadOnly() bool {
	return c.config.Spec.Root.Readonly
}

// NetworkDisabled returns whether the container is running with a disabled network
func (c *Container) NetworkDisabled() (bool, error) {
	if c.config.NetNsCtr != "" {
		container, err := c.runtime.state.Container(c.config.NetNsCtr)
		if err != nil {
			return false, err
		}
		return container.NetworkDisabled()
	}
	return networkDisabled(c)
}

func (c *Container) HostNetwork() bool {
	if c.config.CreateNetNS || c.config.NetNsCtr != "" {
		return false
	}
	if c.config.Spec.Linux != nil {
		for _, ns := range c.config.Spec.Linux.Namespaces {
			if ns.Type == spec.NetworkNamespace {
				return false
			}
		}
	}
	return true
}

// HasHealthCheck returns bool as to whether there is a health check
// defined for the container
func (c *Container) HasHealthCheck() bool {
	return c.config.HealthCheckConfig != nil
}

// HealthCheckConfig returns the command and timing attributes of the health check
func (c *Container) HealthCheckConfig() *manifest.Schema2HealthConfig {
	return c.config.HealthCheckConfig
}

func (c *Container) HealthCheckLogDestination() string {
	if c.config.HealthLogDestination == nil {
		return define.DefaultHealthCheckLocalDestination
	}
	return *c.config.HealthLogDestination
}

func (c *Container) HealthCheckMaxLogCount() uint {
	if c.config.HealthMaxLogCount == nil {
		return define.DefaultHealthMaxLogCount
	}
	return *c.config.HealthMaxLogCount
}

func (c *Container) HealthCheckMaxLogSize() uint {
	if c.config.HealthMaxLogSize == nil {
		return define.DefaultHealthMaxLogSize
	}
	return *c.config.HealthMaxLogSize
}

// AutoRemove indicates whether the container will be removed after it is executed
func (c *Container) AutoRemove() bool {
	spec := c.config.Spec
	if spec.Annotations == nil {
		return false
	}
	return spec.Annotations[define.InspectAnnotationAutoremove] == define.InspectResponseTrue
}

// AutoRemoveImage indicates that the container will automatically remove the
// image it is using after it exits and is removed.
// Only allowed if AutoRemove is true.
func (c *Container) AutoRemoveImage() bool {
	spec := c.config.Spec
	if spec.Annotations == nil {
		return false
	}
	return spec.Annotations[define.InspectAnnotationAutoremoveImage] == define.InspectResponseTrue
}

// Timezone returns the timezone configured inside the container.
// Local means it has the same timezone as the host machine
func (c *Container) Timezone() string {
	return c.config.Timezone
}

// Umask returns the Umask bits configured inside the container.
func (c *Container) Umask() string {
	return c.config.Umask
}

// Secrets return the secrets in the container
func (c *Container) Secrets() []*ContainerSecret {
	return c.config.Secrets
}

// Networks gets all the networks this container is connected to.
// Please do NOT use ctr.config.Networks, as this can be changed from those
// values at runtime via network connect and disconnect.
// Returned array of network names or error.
func (c *Container) Networks() ([]string, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	networks, err := c.networks()
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(networks))

	for name := range networks {
		names = append(names, name)
	}

	return names, nil
}

// NetworkMode gets the configured network mode for the container.
// Get actual value from the database
func (c *Container) NetworkMode() string {
	networkMode := ""
	ctrSpec := c.config.Spec

	switch {
	case c.config.CreateNetNS:
		// We actually store the network
		// mode for Slirp and Bridge, so
		// we can just use that
		networkMode = string(c.config.NetMode)
	case c.config.NetNsCtr != "":
		networkMode = fmt.Sprintf("container:%s", c.config.NetNsCtr)
	default:
		// Find the spec's network namespace.
		// If there is none, it's host networking.
		// If there is one and it has a path, it's "ns:".
		foundNetNS := false
		for _, ns := range ctrSpec.Linux.Namespaces {
			if ns.Type == spec.NetworkNamespace {
				foundNetNS = true
				if ns.Path != "" {
					networkMode = fmt.Sprintf("ns:%s", ns.Path)
				} else {
					// We're making a network ns,  but not
					// configuring with Slirp or CNI. That
					// means it's --net=none
					networkMode = "none"
				}
				break
			}
		}
		if !foundNetNS {
			networkMode = "host"
		}
	}
	return networkMode
}

// Unlocked accessor for networks
func (c *Container) networks() (map[string]types.PerNetworkOptions, error) {
	return c.runtime.state.GetNetworks(c)
}

// getInterfaceByName returns a formatted interface name for a given
// network along with a bool as to whether the network existed
func (d ContainerNetworkDescriptions) getInterfaceByName(networkName string) (string, bool) {
	val, exists := d[networkName]
	if !exists {
		return "", exists
	}
	return fmt.Sprintf("eth%d", val), exists
}

// GetNetworkStatus returns the current network status for this container.
// This returns a map without deep copying which means this should only ever
// be used as read only access, do not modify this status.
func (c *Container) GetNetworkStatus() (map[string]types.StatusBlock, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}
	return c.getNetworkStatus(), nil
}

// getNetworkStatus get the current network status from the state. This function
// should be used instead of reading c.state.NetworkStatus directly.
func (c *Container) getNetworkStatus() map[string]types.StatusBlock {
	return c.state.NetworkStatus
}

func (c *Container) NamespaceMode(ns spec.LinuxNamespaceType, ctrSpec *spec.Spec) string {
	switch ns {
	case spec.UTSNamespace:
		if c.config.UTSNsCtr != "" {
			return fmt.Sprintf("container:%s", c.config.UTSNsCtr)
		}
	case spec.CgroupNamespace:
		if c.config.CgroupNsCtr != "" {
			return fmt.Sprintf("container:%s", c.config.CgroupNsCtr)
		}
	case spec.IPCNamespace:
		if c.config.IPCNsCtr != "" {
			return fmt.Sprintf("container:%s", c.config.IPCNsCtr)
		}
	case spec.PIDNamespace:
		if c.config.PIDNsCtr != "" {
			return fmt.Sprintf("container:%s", c.config.PIDNsCtr)
		}
	case spec.UserNamespace:
		if c.config.UserNsCtr != "" {
			return fmt.Sprintf("container:%s", c.config.UserNsCtr)
		}
	case spec.NetworkNamespace:
		if c.config.NetNsCtr != "" {
			return fmt.Sprintf("container:%s", c.config.NetNsCtr)
		}
	case spec.MountNamespace:
		if c.config.MountNsCtr != "" {
			return fmt.Sprintf("container:%s", c.config.MountNsCtr)
		}
	}

	if ctrSpec.Linux != nil {
		// Locate the spec's given namespace.
		// If there is none, it's namespace=host.
		// If there is one and it has a path, it's "ns:".
		// If there is no path, it's default - the empty string.
		for _, availableNS := range ctrSpec.Linux.Namespaces {
			if availableNS.Type == ns {
				if availableNS.Path != "" {
					return fmt.Sprintf("ns:%s", availableNS.Path)
				}
				return "private"
			}
		}
	}
	return "host"
}
