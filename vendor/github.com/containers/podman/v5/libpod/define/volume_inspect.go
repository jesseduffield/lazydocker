package define

import (
	"time"
)

// InspectVolumeData is the output of Inspect() on a volume. It is matched to
// the format of 'docker volume inspect'.
type InspectVolumeData struct {
	// Name is the name of the volume.
	Name string `json:"Name"`
	// Driver is the driver used to create the volume.
	// If set to "local" or "", the Local driver (Podman built-in code) is
	// used to service the volume; otherwise, a volume plugin with the given
	// name is used to mount and manage the volume.
	Driver string `json:"Driver"`
	// Mountpoint is the path on the host where the volume is mounted.
	Mountpoint string `json:"Mountpoint"`
	// CreatedAt is the date and time the volume was created at. This is not
	// stored for older Libpod volumes; if so, it will be omitted.
	CreatedAt time.Time `json:"CreatedAt"`
	// Status is used to return information on the volume's current state,
	// if the volume was created using a volume plugin (uses a Driver that
	// is not the local driver).
	// Status is provided to us by an external program, so no guarantees are
	// made about its format or contents. Further, it is an optional field,
	// so it may not be set even in cases where a volume plugin is in use.
	Status map[string]any `json:"Status,omitempty"`
	// Labels includes the volume's configured labels, key:value pairs that
	// can be passed during volume creation to provide information for third
	// party tools.
	Labels map[string]string `json:"Labels"`
	// Scope is unused and provided solely for Docker compatibility. It is
	// unconditionally set to "local".
	Scope string `json:"Scope"`
	// Options is a set of options that were used when creating the volume.
	// For the Local driver, these are mount options that will be used to
	// determine how a local filesystem is mounted; they are handled as
	// parameters to Mount in a manner described in the volume create
	// manpage.
	// For non-local drivers, these are passed as-is to the volume plugin.
	Options map[string]string `json:"Options"`
	// UID is the UID that the volume was created with.
	UID int `json:"UID,omitempty"`
	// GID is the GID that the volume was created with.
	GID int `json:"GID,omitempty"`
	// Anonymous indicates that the volume was created as an anonymous
	// volume for a specific container, and will be removed when any
	// container using it is removed.
	Anonymous bool `json:"Anonymous,omitempty"`
	// MountCount is the number of times this volume has been mounted.
	MountCount uint `json:"MountCount"`
	// NeedsCopyUp indicates that the next time the volume is mounted into
	NeedsCopyUp bool `json:"NeedsCopyUp,omitempty"`
	// NeedsChown indicates that the next time the volume is mounted into
	// a container, the container will chown the volume to the container process
	// UID/GID.
	NeedsChown bool `json:"NeedsChown,omitempty"`
	// Timeout is the specified driver timeout if given
	Timeout uint `json:"Timeout,omitempty"`
	// StorageID is the ID of the container backing the volume in c/storage.
	// Only used with Image Volumes.
	StorageID string `json:"StorageID,omitempty"`
	// LockNumber is the number of the volume's Libpod lock.
	LockNumber uint32
}

type VolumeReload struct {
	Added   []string
	Removed []string
	Errors  []error
}
