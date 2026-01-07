//go:build !remote

package libpod

import "go.podman.io/common/libnetwork/types"

// State is a storage backend for libpod's current state.
// A State is only initialized once per instance of libpod.
// As such, initialization methods for State implementations may safely assume
// they will be run as a singleton.
// For all container and pod retrieval methods, a State must retrieve the
// Configuration struct of the container or pod and include it in the returned
// struct. The State of the container or pod may optionally be included as well,
// but this is not a requirement.
// As such, all containers and pods must be synced with the database via the
// UpdateContainer and UpdatePod calls before any state-specific information is
// retrieved after they are pulled from the database.
// Generally speaking, the syncContainer() call should be run at the beginning
// of all API operations, which will silently handle this.
type State interface { //nolint:interfacebloat
	// Close performs any pre-exit cleanup (e.g. closing database
	// connections) that may be required
	Close() error

	// Refresh clears container and pod states after a reboot
	Refresh() error

	// GetDBConfig retrieves several paths configured within the database
	// when it was created - namely, Libpod root and tmp dirs, c/storage
	// root and tmp dirs, and c/storage graph driver.
	// This is not implemented by the in-memory state, as it has no need to
	// validate runtime configuration.
	GetDBConfig() (*DBConfig, error)

	// ValidateDBConfig validates the config in the given Runtime struct
	// against paths stored in the configured database.
	// Libpod root and tmp dirs and c/storage root and tmp dirs and graph
	// driver are validated.
	// This is not implemented by the in-memory state, as it has no need to
	// validate runtime configuration that may change over multiple runs of
	// the program.
	ValidateDBConfig(runtime *Runtime) error

	// Resolve an ID to a Container Name.
	GetContainerName(id string) (string, error)
	// Resolve an ID to a Pod Name.
	GetPodName(id string) (string, error)

	// Return a container from the database from its full ID.
	// If the container is not in the set namespace, an error will be
	// returned.
	Container(id string) (*Container, error)
	// Return a container ID from the database by full or partial ID or full
	// name.
	LookupContainerID(idOrName string) (string, error)
	// Return a container from the database by full or partial ID or full
	// name.
	// Containers not in the set namespace will be ignored.
	LookupContainer(idOrName string) (*Container, error)
	// Check if a container with the given full ID exists in the database.
	// If the container exists but is not in the set namespace, false will
	// be returned.
	HasContainer(id string) (bool, error)
	// Adds container to state.
	// The container cannot be part of a pod.
	// The container must have globally unique name and ID - pod names and
	// IDs also conflict with container names and IDs.
	// The container must be in the set namespace if a namespace has been
	// set.
	// All containers this container depends on must be part of the same
	// namespace and must not be joined to a pod.
	AddContainer(ctr *Container) error
	// Removes container from state.
	// Containers that are part of pods must use RemoveContainerFromPod.
	// The container must be part of the set namespace.
	// All dependencies must be removed first.
	// All exec sessions referencing the container must be removed first.
	RemoveContainer(ctr *Container) error
	// UpdateContainer updates a container's state from the backing store.
	// The container must be part of the set namespace.
	UpdateContainer(ctr *Container) error
	// SaveContainer saves a container's current state to the backing store.
	// The container must be part of the set namespace.
	SaveContainer(ctr *Container) error
	// ContainerInUse checks if other containers depend upon a given
	// container.
	// It returns a slice of the IDs of containers which depend on the given
	// container. If the slice is empty, no container depend on the given
	// container.
	// A container cannot be removed if other containers depend on it.
	// The container being checked must be part of the set namespace.
	ContainerInUse(ctr *Container) ([]string, error)
	// Retrieves all containers presently in state.
	// If `loadState` is set, the containers' state will be loaded as well.
	// If a namespace is set, only containers within the namespace will be
	// returned.
	AllContainers(loadState bool) ([]*Container, error)

	// Get networks the container is currently connected to.
	GetNetworks(ctr *Container) (map[string]types.PerNetworkOptions, error)
	// Add the container to the given network with the given options
	NetworkConnect(ctr *Container, network string, opts types.PerNetworkOptions) error
	// Modify the container network with the given options.
	NetworkModify(ctr *Container, network string, opts types.PerNetworkOptions) error
	// Remove the container from the given network, removing all aliases for
	// the container in that network in the process.
	NetworkDisconnect(ctr *Container, network string) error

	// Return a container config from the database by full ID
	GetContainerConfig(id string) (*ContainerConfig, error)

	// Add the exit code for the specified container to the database.
	AddContainerExitCode(id string, exitCode int32) error
	// Return the exit code for the specified container.
	GetContainerExitCode(id string) (int32, error)
	// Remove exit codes older than 5 minutes.
	PruneContainerExitCodes() error

	// Add creates a reference to an exec session in the database.
	// The container the exec session is attached to will be recorded.
	// The container state will not be modified.
	// The actual exec session itself is part of the container's state.
	// We assume higher-level callers will add the session by saving the
	// container's state before calling this. This only ensures that the ID
	// of the exec session is associated with the ID of the container.
	// Implementations may, but are not required to, verify that the state
	// of the given container has an exec session with the ID given.
	AddExecSession(ctr *Container, session *ExecSession) error
	// Get retrieves the container a given exec session is attached to.
	GetExecSession(id string) (string, error)
	// Remove a reference to an exec session from the database.
	// This will not modify container state to remove the exec session there
	// and instead only removes the session ID -> container ID reference
	// added by AddExecSession.
	RemoveExecSession(session *ExecSession) error
	// Get the IDs of all exec sessions attached to a given container.
	GetContainerExecSessions(ctr *Container) ([]string, error)
	// Remove all exec sessions for a single container.
	// Usually used as part of removing the container.
	// As with RemoveExecSession, container state will not be modified.
	RemoveContainerExecSessions(ctr *Container) error

	// ContainerIDIsVolume checks if the given container ID is in use by a
	// volume.
	// Some volumes are backed by a c/storage container. These do not have a
	// corresponding Container struct in Libpod, but rather a Volume.
	// This determines if a given ID from c/storage is used as a backend by
	// a Podman volume.
	ContainerIDIsVolume(id string) (bool, error)

	// PLEASE READ FULL DESCRIPTION BEFORE USING.
	// Rewrite a container's configuration.
	// This function breaks libpod's normal prohibition on a read-only
	// configuration, and as such should be used EXTREMELY SPARINGLY and
	// only in very specific circumstances.
	// Specifically, it is ONLY safe to use thing function to make changes
	// that result in a functionally identical configuration (migrating to
	// newer, but identical, configuration fields), or during libpod init
	// WHILE HOLDING THE ALIVE LOCK (to prevent other libpod instances from
	// being initialized).
	// Most things in config can be changed by this, but container ID and
	// name ABSOLUTELY CANNOT BE ALTERED. If you do so, there is a high
	// potential for database corruption.
	// There are a lot of capital letters and conditions here, but the short
	// answer is this: use this only very sparingly, and only if you really
	// know what you're doing.
	// TODO: Once BoltDB is removed, RewriteContainerConfig and
	// SafeRewriteContainerConfig can be merged.
	RewriteContainerConfig(ctr *Container, newCfg *ContainerConfig) error
	// This is a more limited version of RewriteContainerConfig, though it
	// comes with the added ability to alter a container's name. In exchange
	// it loses the ability to manipulate the container's locks.
	// It is not intended to be as restrictive as RewriteContainerConfig, in
	// that we allow it to be run while other Podman processes are running,
	// and without holding the alive lock.
	// Container ID and pod membership still *ABSOLUTELY CANNOT* be altered.
	// Also, you cannot change a container's dependencies - shared namespace
	// containers or generic dependencies - at present. This is
	// theoretically possible but not yet implemented.
	// If newName is not "" the container will be renamed to the new name.
	// The oldName parameter is only required if newName is given.
	SafeRewriteContainerConfig(ctr *Container, oldName, newName string, newCfg *ContainerConfig) error
	// PLEASE READ THE DESCRIPTION FOR RewriteContainerConfig BEFORE USING.
	// This function is identical to RewriteContainerConfig, save for the
	// fact that it is used with pods instead.
	// It is subject to the same conditions as RewriteContainerConfig.
	// Please do not use this unless you know what you're doing.
	RewritePodConfig(pod *Pod, newCfg *PodConfig) error
	// PLEASE READ THE DESCRIPTION FOR RewriteContainerConfig BEFORE USING.
	// This function is identical to RewriteContainerConfig, save for the
	// fact that it is used with volumes instead.
	// It is subject to the same conditions as RewriteContainerConfig.
	// The exception is that volumes do not have IDs, so only volume name
	// cannot be altered.
	// Please do not use this unless you know what you're doing.
	RewriteVolumeConfig(volume *Volume, newCfg *VolumeConfig) error

	// Accepts full ID of pod.
	// If the pod given is not in the set namespace, an error will be
	// returned.
	Pod(id string) (*Pod, error)
	// Accepts full or partial IDs (as long as they are unique) and names.
	// Pods not in the set namespace are ignored.
	LookupPod(idOrName string) (*Pod, error)
	// Checks if a pod with the given ID is present in the state.
	// If the given pod is not in the set namespace, false is returned.
	HasPod(id string) (bool, error)
	// Check if a pod has a container with the given ID.
	// The pod must be part of the set namespace.
	PodHasContainer(pod *Pod, ctrID string) (bool, error)
	// Get the IDs of all containers in a pod.
	// The pod must be part of the set namespace.
	PodContainersByID(pod *Pod) ([]string, error)
	// Get all the containers in a pod.
	// The pod must be part of the set namespace.
	PodContainers(pod *Pod) ([]*Container, error)
	// Adds pod to state.
	// The pod must be part of the set namespace.
	// The pod's name and ID must be globally unique.
	AddPod(pod *Pod) error
	// Removes pod from state.
	// Only empty pods can be removed from the state.
	// The pod must be part of the set namespace.
	RemovePod(pod *Pod) error
	// Remove all containers from a pod.
	// Used to simultaneously remove containers that might otherwise have
	// dependency issues.
	// Will fail if a dependency outside the pod is encountered.
	// The pod must be part of the set namespace.
	RemovePodContainers(pod *Pod) error
	// AddContainerToPod adds a container to an existing pod.
	// The container given will be added to the state and the pod.
	// The container and its dependencies must be part of the given pod,
	// and the given pod's namespace.
	// The pod must be part of the set namespace.
	// The pod must already exist in the state.
	// The container's name and ID must be globally unique.
	AddContainerToPod(pod *Pod, ctr *Container) error
	// RemoveContainerFromPod removes a container from an existing pod.
	// The container will also be removed from the state.
	// The container must be in the given pod, and the pod must be in the
	// set namespace.
	RemoveContainerFromPod(pod *Pod, ctr *Container) error
	// UpdatePod updates a pod's state from the database.
	// The pod must be in the set namespace.
	UpdatePod(pod *Pod) error
	// SavePod saves a pod's state to the database.
	// The pod must be in the set namespace.
	SavePod(pod *Pod) error
	// Retrieves all pods presently in state.
	// If a namespace has been set, only pods in that namespace will be
	// returned.
	AllPods() ([]*Pod, error)

	// Volume accepts full name of volume
	// If the volume doesn't exist, an error will be returned
	Volume(volName string) (*Volume, error)
	// LookupVolume accepts an unambiguous partial name or full name of a
	// volume. Ambiguous names will result in an error.
	LookupVolume(name string) (*Volume, error)
	// HasVolume returns true if volName exists in the state,
	// otherwise it returns false
	HasVolume(volName string) (bool, error)
	// VolumeInUse goes through the container dependencies of a volume
	// and checks if the volume is being used by any container. If it is
	// a slice of container IDs using the volume is returned
	VolumeInUse(volume *Volume) ([]string, error)
	// AddVolume adds the specified volume to state. The volume's name
	// must be unique within the list of existing volumes
	AddVolume(volume *Volume) error
	// RemoveVolume removes the specified volume.
	// Only volumes that have no container dependencies can be removed
	RemoveVolume(volume *Volume) error
	// UpdateVolume updates the volume's state from the database.
	UpdateVolume(volume *Volume) error
	// SaveVolume saves a volume's state to the database.
	SaveVolume(volume *Volume) error
	// AllVolumes returns all the volumes available in the state
	AllVolumes() ([]*Volume, error)
}
