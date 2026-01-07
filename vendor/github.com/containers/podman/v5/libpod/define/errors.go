package define

import (
	"errors"
	"fmt"

	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/detach"
)

var (
	// ErrNoSuchCtr indicates the requested container does not exist
	ErrNoSuchCtr = errors.New("no such container")

	// ErrNoSuchPod indicates the requested pod does not exist
	ErrNoSuchPod = errors.New("no such pod")

	// ErrNoSuchVolume indicates the requested volume does not exist
	ErrNoSuchVolume = errors.New("no such volume")

	// ErrNoSuchNetwork indicates the requested network does not exist
	ErrNoSuchNetwork = types.ErrNoSuchNetwork

	// ErrNoSuchExecSession indicates that the requested exec session does
	// not exist.
	ErrNoSuchExecSession = errors.New("no such exec session")

	// ErrNoSuchExitCode indicates that the requested container exit code
	// does not exist.
	ErrNoSuchExitCode = errors.New("no such exit code")

	// ErrDepExists indicates that the current object has dependencies and
	// cannot be removed before them.
	ErrDepExists = errors.New("dependency exists")

	// ErrNoAliases indicates that the container does not have any network
	// aliases.
	ErrNoAliases = errors.New("no aliases for container")

	// ErrMissingPlugin indicates that the requested operation requires a
	// plugin that is not present on the system or in the configuration.
	ErrMissingPlugin = errors.New("required plugin missing")

	// ErrCtrExists indicates a container with the same name or ID already
	// exists
	ErrCtrExists = errors.New("container already exists")
	// ErrPodExists indicates a pod with the same name or ID already exists
	ErrPodExists = errors.New("pod already exists")
	// ErrImageExists indicates an image with the same ID already exists
	ErrImageExists = errors.New("image already exists")
	// ErrVolumeExists indicates a volume with the same name already exists
	ErrVolumeExists = errors.New("volume already exists")
	// ErrExecSessionExists indicates an exec session with the same ID
	// already exists.
	ErrExecSessionExists = errors.New("exec session already exists")
	// ErrNetworkExists indicates that a network with the given name already
	// exists.
	ErrNetworkExists = types.ErrNetworkExists

	// ErrCtrStateInvalid indicates a container is in an improper state for
	// the requested operation
	ErrCtrStateInvalid = errors.New("container state improper")
	// ErrCtrStateRunning indicates a container is running.
	ErrCtrStateRunning = errors.New("container is running")
	// ErrExecSessionStateInvalid indicates that an exec session is in an
	// improper state for the requested operation
	ErrExecSessionStateInvalid = errors.New("exec session state improper")
	// ErrVolumeBeingUsed indicates that a volume is being used by at least one container
	ErrVolumeBeingUsed = errors.New("volume is being used")

	// ErrRuntimeFinalized indicates that the runtime has already been
	// created and cannot be modified
	ErrRuntimeFinalized = errors.New("runtime has been finalized")
	// ErrCtrFinalized indicates that the container has already been created
	// and cannot be modified
	ErrCtrFinalized = errors.New("container has been finalized")
	// ErrPodFinalized indicates that the pod has already been created and
	// cannot be modified
	ErrPodFinalized = errors.New("pod has been finalized")
	// ErrVolumeFinalized indicates that the volume has already been created and
	// cannot be modified
	ErrVolumeFinalized = errors.New("volume has been finalized")

	// ErrInvalidArg indicates that an invalid argument was passed
	ErrInvalidArg = types.ErrInvalidArg
	// ErrEmptyID indicates that an empty ID was passed
	ErrEmptyID = errors.New("name or ID cannot be empty")

	// ErrInternal indicates an internal library error
	ErrInternal = errors.New("internal libpod error")

	// ErrPodPartialFail indicates that a pod operation was only partially
	// successful, and some containers within the pod failed.
	ErrPodPartialFail = errors.New("some containers failed")

	// ErrDetach indicates that an attach session was manually detached by
	// the user.
	ErrDetach = detach.ErrDetach

	// ErrWillDeadlock indicates that the requested operation will cause a
	// deadlock. This is usually caused by upgrade issues, and is resolved
	// by renumbering the locks.
	ErrWillDeadlock = errors.New("deadlock due to lock mismatch")

	// ErrNoCgroups indicates that the container does not have its own
	// Cgroup.
	ErrNoCgroups = errors.New("this container does not have a cgroup")
	// ErrNoLogs indicates that this container is not creating a log so log
	// operations cannot be performed on it
	ErrNoLogs = errors.New("this container is not logging output")

	// ErrRootless indicates that the given command cannot but run without
	// root.
	ErrRootless = errors.New("operation requires root privileges")

	// ErrRuntimeStopped indicates that the runtime has already been shut
	// down and no further operations can be performed on it
	ErrRuntimeStopped = errors.New("runtime has already been stopped")
	// ErrCtrStopped indicates that the requested container is not running
	// and the requested operation cannot be performed until it is started
	ErrCtrStopped = errors.New("container is stopped")

	// ErrCtrRemoved indicates that the container has already been removed
	// and no further operations can be performed on it
	ErrCtrRemoved = errors.New("container has already been removed")
	// ErrPodRemoved indicates that the pod has already been removed and no
	// further operations can be performed on it
	ErrPodRemoved = errors.New("pod has already been removed")
	// ErrVolumeRemoved indicates that the volume has already been removed and
	// no further operations can be performed on it
	ErrVolumeRemoved = errors.New("volume has already been removed")
	// ErrExecSessionRemoved indicates that the exec session has already
	// been removed and no further operations can be performed on it.
	ErrExecSessionRemoved = errors.New("exec session has already been removed")

	// ErrDBClosed indicates that the connection to the state database has
	// already been closed
	ErrDBClosed = errors.New("database connection already closed")
	// ErrDBBadConfig indicates that the database has a different schema or
	// was created by a libpod with a different config
	ErrDBBadConfig = errors.New("database configuration mismatch")

	// ErrNSMismatch indicates that the requested pod or container is in a
	// different namespace and cannot be accessed or modified.
	ErrNSMismatch = errors.New("target is in a different namespace")

	// ErrNotImplemented indicates that the requested functionality is not
	// yet present
	ErrNotImplemented = errors.New("not yet implemented")

	// ErrOSNotSupported indicates the function is not available on the particular
	// OS.
	ErrOSNotSupported = errors.New("no support for this OS yet")

	// ErrOCIRuntime indicates a generic error from the OCI runtime
	ErrOCIRuntime = errors.New("OCI runtime error")

	// ErrOCIRuntimePermissionDenied indicates the OCI runtime attempted to invoke a command that returned
	// a permission denied error
	ErrOCIRuntimePermissionDenied = errors.New("OCI permission denied")

	// ErrOCIRuntimeNotFound indicates the OCI runtime attempted to invoke a command
	// that was not found
	ErrOCIRuntimeNotFound = errors.New("OCI runtime attempted to invoke a command that was not found")

	// ErrOCIRuntimeUnavailable indicates that the OCI runtime associated to a container
	// could not be found in the configuration
	ErrOCIRuntimeUnavailable = errors.New("OCI unavailable")

	// ErrConmonOutdated indicates the version of conmon found (whether via the configuration or $PATH)
	// is out of date for the current podman version
	ErrConmonOutdated = errors.New("outdated conmon version")
	// ErrConmonDead indicates that the container's conmon process has been
	// killed, preventing normal operation.
	ErrConmonDead = errors.New("conmon process killed")

	// ErrNetworkOnPodContainer indicates the user wishes to alter network attributes on a container
	// in a pod.  This cannot be done as the infra container has all the network information
	ErrNetworkOnPodContainer = errors.New("network cannot be configured when it is shared with a pod")

	// ErrNetworkInUse indicates the requested operation failed because the network was in use
	ErrNetworkInUse = errors.New("network is being used")

	// ErrNetworkConnected indicates that the required operation failed because the container is already a network endpoint
	ErrNetworkConnected = errors.New("network is already connected")

	// ErrStoreNotInitialized indicates that the container storage was never
	// initialized.
	ErrStoreNotInitialized = errors.New("the container storage was never initialized")

	// ErrNoNetwork indicates that a container has no net namespace, like network=none
	ErrNoNetwork = errors.New("container has no network namespace")

	// ErrNetworkModeInvalid indicates that a container has the wrong network mode for an operation
	ErrNetworkModeInvalid = errors.New("invalid network mode")

	// ErrSetSecurityAttribute indicates that a request to set a container's security attribute
	// was not possible.
	ErrSetSecurityAttribute = fmt.Errorf("%w: unable to assign security attribute", ErrOCIRuntime)

	// ErrGetSecurityAttribute indicates that a request to get a container's security attribute
	// was not possible.
	ErrGetSecurityAttribute = fmt.Errorf("%w: unable to get security attribute", ErrOCIRuntime)

	// ErrSecurityAttribute indicates that an error processing security attributes
	// for the container
	ErrSecurityAttribute = fmt.Errorf("%w: unable to process security attribute", ErrOCIRuntime)

	// ErrCanceled indicates that an operation has been cancelled by a user.
	// Useful for potentially long running tasks.
	ErrCanceled = errors.New("cancelled by user")

	// ErrConmonVersionFormat is used when the expected version format of conmon
	// has changed.
	ErrConmonVersionFormat = "conmon version changed format"

	// ErrRemovingCtrs indicates that there was an error removing all
	// containers from a pod.
	ErrRemovingCtrs = errors.New("removing pod containers")

	// ErrHealthCheckTimeout indicates that a HealthCheck timed out.
	ErrHealthCheckTimeout = errors.New("healthcheck command exceeded timeout")
)
