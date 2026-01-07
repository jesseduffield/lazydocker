package events

import (
	"context"
	"errors"
)

// EventToNull is an eventer type that does nothing.
// It is meant for unit tests only
type EventToNull struct{}

// Write eats the event and always returns nil
func (e EventToNull) Write(_ Event) error {
	return nil
}

// Read does nothing and returns an error.
func (e EventToNull) Read(_ context.Context, _ ReadOptions) error {
	return errors.New("cannot read events with the \"none\" backend")
}

// String returns a string representation of the logger
func (e EventToNull) String() string {
	return "none"
}
