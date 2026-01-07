//go:build !remote && !systemd

package libpod

import (
	"context"
)

// createTimer systemd timers for healthchecks of a container
func (c *Container) createTimer(_ string, _ bool) error {
	return nil
}

// startTimer starts a systemd timer for the healthchecks
func (c *Container) startTimer(_ bool) error {
	return nil
}

// removeTransientFiles removes the systemd timer and unit files
// for the container
func (c *Container) removeTransientFiles(_ context.Context, _ bool, _ string) error {
	return nil
}
