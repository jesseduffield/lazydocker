package containers

import (
	"bufio"
	"io"

	"github.com/containers/podman/v5/libpod/define"
)

// LogOptions describe finer control of log content or
// how the content is formatted.
//
//go:generate go run ../generator/generator.go LogOptions
type LogOptions struct {
	Follow     *bool
	Since      *string
	Stderr     *bool
	Stdout     *bool
	Tail       *string
	Timestamps *bool
	Until      *string
}

// CommitOptions describe details about the resulting committed
// image as defined by repo and tag. None of these options
// are required.
//
//go:generate go run ../generator/generator.go CommitOptions
type CommitOptions struct {
	Author  *string
	Changes []string
	Config  *io.Reader `schema:"-"`
	Comment *string
	Format  *string
	Pause   *bool
	Stream  *bool
	Squash  *bool
	Repo    *string
	Tag     *string
}

// AttachOptions are optional options for attaching to containers
//
//go:generate go run ../generator/generator.go AttachOptions
type AttachOptions struct {
	DetachKeys *string // Keys to detach from running container
	Logs       *bool   // Flag to return all logs from container when true
	Stream     *bool   // Flag only return container logs when false and Logs is true
}

// CheckpointOptions are optional options for checkpointing containers
//
//go:generate go run ../generator/generator.go CheckpointOptions
type CheckpointOptions struct {
	Export         *string
	CreateImage    *string
	IgnoreRootfs   *bool
	Keep           *bool
	LeaveRunning   *bool
	TCPEstablished *bool
	PrintStats     *bool
	PreCheckpoint  *bool
	WithPrevious   *bool
	FileLocks      *bool
}

// RestoreOptions are optional options for restoring containers
//
//go:generate go run ../generator/generator.go RestoreOptions
type RestoreOptions struct {
	IgnoreRootfs    *bool
	IgnoreVolumes   *bool
	IgnoreStaticIP  *bool
	IgnoreStaticMAC *bool
	// ImportAchive is the path to an archive which contains the checkpoint data.
	//
	// Deprecated: Use ImportArchive instead. This field name is a typo and
	// will be removed in a future major release.
	ImportAchive *string
	// ImportArchive is the path to an archive which contains the checkpoint data.
	// ImportArchive is preferred over ImportAchive when both are set.
	ImportArchive  *string
	Keep           *bool
	Name           *string
	TCPEstablished *bool
	TCPClose       *bool
	Pod            *string
	PrintStats     *bool
	PublishPorts   []string
	FileLocks      *bool
}

// CreateOptions are optional options for creating containers
//
//go:generate go run ../generator/generator.go CreateOptions
type CreateOptions struct{}

// DiffOptions are optional options for creating containers
//
//go:generate go run ../generator/generator.go DiffOptions
type DiffOptions struct {
	// By the default diff will compare against the parent layer. Change the Parent if you want to compare against something else.
	Parent *string
	// Change the type the backend should match. This can be set to "all", "container" or "image".
	DiffType *string
}

// ExecInspectOptions are optional options for inspecting
// exec sessions
//
//go:generate go run ../generator/generator.go ExecInspectOptions
type ExecInspectOptions struct{}

// ExecStartOptions are optional options for starting
// exec sessions
//
//go:generate go run ../generator/generator.go ExecStartOptions
type ExecStartOptions struct {
}

// HealthCheckOptions are optional options for checking
// the health of a container
//
//go:generate go run ../generator/generator.go HealthCheckOptions
type HealthCheckOptions struct{}

// MountOptions are optional options for mounting
// containers
//
//go:generate go run ../generator/generator.go MountOptions
type MountOptions struct{}

// UnmountOptions are optional options for unmounting
// containers
//
//go:generate go run ../generator/generator.go UnmountOptions
type UnmountOptions struct{}

// MountedContainerPathsOptions are optional options for getting
// container mount paths
//
//go:generate go run ../generator/generator.go MountedContainerPathsOptions
type MountedContainerPathsOptions struct{}

// ListOptions are optional options for listing containers
//
//go:generate go run ../generator/generator.go ListOptions
type ListOptions struct {
	All       *bool
	External  *bool
	Filters   map[string][]string
	Last      *int
	Namespace *bool
	Size      *bool
	Sync      *bool
}

// PruneOptions are optional options for pruning containers
//
//go:generate go run ../generator/generator.go PruneOptions
type PruneOptions struct {
	Filters map[string][]string
}

// RemoveOptions are optional options for removing containers
//
//go:generate go run ../generator/generator.go RemoveOptions
type RemoveOptions struct {
	Depend  *bool
	Ignore  *bool
	Force   *bool
	Volumes *bool
	Timeout *uint
}

// InspectOptions are optional options for inspecting containers
//
//go:generate go run ../generator/generator.go InspectOptions
type InspectOptions struct {
	Size *bool
}

// KillOptions are optional options for killing containers
//
//go:generate go run ../generator/generator.go KillOptions
type KillOptions struct {
	Signal *string
}

// PauseOptions are optional options for pausing containers
//
//go:generate go run ../generator/generator.go PauseOptions
type PauseOptions struct{}

// RestartOptions are optional options for restarting containers
//
//go:generate go run ../generator/generator.go RestartOptions
type RestartOptions struct {
	Timeout *int
}

// StartOptions are optional options for starting containers
//
//go:generate go run ../generator/generator.go StartOptions
type StartOptions struct {
	DetachKeys *string
	Recursive  *bool
}

// StatsOptions are optional options for getting stats on containers
//
//go:generate go run ../generator/generator.go StatsOptions
type StatsOptions struct {
	All      *bool
	Stream   *bool
	Interval *int
}

// TopOptions are optional options for getting running
// processes in containers
//
//go:generate go run ../generator/generator.go TopOptions
type TopOptions struct {
	Descriptors *[]string
}

// UnpauseOptions are optional options for unpausing containers
//
//go:generate go run ../generator/generator.go UnpauseOptions
type UnpauseOptions struct{}

// WaitOptions are optional options for waiting on containers
//
//go:generate go run ../generator/generator.go WaitOptions
type WaitOptions struct {
	// Conditions to wait on.  Includes container statuses such as
	// "running" or "stopped" and health-related values such "healthy".
	Conditions []string `schema:"condition"`
	// Time interval to wait before polling for completion.
	Interval *string
	// Container status to wait on.
	// Deprecated: use Conditions instead.
	Condition []define.ContainerStatus
}

// StopOptions are optional options for stopping containers
//
//go:generate go run ../generator/generator.go StopOptions
type StopOptions struct {
	Ignore  *bool
	Timeout *uint
}

// ExportOptions are optional options for exporting containers
//
//go:generate go run ../generator/generator.go ExportOptions
type ExportOptions struct{}

// InitOptions are optional options for initing containers
//
//go:generate go run ../generator/generator.go InitOptions
type InitOptions struct{}

// ShouldRestartOptions
//
//go:generate go run ../generator/generator.go ShouldRestartOptions
type ShouldRestartOptions struct{}

// RenameOptions are options for renaming containers.
// The Name field is required.
//
//go:generate go run ../generator/generator.go RenameOptions
type RenameOptions struct {
	Name *string
}

// ResizeTTYOptions are optional options for resizing
// container TTYs
//
//go:generate go run ../generator/generator.go ResizeTTYOptions
type ResizeTTYOptions struct {
	Height  *int
	Width   *int
	Running *bool
}

// ResizeExecTTYOptions are optional options for resizing
// container ExecTTYs
//
//go:generate go run ../generator/generator.go ResizeExecTTYOptions
type ResizeExecTTYOptions struct {
	Height *int
	Width  *int
}

// ExecStartAndAttachOptions are optional options for resizing
// container ExecTTYs
//
//go:generate go run ../generator/generator.go ExecStartAndAttachOptions
type ExecStartAndAttachOptions struct {
	// OutputStream will be attached to container's STDOUT
	OutputStream *io.Writer
	// ErrorStream will be attached to container's STDERR
	ErrorStream *io.Writer
	// InputStream will be attached to container's STDIN
	InputStream *bufio.Reader
	// AttachOutput is whether to attach to STDOUT
	// If false, stdout will not be attached
	AttachOutput *bool
	// AttachError is whether to attach to STDERR
	// If false, stdout will not be attached
	AttachError *bool
	// AttachInput is whether to attach to STDIN
	// If false, stdout will not be attached
	AttachInput *bool
}

// ExistsOptions are optional options for checking if a container exists
//
//go:generate go run ../generator/generator.go ExistsOptions
type ExistsOptions struct {
	// External checks for containers created outside of Podman
	External *bool
}

// CopyOptions are options for copying to containers.
//
//go:generate go run ../generator/generator.go CopyOptions
type CopyOptions struct {
	// If used with CopyFromArchive and set to true it will change ownership of files from the source tar archive
	// to the primary uid/gid of the target container.
	Chown *bool `schema:"copyUIDGID"`
	// Map to translate path names.
	Rename map[string]string
	// NoOverwriteDirNonDir when true prevents an existing directory or file from being overwritten
	// by the other type.
	NoOverwriteDirNonDir *bool
}

// ExecRemoveOptions are optional options for removing an exec session
//
//go:generate go run ../generator/generator.go ExecRemoveOptions
type ExecRemoveOptions struct {
	Force *bool
}
