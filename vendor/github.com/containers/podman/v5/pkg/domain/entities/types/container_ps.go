package types

import (
	"time"

	define "github.com/containers/podman/v5/pkg/ps/define"
	netTypes "go.podman.io/common/libnetwork/types"
)

// ListContainer describes a container suitable for listing
type ListContainer struct {
	// AutoRemove
	AutoRemove bool
	// Container command
	Command []string
	// Container creation time
	Created time.Time
	// Human-readable container creation time.
	CreatedAt string
	// CIDFile specified at creation time.
	CIDFile string
	// If container has exited/stopped
	Exited bool
	// Time container exited
	ExitedAt int64
	// If container has exited, the return code from the command
	ExitCode int32
	// ExposedPorts contains the ports that are exposed but not forwarded,
	// see Ports for forwarded ports.
	// The key is the port number and the string slice contains the protocols,
	// i.e. "tcp", "udp" and "sctp".
	ExposedPorts map[uint16][]string
	// The unique identifier for the container
	ID string `json:"Id"`
	// Container image
	Image string
	// Container image ID
	ImageID string
	// If this container is a Pod infra container
	IsInfra bool
	// Labels for container
	Labels map[string]string
	// User volume mounts
	Mounts []string
	// The names assigned to the container
	Names []string
	// Namespaces the container belongs to.  Requires the
	// namespace boolean to be true
	Namespaces ListContainerNamespaces
	// The network names assigned to the container
	Networks []string
	// The process id of the container
	Pid int
	// If the container is part of Pod, the Pod ID. Requires the pod
	// boolean to be set
	Pod string
	// If the container is part of Pod, the Pod name. Requires the pod
	// boolean to be set
	PodName string
	// Port mappings
	Ports []netTypes.PortMapping
	// Restarts is how many times the container was restarted by its
	// restart policy. This is NOT incremented by normal container restarts
	// (only by restart policy).
	Restarts uint
	// Size of the container rootfs.  Requires the size boolean to be true
	Size *define.ContainerSize
	// Time when container started
	StartedAt int64
	// State of container
	State string
	// Status is a human-readable approximation of a duration for json output
	Status string
}

// ListContainerNamespaces contains the identifiers of the container's Linux namespaces
type ListContainerNamespaces struct {
	// Mount namespace
	MNT string `json:"Mnt,omitempty"`
	// Cgroup namespace
	Cgroup string `json:"Cgroup,omitempty"`
	// IPC namespace
	IPC string `json:"Ipc,omitempty"`
	// Network namespace
	NET string `json:"Net,omitempty"`
	// PID namespace
	PIDNS string `json:"Pidns,omitempty"`
	// UTS namespace
	UTS string `json:"Uts,omitempty"`
	// User namespace
	User string `json:"User,omitempty"`
}

func (l ListContainer) CGROUPNS() string {
	return l.Namespaces.Cgroup
}

func (l ListContainer) IPC() string {
	return l.Namespaces.IPC
}

func (l ListContainer) MNT() string {
	return l.Namespaces.MNT
}

func (l ListContainer) NET() string {
	return l.Namespaces.NET
}

func (l ListContainer) PIDNS() string {
	return l.Namespaces.PIDNS
}

func (l ListContainer) USERNS() string {
	return l.Namespaces.User
}

func (l ListContainer) UTS() string {
	return l.Namespaces.UTS
}
