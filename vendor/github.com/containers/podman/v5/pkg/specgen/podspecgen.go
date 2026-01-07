package specgen

import (
	"net"

	spec "github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/common/libnetwork/types"
	storageTypes "go.podman.io/storage/types"
)

// PodBasicConfig contains basic configuration options for pods.
type PodBasicConfig struct {
	// Name is the name of the pod.
	// If not provided, a name will be generated when the pod is created.
	// Optional.
	Name string `json:"name,omitempty"`
	// Hostname is the pod's hostname. If not set, the name of the pod will
	// be used (if a name was not provided here, the name auto-generated for
	// the pod will be used). This will be used by the infra container and
	// all containers in the pod as long as the UTS namespace is shared.
	// Optional.
	Hostname string `json:"hostname,omitempty"`
	// ExitPolicy determines the pod's exit and stop behaviour.
	ExitPolicy string `json:"exit_policy,omitempty"`
	// Labels are key-value pairs that are used to add metadata to pods.
	// Optional.
	Labels map[string]string `json:"labels,omitempty"`
	// NoInfra tells the pod not to create an infra container. If this is
	// done, many networking-related options will become unavailable.
	// Conflicts with setting any options in PodNetworkConfig, and the
	// InfraCommand and InfraImages in this struct.
	// Optional.
	NoInfra bool `json:"no_infra,omitempty"`
	// InfraConmonPidFile is a custom path to store the infra container's
	// conmon PID.
	InfraConmonPidFile string `json:"infra_conmon_pid_file,omitempty"`
	// InfraCommand sets the command that will be used to start the infra
	// container.
	// If not set, the default set in the Libpod configuration file will be
	// used.
	// Conflicts with NoInfra=true.
	// Optional.
	InfraCommand []string `json:"infra_command,omitempty"`
	// InfraImage is the image that will be used for the infra container.
	// If not set, the default set in the Libpod configuration file will be
	// used.
	// Conflicts with NoInfra=true.
	// Optional.
	InfraImage string `json:"infra_image,omitempty"`
	// InfraName is the name that will be used for the infra container.
	// If not set, the default set in the Libpod configuration file will be
	// used.
	// Conflicts with NoInfra=true.
	// Optional.
	InfraName string `json:"infra_name,omitempty"`
	// Ipc sets the IPC namespace of the pod, set to private by default.
	// This configuration will then be shared with the entire pod if PID namespace sharing is enabled via --share
	Ipc Namespace `json:"ipcns"`
	// SharedNamespaces instructs the pod to share a set of namespaces.
	// Shared namespaces will be joined (by default) by every container
	// which joins the pod.
	// If not set and NoInfra is false, the pod will set a default set of
	// namespaces to share.
	// Conflicts with NoInfra=true.
	// Optional.
	SharedNamespaces []string `json:"shared_namespaces,omitempty"`
	// RestartPolicy is the pod's restart policy - an action which
	// will be taken when one or all the containers in the pod exits.
	// If not given, the default policy will be set to Always, which
	// restarts the containers in the pod when they exit indefinitely.
	// Optional.
	RestartPolicy string `json:"restart_policy,omitempty"`
	// RestartRetries is the number of attempts that will be made to restart
	// the container.
	// Only available when RestartPolicy is set to "on-failure".
	// Optional.
	RestartRetries *uint `json:"restart_tries,omitempty"`
	// PodCreateCommand is the command used to create this pod.
	// This will be shown in the output of Inspect() on the pod, and may
	// also be used by some tools that wish to recreate the pod
	// (e.g. `podman generate systemd --new`).
	// Optional.
	// ShareParent determines if all containers in the pod will share the pod's cgroup as the cgroup parent
	ShareParent      *bool    `json:"share_parent,omitempty"`
	PodCreateCommand []string `json:"pod_create_command,omitempty"`
	// Pid sets the process id namespace of the pod
	// Optional (defaults to private if unset). This sets the PID namespace of the infra container
	// This configuration will then be shared with the entire pod if PID namespace sharing is enabled via --share
	Pid Namespace `json:"pidns"`
	// Userns is used to indicate which kind of Usernamespace to enter.
	// Any containers created within the pod will inherit the pod's userns settings.
	// Optional
	Userns Namespace `json:"userns"`
	// UtsNs is used to indicate the UTS mode the pod is in
	UtsNs Namespace `json:"utsns"`
	// Devices contains user specified Devices to be added to the Pod
	Devices []string `json:"pod_devices,omitempty"`
	// Sysctl sets kernel parameters for the pod
	Sysctl map[string]string `json:"sysctl,omitempty"`
}

// PodNetworkConfig contains networking configuration for a pod.
type PodNetworkConfig struct {
	// NetNS is the configuration to use for the infra container's network
	// namespace. This network will, by default, be shared with all
	// containers in the pod.
	// Cannot be set to FromContainer and FromPod.
	// Setting this to anything except default conflicts with NoInfra=true.
	// Defaults to Bridge as root and Slirp as rootless.
	// Mandatory.
	NetNS Namespace `json:"netns"`
	// PortMappings is a set of ports to map into the infra container.
	// As, by default, containers share their network with the infra
	// container, this will forward the ports to the entire pod.
	// Only available if NetNS is set to Bridge, Slirp, or Pasta.
	// Optional.
	PortMappings []types.PortMapping `json:"portmappings,omitempty"`
	// Map of networks names to ids the container should join to.
	// You can request additional settings for each network, you can
	// set network aliases, static ips, static mac address  and the
	// network interface name for this container on the specific network.
	// If the map is empty and the bridge network mode is set the container
	// will be joined to the default network.
	Networks map[string]types.PerNetworkOptions
	// CNINetworks is a list of CNI networks to join the container to.
	// If this list is empty, the default CNI network will be joined
	// instead. If at least one entry is present, we will not join the
	// default network (unless it is part of this list).
	// Only available if NetNS is set to bridge.
	// Optional.
	// Deprecated: as of podman 4.0 use "Networks" instead.
	CNINetworks []string `json:"cni_networks,omitempty"`
	// NoManageResolvConf indicates that /etc/resolv.conf should not be
	// managed by the pod. Instead, each container will create and manage a
	// separate resolv.conf as if they had not joined a pod.
	// Conflicts with NoInfra=true and DNSServer, DNSSearch, DNSOption.
	// Optional.
	NoManageResolvConf bool `json:"no_manage_resolv_conf,omitempty"`
	// DNSServer is a set of DNS servers that will be used in the infra
	// container's resolv.conf, which will, by default, be shared with all
	// containers in the pod.
	// If not provided, the host's DNS servers will be used, unless the only
	// server set is a localhost address. As the container cannot connect to
	// the host's localhost, a default server will instead be set.
	// Conflicts with NoInfra=true.
	// Optional.
	DNSServer []net.IP `json:"dns_server,omitempty"`
	// DNSSearch is a set of DNS search domains that will be used in the
	// infra container's resolv.conf, which will, by default, be shared with
	// all containers in the pod.
	// If not provided, DNS search domains from the host's resolv.conf will
	// be used.
	// Conflicts with NoInfra=true.
	// Optional.
	DNSSearch []string `json:"dns_search,omitempty"`
	// DNSOption is a set of DNS options that will be used in the infra
	// container's resolv.conf, which will, by default, be shared with all
	// containers in the pod.
	// Conflicts with NoInfra=true.
	// Optional.
	DNSOption []string `json:"dns_option,omitempty"`
	// NoManageHostname indicates that /etc/hostname should not be managed
	//  by the pod. Instead, each container will create a separate
	// /etc/hostname as they would if not in a pod.
	NoManageHostname bool `json:"no_manage_hostname,omitempty"`
	// NoManageHosts indicates that /etc/hosts should not be managed by the
	// pod. Instead, each container will create a separate /etc/hosts as
	// they would if not in a pod.
	// Conflicts with HostAdd.
	NoManageHosts bool `json:"no_manage_hosts,omitempty"`
	// HostAdd is a set of hosts that will be added to the infra container's
	// /etc/hosts that will, by default, be shared with all containers in
	// the pod.
	// Conflicts with NoInfra=true and NoManageHosts.
	// Optional.
	HostAdd []string `json:"hostadd,omitempty"`
	// HostsFile is the base file to create the `/etc/hosts` file inside the infra container.
	// This must either be an absolute path to a file on the host system, or one of the
	// special flags `image` or `none`.
	// If it is empty it defaults to the base_hosts_file configuration in containers.conf.
	// Conflicts with NoInfra=true and NoManageHosts.
	// Optional.
	HostsFile string `json:"hostsFile,omitempty"`
	// NetworkOptions are additional options for each network
	// Optional.
	NetworkOptions map[string][]string `json:"network_options,omitempty"`
}

// PodStorageConfig contains all of the storage related options for the pod and its infra container.
type PodStorageConfig struct {
	// Mounts are mounts that will be added to the pod.
	// These will supersede Image Volumes and VolumesFrom volumes where
	// there are conflicts.
	// Optional.
	Mounts []spec.Mount `json:"mounts,omitempty"`
	// Volumes are named volumes that will be added to the pod.
	// These will supersede Image Volumes and VolumesFrom  volumes where
	// there are conflicts.
	// Optional.
	Volumes []*NamedVolume `json:"volumes,omitempty"`
	// Overlay volumes are named volumes that will be added to the pod.
	// Optional.
	OverlayVolumes []*OverlayVolume `json:"overlay_volumes,omitempty"`
	// Image volumes bind-mount a container-image mount into the pod's infra container.
	// Optional.
	ImageVolumes []*ImageVolume `json:"image_volumes,omitempty"`
	// VolumesFrom is a set of containers whose volumes will be added to
	// this pod. The name or ID of the container must be provided, and
	// may optionally be followed by a : and then one or more
	// comma-separated options. Valid options are 'ro', 'rw', and 'z'.
	// Options will be used for all volumes sourced from the container.
	VolumesFrom []string `json:"volumes_from,omitempty"`
	// ShmSize is the size of the tmpfs to mount in at /dev/shm, in bytes.
	// Conflicts with ShmSize if IpcNS is not private.
	// Optional.
	ShmSize *int64 `json:"shm_size,omitempty"`
	// ShmSizeSystemd is the size of systemd-specific tmpfs mounts
	// specifically /run, /run/lock, /var/log/journal and /tmp.
	// Optional
	ShmSizeSystemd *int64 `json:"shm_size_systemd,omitempty"`
}

// PodCgroupConfig contains configuration options about a pod's cgroups.
// This will be expanded in future updates to pods.
type PodCgroupConfig struct {
	// CgroupParent is the parent for the Cgroup that the pod will create.
	// This pod cgroup will, in turn, be the default cgroup parent for all
	// containers in the pod.
	// Optional.
	CgroupParent string `json:"cgroup_parent,omitempty"`
}

// PodSpecGenerator describes options to create a pod
// swagger:model PodSpecGenerator
type PodSpecGenerator struct {
	PodBasicConfig
	PodNetworkConfig
	PodCgroupConfig
	PodResourceConfig
	PodStorageConfig
	PodSecurityConfig
	InfraContainerSpec *SpecGenerator `json:"-"`

	// The ID of the pod's service container.
	ServiceContainerID string `json:"serviceContainerID,omitempty"`
}

type PodResourceConfig struct {
	// ResourceLimits contains linux specific CPU data for the pod
	ResourceLimits *spec.LinuxResources `json:"resource_limits,omitempty"`
}

type PodSecurityConfig struct {
	SecurityOpt []string `json:"security_opt,omitempty"`
	// IDMappings are UID and GID mappings that will be used by user
	// namespaces.
	// Required if UserNS is private.
	IDMappings *storageTypes.IDMappingOptions `json:"idmappings,omitempty"`
}

// NewPodSpecGenerator creates a new pod spec
func NewPodSpecGenerator() *PodSpecGenerator {
	return &PodSpecGenerator{}
}
