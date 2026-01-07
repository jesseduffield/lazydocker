package events

import (
	"context"
	"errors"
	"time"
)

// EventerType describes the type of event logger
// The string values for EventerType should be entirely lowercase.
type EventerType string

const (
	// LogFile indicates the event logger will be a logfile
	LogFile EventerType = "file"
	// Journald indicates journald should be used to log events
	Journald EventerType = "journald"
	// Null is a no-op events logger. It does not read or write events.
	Null EventerType = "none"
)

// Event describes the attributes of a libpod event
type Event struct {
	// ContainerExitCode is for storing the exit code of a container which can
	// be used for "internal" event notification
	ContainerExitCode *int `json:",omitempty"`
	// ID can be for the container, image, volume, etc
	ID string `json:",omitempty"`
	// Image used where applicable
	Image string `json:",omitempty"`
	// Name where applicable
	Name string `json:",omitempty"`
	// Network is the network name in a network event
	Network string `json:"network,omitempty"`
	// Status describes the event that occurred
	Status Status
	// Time the event occurred
	Time time.Time
	// Type of event that occurred
	Type Type
	// Health status of the current container
	HealthStatus string `json:"health_status,omitempty"`
	// Healthcheck log of the current container
	HealthLog string `json:"health_log,omitempty"`
	// HealthFailingStreak log of the current container
	HealthFailingStreak int `json:"health_failing_streak,omitempty"`
	// Error code for certain events involving errors.
	Error string `json:"error,omitempty"`

	Details
}

// Details describes specifics about certain events, specifically around
// container events
type Details struct {
	// ContainerInspectData includes the payload of the container's inspect
	// data. Only set when events_container_create_inspect_data is set true
	// in containers.conf.
	ContainerInspectData string `json:",omitempty"`
	// PodID is the ID of the pod associated with the container.
	PodID string `json:",omitempty"`
	// Attributes can be used to describe specifics about the event
	// in the case of a container event, labels for example
	Attributes map[string]string
}

// EventerOptions describe options that need to be passed to create
// an eventer
type EventerOptions struct {
	// EventerType describes whether to use journald, file or memory
	EventerType string
	// LogFilePath is the path to where the log file should reside if using
	// the file logger
	LogFilePath string
	// LogFileMaxSize is the default limit used for rotating the log file
	LogFileMaxSize uint64
}

// Eventer is the interface for journald or file event logging
type Eventer interface {
	// Write an event to a backend
	Write(event Event) error
	// Read an event from the backend
	Read(ctx context.Context, options ReadOptions) error
	// String returns the type of event logger
	String() string
}

type ReadResult struct {
	Event *Event
	Error error
}

// ReadOptions describe the attributes needed to read event logs
type ReadOptions struct {
	// EventChannel is the comm path back to user
	EventChannel chan ReadResult
	// Filters are key/value pairs that describe to limit output
	Filters []string
	// FromStart means you start reading from the start of the logs
	FromStart bool
	// Since reads "since" the given time
	Since string
	// Stream is follow
	Stream bool
	// Until reads "until" the given time
	Until string
}

// Type of event that occurred (container, volume, image, pod, etc)
type Type string

// Status describes the actual event action (stop, start, create, kill)
type Status string

// When updating this list below please also update the shell completion list in
// cmd/podman/common/completion.go and the StringToXXX function in events.go.
const (
	// Container - event is related to containers
	Container Type = "container"
	// Image - event is related to images
	Image Type = "image"
	// Network - event is related to networks
	Network Type = "network"
	// Pod - event is related to pods
	Pod Type = "pod"
	// System - event is related to Podman whole and not to any specific
	// container/pod/image/volume
	System Type = "system"
	// Volume - event is related to volumes
	Volume Type = "volume"
	// Machine - event is related to machine VM's
	Machine Type = "machine"
	// Secret - event is related to secrets
	Secret Type = "secret"

	// Attach ...
	Attach Status = "attach"
	// AutoUpdate ...
	AutoUpdate Status = "auto-update"
	// Build ...
	Build Status = "build"
	// Checkpoint ...
	Checkpoint Status = "checkpoint"
	// Cleanup ...
	Cleanup Status = "cleanup"
	// Commit ...
	Commit Status = "commit"
	// Copy ...
	Copy Status = "copy"
	// Create ...
	Create Status = "create"
	// Exec ...
	Exec Status = "exec"
	// ExecDied indicates that an exec session in a container died.
	ExecDied Status = "exec_died"
	// Exited indicates that a container's process died
	Exited Status = "died"
	// Export ...
	Export Status = "export"
	// HealthStatus ...
	HealthStatus Status = "health_status"
	// History ...
	History Status = "history"
	// Import ...
	Import Status = "import"
	// Init ...
	Init Status = "init"
	// Kill ...
	Kill Status = "kill"
	// LoadFromArchive ...
	LoadFromArchive Status = "loadfromarchive"
	// Mount ...
	Mount Status = "mount"
	// NetworkConnect
	NetworkConnect Status = "connect"
	// NetworkDisconnect
	NetworkDisconnect Status = "disconnect"
	// Pause ...
	Pause Status = "pause"
	// Prune ...
	Prune Status = "prune"
	// Pull ...
	Pull Status = "pull"
	// PullError is an error pulling an image
	PullError Status = "pull-error"
	// Push ...
	Push Status = "push"
	// Refresh indicates that the system refreshed the state after a
	// reboot.
	Refresh Status = "refresh"
	// Remove ...
	Remove Status = "remove"
	// Rename indicates that a container was renamed
	Rename Status = "rename"
	// Renumber indicates that lock numbers were reallocated at user
	// request.
	Renumber Status = "renumber"
	// Restart indicates that the target was restarted via an API call.
	Restart Status = "restart"
	// Restore ...
	Restore Status = "restore"
	// Rotate indicates that the log file was rotated
	Rotate Status = "log-rotation"
	// Save ...
	Save Status = "save"
	// Start ...
	Start Status = "start"
	// Stop ...
	Stop Status = "stop"
	// Sync ...
	Sync Status = "sync"
	// Tag ...
	Tag Status = "tag"
	// Unmount ...
	Unmount Status = "unmount"
	// Unpause ...
	Unpause Status = "unpause"
	// Untag ...
	Untag Status = "untag"
	// Update indicates that a container's configuration has been modified.
	Update Status = "update"
)

// EventFilter for filtering events
type EventFilter func(*Event) bool

var (
	// ErrEventTypeBlank indicates the event log found something done by podman
	// but it isn't likely an event
	ErrEventTypeBlank = errors.New("event type blank")

	// ErrEventNotFound indicates that the event was not found in the event log
	ErrEventNotFound = errors.New("unable to find event")
)
