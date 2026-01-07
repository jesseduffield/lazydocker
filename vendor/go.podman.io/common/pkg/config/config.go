package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	units "github.com/docker/go-units"
	selinux "github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/internal/attributedstring"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/unshare"
)

const (
	// userOverrideContainersConfig holds the containers config path overridden by the rootless user.
	userOverrideContainersConfig = ".config/" + _configPath
	// Token prefix for looking for helper binary under $BINDIR.
	bindirPrefix = "$BINDIR"
)

var validImageVolumeModes = []string{"anonymous", "tmpfs", "ignore"}

// ProxyEnv is a list of Proxy Environment variables.
var ProxyEnv = []string{
	"http_proxy",
	"https_proxy",
	"ftp_proxy",
	"no_proxy",
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"FTP_PROXY",
	"NO_PROXY",
}

// Config contains configuration options for container tools.
type Config struct {
	// Containers specify settings that configure how containers will run ont the system
	Containers ContainersConfig `toml:"containers"`
	// Engine specifies how the container engine based on Engine will run
	Engine EngineConfig `toml:"engine"`
	// Machine specifies configurations of podman machine VMs
	Machine MachineConfig `toml:"machine"`
	// Network section defines the configuration of CNI Plugins
	Network NetworkConfig `toml:"network"`
	// Secret section defines configurations for the secret management
	Secrets SecretConfig `toml:"secrets"`
	// ConfigMap section defines configurations for the configmaps management
	ConfigMaps ConfigMapConfig `toml:"configmaps"`
	// Farms defines configurations for the buildfarm farms
	Farms FarmConfig `toml:"farms"`
	// Podmansh defined configurations for the podman shell
	Podmansh PodmanshConfig `toml:"podmansh"`

	loadedModules []string // only used at runtime to store which modules were loaded
}

// ContainersConfig represents the "containers" TOML config table
// containers global options for containers tools.
type ContainersConfig struct {
	// Devices to add to all containers
	Devices attributedstring.Slice `toml:"devices,omitempty"`

	// Volumes to add to all containers
	Volumes attributedstring.Slice `toml:"volumes,omitempty"`

	// ApparmorProfile is the apparmor profile name which is used as the
	// default for the runtime.
	ApparmorProfile string `toml:"apparmor_profile,omitempty"`

	// Annotation to add to all containers
	Annotations attributedstring.Slice `toml:"annotations,omitempty"`

	// BaseHostsFile is the path to a hosts file, the entries from this file
	// are added to the containers hosts file. As special value "image" is
	// allowed which uses the /etc/hosts file from within the image and "none"
	// which uses no base file at all. If it is empty we should default
	// to /etc/hosts.
	BaseHostsFile string `toml:"base_hosts_file,omitempty"`

	// Default way to create a cgroup namespace for the container
	CgroupNS string `toml:"cgroupns,omitempty"`

	// Default cgroup configuration
	Cgroups string `toml:"cgroups,omitempty"`

	// CgroupConf entries specifies a list of cgroup files to write to and their values. For example
	// "memory.high=1073741824" sets the memory.high limit to 1GB.
	CgroupConf attributedstring.Slice `toml:"cgroup_conf,omitempty"`

	// When no hostname is set for a container, use the container's name, with
	// characters not valid for a hostname removed, as the hostname instead of
	// the first 12 characters of the container's ID. Containers not running
	// in a private UTS namespace will have their hostname set to the host's
	// hostname regardless of this setting.
	ContainerNameAsHostName bool `toml:"container_name_as_hostname,omitempty"`

	// Capabilities to add to all containers.
	DefaultCapabilities attributedstring.Slice `toml:"default_capabilities,omitempty"`

	// Sysctls to add to all containers.
	DefaultSysctls attributedstring.Slice `toml:"default_sysctls,omitempty"`

	// DefaultUlimits specifies the default ulimits to apply to containers
	DefaultUlimits attributedstring.Slice `toml:"default_ulimits,omitempty"`

	// DefaultMountsFile is the path to the default mounts file for testing
	DefaultMountsFile string `toml:"-"`

	// DNSServers set default DNS servers.
	DNSServers attributedstring.Slice `toml:"dns_servers,omitempty"`

	// DNSOptions set default DNS options.
	DNSOptions attributedstring.Slice `toml:"dns_options,omitempty"`

	// DNSSearches set default DNS search domains.
	DNSSearches attributedstring.Slice `toml:"dns_searches,omitempty"`

	// EnableKeyring tells the container engines whether to create
	// a kernel keyring for use within the container
	EnableKeyring bool `toml:"keyring,omitempty"`

	// EnableLabeling tells the container engines whether to use MAC
	// Labeling to separate containers (SELinux)
	EnableLabeling bool `toml:"label,omitempty"`

	// EnableLabeledUsers indicates whether to enforce confined users with
	// containers on SELinux systems. This option causes containers to
	// maintain the current user and role field of the calling process.
	// Otherwise containers run with user system_u, and the role system_r.
	EnableLabeledUsers bool `toml:"label_users,omitempty"`

	// Env is the environment variable list for container process.
	Env attributedstring.Slice `toml:"env,omitempty"`

	// EnvHost Pass all host environment variables into the container.
	EnvHost bool `toml:"env_host,omitempty"`

	// HostContainersInternalIP is used to set a specific host.containers.internal ip.
	HostContainersInternalIP string `toml:"host_containers_internal_ip,omitempty"`

	// HTTPProxy is the proxy environment variable list to apply to container process
	HTTPProxy bool `toml:"http_proxy,omitempty"`

	// Init tells container runtimes whether to run init inside the
	// container that forwards signals and reaps processes.
	Init bool `toml:"init,omitempty"`

	// InitPath is the path for init to run if the Init bool is enabled
	//
	// Deprecated: Do not use this field directly use conf.FindInitBinary() instead.
	InitPath string `toml:"init_path,omitempty"`

	// InterfaceName tells container runtimes how to set interface names
	// inside containers.
	// The only valid value at the moment is "device" that indicates the
	// interface name should be set as the network_interface name from
	// the network config.
	InterfaceName string `toml:"interface_name,omitempty"`

	// IPCNS way to create a ipc namespace for the container
	IPCNS string `toml:"ipcns,omitempty"`

	// LogDriver  for the container.  For example: k8s-file and journald
	LogDriver string `toml:"log_driver,omitempty"`

	// LogPath is the path to the container log file.
	LogPath string `toml:"log_path,omitempty"`

	// LogSizeMax is the maximum number of bytes after which the log file
	// will be truncated. It can be expressed as a human-friendly string
	// that is parsed to bytes.
	// Negative values indicate that the log file won't be truncated.
	LogSizeMax int64 `toml:"log_size_max,omitempty,omitzero"`

	// Specifies default format tag for container log messages.
	// This is useful for creating a specific tag for container log messages.
	// Containers logs default to truncated container ID as a tag.
	LogTag string `toml:"log_tag,omitempty"`

	// Mount to add to all containers
	Mounts attributedstring.Slice `toml:"mounts,omitempty"`

	// NetNS indicates how to create a network namespace for the container
	NetNS string `toml:"netns,omitempty"`

	// NoHosts tells container engine whether to create its own /etc/hosts
	NoHosts bool `toml:"no_hosts,omitempty"`

	// OOMScoreAdj tunes the host's OOM preferences for containers
	// (accepts values from -1000 to 1000).
	OOMScoreAdj *int `toml:"oom_score_adj,omitempty"`

	// PidsLimit is the number of processes each container is restricted to
	// by the cgroup process number controller.
	PidsLimit int64 `toml:"pids_limit,omitempty,omitzero"`

	// PidNS indicates how to create a pid namespace for the container
	PidNS string `toml:"pidns,omitempty"`

	// Copy the content from the underlying image into the newly created
	// volume when the container is created instead of when it is started.
	// If false, the container engine will not copy the content until
	// the container is started. Setting it to true may have negative
	// performance implications.
	PrepareVolumeOnCreate bool `toml:"prepare_volume_on_create,omitempty"`

	// Give extended privileges to all containers. A privileged container
	// turns off the security features that isolate the container from the
	// host. Dropped Capabilities, limited devices, read-only mount points,
	// Apparmor/SELinux separation, and Seccomp filters are all disabled.
	// Due to the disabled security features the privileged field should
	// almost never be set as containers can easily break out of
	// confinment.
	//
	// Containers running in a user namespace (e.g., rootless containers)
	// cannot have more privileges than the user that launched them.
	Privileged bool `toml:"privileged,omitempty"`

	// ReadOnly causes engine to run all containers with root file system mounted read-only
	ReadOnly bool `toml:"read_only,omitempty"`

	// SeccompProfile is the seccomp.json profile path which is used as the
	// default for the runtime.
	SeccompProfile string `toml:"seccomp_profile,omitempty"`

	// ShmSize holds the size of /dev/shm.
	ShmSize string `toml:"shm_size,omitempty"`

	// TZ sets the timezone inside the container
	TZ string `toml:"tz,omitempty"`

	// Umask is the umask inside the container.
	Umask string `toml:"umask,omitempty"`

	// UTSNS indicates how to create a UTS namespace for the container
	UTSNS string `toml:"utsns,omitempty"`

	// UserNS indicates how to create a User namespace for the container
	UserNS string `toml:"userns,omitempty"`

	// UserNSSize how many UIDs to allocate for automatically created UserNS
	// Deprecated: no user of this field is known.
	UserNSSize int `toml:"userns_size,omitempty,omitzero"`
}

// EngineConfig contains configuration options used to set up a engine runtime.
type EngineConfig struct {
	// CgroupCheck indicates the configuration has been rewritten after an
	// upgrade to Fedora 31 to change the default OCI runtime for cgroupv2v2.
	CgroupCheck bool `toml:"cgroup_check,omitempty"`

	// CGroupManager is the CGroup Manager to use Valid values are "cgroupfs"
	// and "systemd".
	CgroupManager string `toml:"cgroup_manager,omitempty"`

	// ConmonEnvVars are environment variables to pass to the Conmon binary
	// when it is launched.
	ConmonEnvVars attributedstring.Slice `toml:"conmon_env_vars,omitempty"`

	// ConmonPath is the path to the Conmon binary used for managing containers.
	// The first path pointing to a valid file will be used.
	ConmonPath attributedstring.Slice `toml:"conmon_path,omitempty"`

	// ConmonRsPath is the path to the Conmon-rs binary used for managing containers.
	// The first path pointing to a valid file will be used.
	ConmonRsPath attributedstring.Slice `toml:"conmonrs_path,omitempty"`

	// CompatAPIEnforceDockerHub enforces using docker.io for completing
	// short names in Podman's compatibility REST API.  Note that this will
	// ignore unqualified-search-registries and short-name aliases defined
	// in containers-registries.conf(5).
	CompatAPIEnforceDockerHub bool `toml:"compat_api_enforce_docker_hub,omitempty"`

	// ComposeProviders specifies one or more external providers for the
	// compose command.  The first found provider is used for execution.
	// Can be an absolute and relative path or a (file) name.  Make sure to
	// expand the return items via `os.ExpandEnv`.
	ComposeProviders attributedstring.Slice `toml:"compose_providers,omitempty"`

	// ComposeWarningLogs emits logs on each invocation of the compose
	// command indicating that an external compose provider is being
	// executed.
	ComposeWarningLogs bool `toml:"compose_warning_logs,omitempty"`

	// DBBackend is the database backend to be used by Podman.
	DBBackend string `toml:"database_backend,omitempty"`

	// DetachKeys is the sequence of keys used to detach a container.
	DetachKeys string `toml:"detach_keys,omitempty"`

	// EnablePortReservation determines whether engine will reserve ports on the
	// host when they are forwarded to containers. When enabled, when ports are
	// forwarded to containers, they are held open by conmon as long as the
	// container is running, ensuring that they cannot be reused by other
	// programs on the host. However, this can cause significant memory usage if
	// a container has many ports forwarded to it. Disabling this can save
	// memory.
	EnablePortReservation bool `toml:"enable_port_reservation,omitempty"`

	// Environment variables to be used when running the container engine (e.g., Podman, Buildah). For example "http_proxy=internal.proxy.company.com"
	Env attributedstring.Slice `toml:"env,omitempty"`

	// EventsLogFilePath is where the events log is stored.
	EventsLogFilePath string `toml:"events_logfile_path,omitempty"`

	// EventsLogFileMaxSize sets the maximum size for the events log. When the limit is exceeded,
	// the logfile is rotated and the old one is deleted.
	EventsLogFileMaxSize eventsLogMaxSize `toml:"events_logfile_max_size,omitzero"`

	// EventsLogger determines where events should be logged.
	EventsLogger string `toml:"events_logger,omitempty"`

	// EventsContainerCreateInspectData creates a more verbose
	// container-create event which includes a JSON payload with detailed
	// information about the container.
	EventsContainerCreateInspectData bool `toml:"events_container_create_inspect_data,omitempty"`

	// graphRoot internal stores the location of the graphroot
	graphRoot string

	// HealthcheckEvents is set to indicate whenever podman should log healthcheck events.
	// With many running healthcheck on short interval Podman will spam the event log a lot.
	// Because this event is optional and only useful to external consumers that may want to
	// know when a healthcheck is run or failed allow users to turn it off by setting it to false.
	// Default is true.
	HealthcheckEvents bool `toml:"healthcheck_events,omitempty"`

	// HelperBinariesDir is a list of directories which are used to search for
	// helper binaries.
	HelperBinariesDir attributedstring.Slice `toml:"helper_binaries_dir,omitempty"`

	// configuration files. When the same filename is present in
	// multiple directories, the file in the directory listed last in
	// this slice takes precedence.
	HooksDir attributedstring.Slice `toml:"hooks_dir,omitempty"`

	// Location of CDI configuration files. These define mounts devices and
	// other configs according to the CDI spec. In particular this is used
	// for GPU passthrough.
	CdiSpecDirs attributedstring.Slice `toml:"cdi_spec_dirs,omitempty"`

	// ImageBuildFormat (DEPRECATED) indicates the default image format to
	// building container images. Should use ImageDefaultFormat
	ImageBuildFormat string `toml:"image_build_format,omitempty"`

	// ImageDefaultTransport is the default transport method used to fetch
	// images.
	ImageDefaultTransport string `toml:"image_default_transport,omitempty"`

	// ImageParallelCopies indicates the maximum number of image layers
	// to be copied simultaneously. If this is zero, container engines
	// will fall back to containers/image defaults.
	ImageParallelCopies uint `toml:"image_parallel_copies,omitempty,omitzero"`

	// ImageDefaultFormat specified the manifest Type (oci, v2s2, or v2s1)
	// to use when pulling, pushing, building container images. By default
	// image pulled and pushed match the format of the source image.
	// Building/committing defaults to OCI.
	ImageDefaultFormat string `toml:"image_default_format,omitempty"`

	// ImageVolumeMode Tells container engines how to handle the built-in
	// image volumes.  Acceptable values are "bind", "tmpfs", and "ignore".
	ImageVolumeMode string `toml:"image_volume_mode,omitempty"`

	// InfraCommand is the command run to start up a pod infra container.
	InfraCommand string `toml:"infra_command,omitempty"`

	// InfraImage is the image a pod infra container will use to manage
	// namespaces.
	InfraImage string `toml:"infra_image,omitempty"`

	// InitPath is the path to the container-init binary.
	//
	// Deprecated: Do not use this field directly use conf.FindInitBinary() instead.
	InitPath string `toml:"init_path,omitempty"`

	// KubeGenerateType sets the Kubernetes kind/specification to generate by default
	// with the podman kube generate command
	KubeGenerateType string `toml:"kube_generate_type,omitempty"`

	// LockType is the type of locking to use.
	LockType string `toml:"lock_type,omitempty"`

	// MultiImageArchive - if true, the container engine allows for storing
	// archives (e.g., of the docker-archive transport) with multiple
	// images.  By default, Podman creates single-image archives.
	MultiImageArchive bool `toml:"multi_image_archive,omitempty"`

	// Namespace is the engine namespace to use. Namespaces are used to create
	// scopes to separate containers and pods in the state. When namespace is
	// set, engine will only view containers and pods in the same namespace. All
	// containers and pods created will default to the namespace set here. A
	// namespace of "", the empty string, is equivalent to no namespace, and all
	// containers and pods will be visible. The default namespace is "".
	Namespace string `toml:"namespace,omitempty"`

	// NetworkCmdPath is the path to the slirp4netns binary.
	NetworkCmdPath string `toml:"network_cmd_path,omitempty"`

	// NetworkCmdOptions is the default options to pass to the slirp4netns binary.
	// For example "allow_host_loopback=true"
	NetworkCmdOptions attributedstring.Slice `toml:"network_cmd_options,omitempty"`

	// NoPivotRoot sets whether to set no-pivot-root in the OCI runtime.
	NoPivotRoot bool `toml:"no_pivot_root,omitempty"`

	// NumLocks is the number of locks to make available for containers and
	// pods.
	NumLocks uint32 `toml:"num_locks,omitempty,omitzero"`

	// OCIRuntime is the OCI runtime to use.
	OCIRuntime string `toml:"runtime,omitempty"`

	// OCIRuntimes are the set of configured OCI runtimes (default is runc).
	OCIRuntimes map[string][]string `toml:"runtimes,omitempty"`

	// OCIRuntimesFlags are the set of configured OCI runtimes' flags
	OCIRuntimesFlags map[string][]string `toml:"runtimes_flags,omitempty"`

	// PlatformToOCIRuntime requests specific OCI runtime for a specified platform of image.
	PlatformToOCIRuntime map[string]string `toml:"platform_to_oci_runtime,omitempty"`

	// PodExitPolicy determines the behaviour when the last container of a pod exits.
	PodExitPolicy PodExitPolicy `toml:"pod_exit_policy,omitempty"`

	// PullPolicy determines whether to pull image before creating or running a container
	// default is "missing"
	PullPolicy string `toml:"pull_policy,omitempty"`

	// Indicates whether the application should be running in Remote mode
	Remote bool `toml:"remote,omitempty"`

	// Number of times to retry pulling/pushing images in case of failure
	Retry uint `toml:"retry,omitempty"`

	// Delay between retries in case pulling/pushing image fails
	// If set, container engines will retry at the set interval,
	// otherwise they delay 2 seconds and then exponentially back off.
	RetryDelay string `toml:"retry_delay,omitempty"`

	// RemoteURI is deprecated, see ActiveService
	// RemoteURI containers connection information used to connect to remote system.
	RemoteURI string `toml:"remote_uri,omitempty"`

	// RemoteIdentity is deprecated, ServiceDestinations
	// RemoteIdentity key file for RemoteURI
	RemoteIdentity string `toml:"remote_identity,omitempty"`

	// ActiveService index to Destinations added v2.0.3
	ActiveService string `toml:"active_service,omitempty"`

	// Add existing instances with requested compression algorithms to manifest list
	AddCompression attributedstring.Slice `toml:"add_compression,omitempty"`

	// ServiceDestinations mapped by service Names
	ServiceDestinations map[string]Destination `toml:"service_destinations,omitempty"`

	// SSHConfig contains the ssh config file path if not the default
	SSHConfig string `toml:"ssh_config,omitempty"`

	// RuntimePath is the path to OCI runtime binary for launching containers.
	// The first path pointing to a valid file will be used This is used only
	// when there are no OCIRuntime/OCIRuntimes defined.  It is used only to be
	// backward compatible with older versions of Podman.
	RuntimePath attributedstring.Slice `toml:"runtime_path,omitempty"`

	// RuntimeSupportsJSON is the list of the OCI runtimes that support
	// --format=json.
	RuntimeSupportsJSON attributedstring.Slice `toml:"runtime_supports_json,omitempty"`

	// RuntimeSupportsNoCgroups is a list of OCI runtimes that support
	// running containers without CGroups.
	RuntimeSupportsNoCgroups attributedstring.Slice `toml:"runtime_supports_nocgroup,omitempty"`

	// RuntimeSupportsKVM is a list of OCI runtimes that support
	// KVM separation for containers.
	RuntimeSupportsKVM attributedstring.Slice `toml:"runtime_supports_kvm,omitempty"`

	// SetOptions contains a subset of config options. It's used to indicate if
	// a given option has either been set by the user or by the parsed
	// configuration file. If not, the corresponding option might be
	// overwritten by values from the database. This behavior guarantees
	// backwards compat with older version of libpod and Podman.
	SetOptions

	// SignaturePolicyPath is the path to a signature policy to use for
	// validating images. If left empty, the containers/image default signature
	// policy will be used.
	SignaturePolicyPath string `toml:"-"`

	// SDNotify tells container engine to allow containers to notify the host systemd of
	// readiness using the SD_NOTIFY mechanism.
	SDNotify bool `toml:"-"`

	// ServiceTimeout is the number of seconds to wait without a connection
	// before the `podman system service` times out and exits
	ServiceTimeout uint `toml:"service_timeout,omitempty,omitzero"`

	// StaticDir is the path to a persistent directory to store container
	// files.
	StaticDir string `toml:"static_dir,omitempty"`

	// StopTimeout is the number of seconds to wait for container to exit
	// before sending kill signal.
	StopTimeout uint `toml:"stop_timeout,omitempty,omitzero"`

	// ExitCommandDelay is the number of seconds to wait for the exit
	// command to be send to the API process on the server.
	ExitCommandDelay uint `toml:"exit_command_delay,omitempty,omitzero"`

	// ImageCopyTmpDir is the default location for storing temporary
	// container image content,  Can be overridden with the TMPDIR
	// environment variable.  If you specify "storage", then the
	// location of the container/storage tmp directory will be used.
	ImageCopyTmpDir string `toml:"image_copy_tmp_dir,omitempty"`

	// TmpDir is the path to a temporary directory to store per-boot container
	// files. Must be stored in a tmpfs.
	TmpDir string `toml:"tmp_dir,omitempty"`

	// VolumePath is the default location that named volumes will be created
	// under. This convention is followed by the default volume driver, but
	// may not be by other drivers.
	VolumePath string `toml:"volume_path,omitempty"`

	// VolumePluginTimeout sets the default timeout, in seconds, for
	// operations that must contact a volume plugin. Plugins are external
	// programs accessed via REST API; this sets a timeout for requests to
	// that API.
	// A value of 0 is treated as no timeout.
	VolumePluginTimeout uint `toml:"volume_plugin_timeout,omitempty,omitzero"`

	// VolumePlugins is a set of plugins that can be used as the backend for
	// Podman named volumes. Each volume is specified as a name (what Podman
	// will refer to the plugin as) mapped to a path, which must point to a
	// Unix socket that conforms to the Volume Plugin specification.
	VolumePlugins map[string]string `toml:"volume_plugins,omitempty"`

	// ChownCopiedFiles tells the container engine whether to chown files copied
	// into a container to the container's primary uid/gid.
	ChownCopiedFiles bool `toml:"chown_copied_files,omitempty"`

	// CompressionFormat is the compression format used to compress image layers.
	CompressionFormat string `toml:"compression_format,omitempty"`

	// CompressionLevel is the compression level used to compress image layers.
	CompressionLevel *int `toml:"compression_level,omitempty"`

	// PodmanshTimeout is the number of seconds to wait for podmansh logins.
	// In other words, the timeout for the `podmansh` container to be in running
	// state.
	// Deprecated: Use podmansh.Timeout instead. podmansh.Timeout has precedence.
	PodmanshTimeout uint `toml:"podmansh_timeout,omitempty,omitzero"`
}

// SetOptions contains a subset of options in a Config. It's used to indicate if
// a given option has either been set by the user or by a parsed engine
// configuration file. If not, the corresponding option might be overwritten by
// values from the database. This behavior guarantees backwards compat with
// older version of libpod and Podman.
type SetOptions struct {
	// StorageConfigRunRootSet indicates if the RunRoot has been explicitly set
	// by the config or by the user. It's required to guarantee backwards
	// compatibility with older versions of libpod for which we must query the
	// database configuration. Not included in the on-disk config.
	StorageConfigRunRootSet bool `toml:"-"`

	// StorageConfigGraphRootSet indicates if the RunRoot has been explicitly
	// set by the config or by the user. It's required to guarantee backwards
	// compatibility with older versions of libpod for which we must query the
	// database configuration. Not included in the on-disk config.
	StorageConfigGraphRootSet bool `toml:"-"`

	// StorageConfigGraphDriverNameSet indicates if the GraphDriverName has been
	// explicitly set by the config or by the user. It's required to guarantee
	// backwards compatibility with older versions of libpod for which we must
	// query the database configuration. Not included in the on-disk config.
	StorageConfigGraphDriverNameSet bool `toml:"-"`
}

// NetworkConfig represents the "network" TOML config table.
type NetworkConfig struct {
	// NetworkBackend determines what backend should be used for Podman's
	// networking.
	NetworkBackend string `toml:"network_backend,omitempty"`

	// CNIPluginDirs is where CNI plugin binaries are stored.
	CNIPluginDirs attributedstring.Slice `toml:"cni_plugin_dirs,omitempty"`

	// NetavarkPluginDirs is a list of directories which contain netavark plugins.
	NetavarkPluginDirs attributedstring.Slice `toml:"netavark_plugin_dirs,omitempty"`

	// FirewallDriver is the firewall driver to be used
	FirewallDriver string `toml:"firewall_driver,omitempty"`

	// DefaultNetwork is the network name of the default network
	// to attach pods to.
	DefaultNetwork string `toml:"default_network,omitempty"`

	// DefaultSubnet is the subnet to be used for the default network.
	// If a network with the name given in DefaultNetwork is not present
	// then a new network using this subnet will be created.
	// Must be a valid IPv4 CIDR block.
	DefaultSubnet string `toml:"default_subnet,omitempty"`

	// DefaultSubnetPools is a list of subnets and size which are used to
	// allocate subnets automatically for podman network create.
	// It will iterate through the list and will pick the first free subnet
	// with the given size. This is only used for ipv4 subnets, ipv6 subnets
	// are always assigned randomly.
	DefaultSubnetPools []SubnetPool `toml:"default_subnet_pools,omitempty"`

	// DefaultRootlessNetworkCmd is used to set the default rootless network
	// program, either "slirp4nents" (default) or "pasta".
	DefaultRootlessNetworkCmd string `toml:"default_rootless_network_cmd,omitempty"`

	// NetworkConfigDir is where network configuration files are stored.
	NetworkConfigDir string `toml:"network_config_dir,omitempty"`

	// DNSBindPort is the port that should be used by dns forwarding daemon
	// for netavark rootful bridges with dns enabled. This can be necessary
	// when other dns forwarders run on the machine. 53 is used if unset.
	DNSBindPort uint16 `toml:"dns_bind_port,omitempty,omitzero"`

	// PastaOptions contains a default list of pasta(1) options that should
	// be used when running pasta.
	PastaOptions attributedstring.Slice `toml:"pasta_options,omitempty"`
}

type SubnetPool struct {
	// Base is a bigger subnet which will be used to allocate a subnet with
	// the given size.
	Base *types.IPNet `toml:"base,omitempty"`
	// Size is the CIDR for the new subnet. It must be equal or small
	// than the CIDR from the base subnet.
	Size int `toml:"size,omitempty"`
}

// SecretConfig represents the "secret" TOML config table.
type SecretConfig struct {
	// Driver specifies the secret driver to use.
	// Current valid value:
	//  * file
	//  * pass
	Driver string `toml:"driver,omitempty"`
	// Opts contains driver specific options
	Opts map[string]string `toml:"opts,omitempty"`
}

// ConfigMapConfig represents the "configmap" TOML config table
//
// revive does not like the name because the package is already called config
//
//nolint:revive
type ConfigMapConfig struct {
	// Driver specifies the configmap driver to use.
	// Current valid value:
	//  * file
	//  * pass
	Driver string `toml:"driver,omitempty"`
	// Opts contains driver specific options
	Opts map[string]string `toml:"opts,omitempty"`
}

// MachineConfig represents the "machine" TOML config table.
type MachineConfig struct {
	// Number of CPU's a machine is created with.
	CPUs uint64 `toml:"cpus,omitempty,omitzero"`
	// DiskSize is the size of the disk in GB created when init-ing a podman-machine VM
	DiskSize uint64 `toml:"disk_size,omitempty,omitzero"`
	// Image is the image used when init-ing a podman-machine VM
	Image string `toml:"image,omitempty"`
	// Memory in MB a machine is created with.
	Memory uint64 `toml:"memory,omitempty,omitzero"`
	// User to use for rootless podman when init-ing a podman machine VM
	User string `toml:"user,omitempty"`
	// Volumes are host directories mounted into the VM by default.
	Volumes attributedstring.Slice `toml:"volumes,omitempty"`
	// Provider is the virtualization provider used to run podman-machine VM
	Provider string `toml:"provider,omitempty"`
	// Rosetta is the flag to enable Rosetta in the podman-machine VM on Apple Silicon
	Rosetta bool `toml:"rosetta,omitempty"`
}

// FarmConfig represents the "farm" TOML config tables.
type FarmConfig struct {
	// Default is the default farm to be used when farming out builds
	Default string `json:",omitempty" toml:"default,omitempty"`
	// List is a map of farms created where key=farm-name and value=list of connections
	List map[string][]string `json:",omitempty" toml:"list,omitempty"`
}

// Destination represents destination for remote service.
type Destination struct {
	// URI, required. Example: ssh://root@example.com:22/run/podman/podman.sock
	URI string `toml:"uri"`

	// Identity file with ssh key, optional
	Identity string `json:",omitempty" toml:"identity,omitempty"`

	// Path to TLS client certificate PEM file, optional
	TLSCert string `json:",omitempty" toml:"tls_cert,omitempty"`
	// Path to TLS client certificate private key PEM file, optional
	TLSKey string `json:",omitempty" toml:"tls_key,omitempty"`
	// Path to TLS certificate authority PEM file, optional
	TLSCA string `json:",omitempty" toml:"tls_ca,omitempty"`

	// isMachine describes if the remote destination is a machine.
	IsMachine bool `json:",omitempty" toml:"is_machine,omitempty"`
}

// PodmanshConfig represents configuration for the podman shell.
type PodmanshConfig struct {
	// Shell to start in container, default: "/bin/sh"
	Shell string `toml:"shell,omitempty"`
	// Name of the container the podmansh user should join
	Container string `toml:"container,omitempty"`

	// Timeout is the number of seconds to wait for podmansh logins.
	// In other words, the timeout for the `podmansh` container to be in running
	// state.
	Timeout uint `toml:"timeout,omitempty,omitzero"`
}

// ImagePlatformToRuntime consumes the container image's os and arch and returns if
// any dedicated runtime was configured otherwise returns default runtime.
func (c *EngineConfig) ImagePlatformToRuntime(os string, arch string) string {
	platformString := os + "/" + arch
	if val, ok := c.PlatformToOCIRuntime[platformString]; ok {
		return val
	}
	return c.OCIRuntime
}

// CheckCgroupsAndAdjustConfig checks if we're running rootless with the systemd
// cgroup manager. In case the user session isn't available, we're switching the
// cgroup manager to cgroupfs.  Note, this only applies to rootless.
func (c *Config) CheckCgroupsAndAdjustConfig() {
	if !unshare.IsRootless() || c.Engine.CgroupManager != SystemdCgroupsManager {
		return
	}

	hasSession := false

	session, found := os.LookupEnv("DBUS_SESSION_BUS_ADDRESS")
	if !found {
		xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if xdgRuntimeDir == "" {
			if dir, err := homedir.GetRuntimeDir(); err == nil {
				xdgRuntimeDir = dir
			}
		}
		sessionAddr := filepath.Join(xdgRuntimeDir, "bus")
		if err := fileutils.Exists(sessionAddr); err == nil {
			sessionAddr, err = filepath.EvalSymlinks(sessionAddr)
			if err == nil {
				os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+sessionAddr)
				hasSession = true
			}
		}
	} else {
		for part := range strings.SplitSeq(session, ",") {
			if path, ok := strings.CutPrefix(part, "unix:path="); ok {
				err := fileutils.Exists(path)
				hasSession = err == nil
				break
			}
		}
	}

	if !hasSession && unshare.GetRootlessUID() != 0 {
		logrus.Warningf("The cgroupv2 manager is set to systemd but there is no systemd user session available")
		logrus.Warningf("For using systemd, you may need to log in using a user session")
		logrus.Warningf("Alternatively, you can enable lingering with: `loginctl enable-linger %d` (possibly as root)", unshare.GetRootlessUID())
		logrus.Warningf("Falling back to --cgroup-manager=cgroupfs")
		c.Engine.CgroupManager = CgroupfsCgroupsManager
	}
}

func (c *Config) addCAPPrefix() {
	caps := c.Containers.DefaultCapabilities.Get()
	newCaps := make([]string, 0, len(caps))
	for _, val := range caps {
		if !strings.HasPrefix(strings.ToLower(val), "cap_") {
			val = "CAP_" + strings.ToUpper(val)
		}
		newCaps = append(newCaps, val)
	}
	c.Containers.DefaultCapabilities.Set(newCaps)
}

// Validate is the main entry point for library configuration validation.
func (c *Config) Validate() error {
	if err := c.Containers.Validate(); err != nil {
		return fmt.Errorf("validating containers config: %w", err)
	}

	if !c.Containers.EnableLabeling {
		selinux.SetDisabled()
	}

	if err := c.Engine.Validate(); err != nil {
		return fmt.Errorf("validating engine configs: %w", err)
	}

	if err := c.Network.Validate(); err != nil {
		return fmt.Errorf("validating network configs %w", err)
	}

	return nil
}

// URI returns the URI Path to the machine image.
func (m *MachineConfig) URI() string {
	uri := m.Image
	for _, val := range []string{"$ARCH", "$arch"} {
		uri = strings.Replace(uri, val, runtime.GOARCH, 1)
	}
	for _, val := range []string{"$OS", "$os"} {
		uri = strings.Replace(uri, val, runtime.GOOS, 1)
	}
	return uri
}

func (c *EngineConfig) findRuntime() string {
	// Search for crun first followed by runc, runj, kata, runsc, ocijail
	for _, name := range []string{"crun", "runc", "runj", "kata", "runsc", "ocijail"} {
		for _, v := range c.OCIRuntimes[name] {
			if err := fileutils.Exists(v); err == nil {
				return name
			}
		}
		if path, err := exec.LookPath(name); err == nil {
			logrus.Debugf("Found default OCI runtime %s path via PATH environment variable", path)
			return name
		}
	}
	return ""
}

// Validate is the main entry point for Engine configuration validation
// It returns an `error` on validation failure, otherwise
// `nil`.
func (c *EngineConfig) Validate() error {
	if err := c.validatePaths(); err != nil {
		return err
	}

	if err := ValidateImageVolumeMode(c.ImageVolumeMode); err != nil {
		return err
	}
	// Check if the pullPolicy from containers.conf is valid
	// if it is invalid returns the error
	if _, err := ParsePullPolicy(c.PullPolicy); err != nil {
		return fmt.Errorf("invalid pull type from containers.conf %q: %w", c.PullPolicy, err)
	}

	if _, err := ParseDBBackend(c.DBBackend); err != nil {
		return err
	}

	// Check if runtimes specified under [engine.runtimes_flags] can be found under [engine.runtimes]
	if err := c.validateRuntimeNames(); err != nil {
		return err
	}

	return nil
}

// Validate is the main entry point for containers configuration validation
// It returns an `error` on validation failure, otherwise
// `nil`.
func (c *ContainersConfig) Validate() error {
	if err := c.validateUlimits(); err != nil {
		return err
	}

	if err := c.validateDevices(); err != nil {
		return err
	}

	if err := c.validateInterfaceName(); err != nil {
		return err
	}

	if err := c.validateTZ(); err != nil {
		return err
	}

	if err := c.validateUmask(); err != nil {
		return err
	}

	if err := c.validateLogPath(); err != nil {
		return err
	}

	if c.LogSizeMax >= 0 && c.LogSizeMax < OCIBufSize {
		return fmt.Errorf("log size max should be negative or >= %d", OCIBufSize)
	}

	if _, err := units.FromHumanSize(c.ShmSize); err != nil {
		return fmt.Errorf("invalid --shm-size %s, %q", c.ShmSize, err)
	}

	return nil
}

// Validate is the main entry point for network configuration validation.
// The parameter `onExecution` specifies if the validation should include
// execution checks. It returns an `error` on validation failure, otherwise
// `nil`.
func (c *NetworkConfig) Validate() error {
	if &c.DefaultSubnetPools != &DefaultSubnetPools {
		for _, pool := range c.DefaultSubnetPools {
			if pool.Base.IP.To4() == nil {
				return fmt.Errorf("invalid subnet pool ip %q", pool.Base.IP)
			}
			ones, _ := pool.Base.Mask.Size()
			if ones > pool.Size {
				return fmt.Errorf("invalid subnet pool, size is bigger than subnet %q", &pool.Base.IPNet)
			}
			if pool.Size > 32 {
				return errors.New("invalid subnet pool size, must be between 0-32")
			}
		}
	}

	return nil
}

// FindConmon iterates over (*Config).ConmonPath and returns the path
// to first (version) matching conmon binary. If non is found, we try
// to do a path lookup of "conmon".
func (c *Config) FindConmon() (string, error) {
	return findConmonPath(c.Engine.ConmonPath.Get(), "conmon")
}

func findConmonPath(paths []string, binaryName string) (string, error) {
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		if stat.IsDir() {
			continue
		}
		logrus.Debugf("Using conmon: %q", path)
		return path, nil
	}

	// Search the $PATH as last fallback
	if path, err := exec.LookPath(binaryName); err == nil {
		logrus.Debugf("Using conmon from $PATH: %q", path)
		return path, nil
	}

	return "", fmt.Errorf("could not find a working conmon binary (configured options: %v: %w)",
		paths, ErrInvalidArg)
}

// FindConmonRs iterates over (*Config).ConmonRsPath and returns the path
// to first (version) matching conmonrs binary. If non is found, we try
// to do a path lookup of "conmonrs".
func (c *Config) FindConmonRs() (string, error) {
	return findConmonPath(c.Engine.ConmonRsPath.Get(), "conmonrs")
}

// GetDefaultEnv returns the environment variables for the container.
// It will check the HTTPProxy and HostEnv booleans and add the appropriate
// environment variables to the container.
func (c *Config) GetDefaultEnv() []string {
	return c.GetDefaultEnvEx(c.Containers.EnvHost, c.Containers.HTTPProxy)
}

// GetDefaultEnvEx returns the environment variables for the container.
// It will check the HTTPProxy and HostEnv boolean parameters and return the appropriate
// environment variables for the container.
func (c *Config) GetDefaultEnvEx(envHost, httpProxy bool) []string {
	var env []string
	if envHost {
		env = append(env, os.Environ()...)
	} else if httpProxy {
		for _, p := range ProxyEnv {
			if val, ok := os.LookupEnv(p); ok {
				env = append(env, fmt.Sprintf("%s=%s", p, val))
			}
		}
	}
	return append(env, c.Containers.Env.Get()...)
}

// Device parses device mapping string to a src, dest & permissions string
// Valid values for device looklike:
//
//	'/dev/sdc"
//	'/dev/sdc:/dev/xvdc"
//	'/dev/sdc:/dev/xvdc:rwm"
//	'/dev/sdc:rm"
func Device(device string) (src, dst, permissions string, err error) {
	permissions = "rwm"
	split := strings.Split(device, ":")
	switch len(split) {
	case 3:
		if !IsValidDeviceMode(split[2]) {
			return "", "", "", fmt.Errorf("invalid device mode: %s", split[2])
		}
		permissions = split[2]
		fallthrough
	case 2:
		if IsValidDeviceMode(split[1]) {
			permissions = split[1]
		} else {
			if split[1] == "" || split[1][0] != '/' {
				return "", "", "", fmt.Errorf("invalid device mode: %s", split[1])
			}
			dst = split[1]
		}
		fallthrough
	case 1:
		if !strings.HasPrefix(split[0], "/dev/") {
			return "", "", "", fmt.Errorf("invalid device mode: %s", split[0])
		}
		src = split[0]
	default:
		return "", "", "", fmt.Errorf("invalid device specification: %s", device)
	}

	if dst == "" {
		dst = src
	}
	return src, dst, permissions, nil
}

// IsValidDeviceMode checks if the mode for device is valid or not.
// IsValid mode is a composition of r (read), w (write), and m (mknod).
func IsValidDeviceMode(mode string) bool {
	legalDeviceMode := map[rune]bool{
		'r': true,
		'w': true,
		'm': true,
	}
	if mode == "" {
		return false
	}
	for _, c := range mode {
		if !legalDeviceMode[c] {
			return false
		}
		legalDeviceMode[c] = false
	}
	return true
}

// Reload clean the cached config and reloads the configuration from containers.conf files
// This function is meant to be used for long-running processes that need to reload potential changes made to
// the cached containers.conf files.
func Reload() (*Config, error) {
	return New(&Options{SetDefault: true})
}

var (
	bindirFailed = false
	bindirCached = ""
)

func findBindir() string {
	if bindirCached != "" || bindirFailed {
		return bindirCached
	}
	execPath, err := os.Executable()
	if err == nil {
		// Resolve symbolic links to find the actual binary file path.
		execPath, err = filepath.EvalSymlinks(execPath)
	}
	if err != nil {
		// If failed to find executable (unlikely to happen), warn about it.
		// The bindirFailed flag will track this, so we only warn once.
		logrus.Warnf("Failed to find $BINDIR: %v", err)
		bindirFailed = true
		return ""
	}
	bindirCached = filepath.Dir(execPath)
	return bindirCached
}

// FindHelperBinary will search the given binary name in the configured directories.
// If searchPATH is set to true it will also search in $PATH.
func (c *Config) FindHelperBinary(name string, searchPATH bool) (string, error) {
	dirList := c.Engine.HelperBinariesDir.Get()
	bindirPath := ""
	bindirSearched := false

	// If set, search this directory first. This is used in testing.
	if dir, found := os.LookupEnv("CONTAINERS_HELPER_BINARY_DIR"); found {
		dirList = append([]string{dir}, dirList...)
	}

	for _, path := range dirList {
		if path == bindirPrefix || strings.HasPrefix(path, bindirPrefix+string(filepath.Separator)) {
			// Calculate the path to the executable first time we encounter a $BINDIR prefix.
			if !bindirSearched {
				bindirSearched = true
				bindirPath = findBindir()
			}
			// If there's an error, don't stop the search for the helper binary.
			// findBindir() will have warned once during the first failure.
			if bindirPath == "" {
				continue
			}
			// Replace the $BINDIR prefix with the path to the directory of the current binary.
			if path == bindirPrefix {
				path = bindirPath
			} else {
				path = filepath.Join(bindirPath, strings.TrimPrefix(path, bindirPrefix+string(filepath.Separator)))
			}
		}
		// Absolute path will force exec.LookPath to check for binary existence instead of lookup everywhere in PATH
		if abspath, err := filepath.Abs(filepath.Join(path, name)); err == nil {
			// exec.LookPath from absolute path on Unix is equal to os.Stat + IsNotDir + check for executable bits in FileMode
			// exec.LookPath from absolute path on Windows is equal to os.Stat + IsNotDir for `file.ext` or loops through extensions from PATHEXT for `file`
			if lp, err := exec.LookPath(abspath); err == nil {
				return lp, nil
			}
		}
	}
	if searchPATH {
		return exec.LookPath(name)
	}
	configHint := "To resolve this error, set the helper_binaries_dir key in the `[engine]` section of containers.conf to the directory containing your helper binaries."
	if len(dirList) == 0 {
		return "", fmt.Errorf("could not find %q because there are no helper binary directories configured.  %s", name, configHint)
	}
	return "", fmt.Errorf("could not find %q in one of %v.  %s", name, dirList, configHint)
}

// ImageCopyTmpDir default directory to store temporary image files during copy.
func (c *Config) ImageCopyTmpDir() (string, error) {
	if path, found := os.LookupEnv("TMPDIR"); found {
		return path, nil
	}
	switch c.Engine.ImageCopyTmpDir {
	case "":
		return "", nil
	case "storage":
		return filepath.Join(c.Engine.graphRoot, "tmp"), nil
	default:
		if filepath.IsAbs(c.Engine.ImageCopyTmpDir) {
			return c.Engine.ImageCopyTmpDir, nil
		}
	}

	return "", fmt.Errorf("invalid image_copy_tmp_dir value %q (relative paths are not accepted)", c.Engine.ImageCopyTmpDir)
}

// setupEnv sets the environment variables for the engine.
func (c *Config) setupEnv() error {
	for _, env := range c.Engine.Env.Get() {
		key, value, ok := strings.Cut(env, "=")
		if !ok {
			logrus.Warnf("invalid environment variable for engine %s, valid configuration is KEY=value pair", env)
			continue
		}
		// skip if the env is already defined
		if _, ok := os.LookupEnv(key); ok {
			logrus.Debugf("environment variable %s is already defined, skip the settings from containers.conf", key)
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// eventsLogMaxSize is the type used by EventsLogFileMaxSize.
type eventsLogMaxSize uint64

// UnmarshalText parses the JSON encoding of eventsLogMaxSize and
// stores it in a value.
func (e *eventsLogMaxSize) UnmarshalText(text []byte) error {
	// REMOVE once writing works
	if string(text) == "" {
		return nil
	}
	val, err := units.FromHumanSize((string(text)))
	if err != nil {
		return err
	}
	if val < 0 {
		return fmt.Errorf("events log file max size cannot be negative: %s", string(text))
	}
	*e = eventsLogMaxSize(uint64(val))
	return nil
}

// MarshalText returns the JSON encoding of eventsLogMaxSize.
func (e eventsLogMaxSize) MarshalText() ([]byte, error) {
	if uint64(e) == DefaultEventsLogSizeMax || e == 0 {
		v := []byte{}
		return v, nil
	}
	return fmt.Appendf(nil, "%d", e), nil
}

func ValidateImageVolumeMode(mode string) error {
	if mode == "" {
		return nil
	}
	if slices.Contains(validImageVolumeModes, mode) {
		return nil
	}

	return fmt.Errorf("invalid image volume mode %q required value: %s", mode, strings.Join(validImageVolumeModes, ", "))
}

// FindInitBinary will return the path to the init binary (catatonit).
func (c *Config) FindInitBinary() (string, error) {
	// Sigh, for some reason we ended up with two InitPath field in containers.conf and
	// both are used in podman so we have to keep supporting both to prevent regressions.
	if c.Containers.InitPath != "" {
		return c.Containers.InitPath, nil
	}
	if c.Engine.InitPath != "" {
		return c.Engine.InitPath, nil
	}
	// keep old default working to guarantee backwards compat
	if err := fileutils.Exists(DefaultInitPath); err == nil {
		return DefaultInitPath, nil
	}
	return c.FindHelperBinary(defaultInitName, true)
}

// PodmanshTimeout returns the timeout in seconds for podmansh to connect to the container.
// Returns podmansh.Timeout if set, otherwise engine.PodmanshTimeout for backwards compatibility.
func (c *Config) PodmanshTimeout() uint {
	// podmansh.Timeout has precedence, if set
	if c.Podmansh.Timeout > 0 {
		return c.Podmansh.Timeout
	}
	return c.Engine.PodmanshTimeout
}
