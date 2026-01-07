//go:build !remote && (!linux || !systemd)

package libpod

import (
	"context"
	"fmt"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/logs"
)

func (c *Container) readFromJournal(_ context.Context, _ *logs.LogOptions, _ chan *logs.LogLine, _ int64, _ string) error {
	return fmt.Errorf("journald logging only enabled with systemd on linux: %w", define.ErrOSNotSupported)
}
