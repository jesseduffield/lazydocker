package types

import (
	dockerEvents "github.com/docker/docker/api/types/events"
)

// Event combines various event-related data such as time, event type, status
// and more.
type Event struct {
	// TODO: it would be nice to have full control over the types at some
	// point and fork such Docker types.
	dockerEvents.Message
	HealthStatus string `json:",omitempty"`
}
