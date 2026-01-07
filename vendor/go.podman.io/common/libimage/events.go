//go:build !remote

package libimage

import (
	"time"

	"github.com/sirupsen/logrus"
)

// EventType indicates the type of an event.  Currently, there is only one
// supported type for container image but we may add more (e.g., for manifest
// lists) in the future.
type EventType int

const (
	// EventTypeUnknown is an uninitialized EventType.
	EventTypeUnknown EventType = iota
	// EventTypeImagePull represents an image pull.
	EventTypeImagePull
	// EventTypeImagePullError represents an image pull failed.
	EventTypeImagePullError
	// EventTypeImagePush represents an image push.
	EventTypeImagePush
	// EventTypeImageRemove represents an image removal.
	EventTypeImageRemove
	// EventTypeImageLoad represents an image being loaded.
	EventTypeImageLoad
	// EventTypeImageSave represents an image being saved.
	EventTypeImageSave
	// EventTypeImageTag represents an image being tagged.
	EventTypeImageTag
	// EventTypeImageUntag represents an image being untagged.
	EventTypeImageUntag
	// EventTypeImageMount represents an image being mounted.
	EventTypeImageMount
	// EventTypeImageUnmount represents an image being unmounted.
	EventTypeImageUnmount
)

// Event represents an event such an image pull or image tag.
type Event struct {
	// ID of the object (e.g., image ID).
	ID string
	// Name of the object (e.g., image name "quay.io/containers/podman:latest")
	Name string
	// Time of the event.
	Time time.Time
	// Type of the event.
	Type EventType
	// Error in case of failure.
	Error error
}

// writeEvent writes the specified event to the Runtime's event channel.  The
// event is discarded if no event channel has been registered (yet).
func (r *Runtime) writeEvent(event *Event) {
	select {
	case r.eventChannel <- event:
		// Done
	case <-time.After(2 * time.Second):
		// The Runtime's event channel has a buffer of size 100 which
		// should be enough even under high load.  However, we
		// shouldn't block too long in case the buffer runs full (could
		// be an honest user error or bug).
		logrus.Warnf("Discarding libimage event which was not read within 2 seconds: %v", event)
	}
}
