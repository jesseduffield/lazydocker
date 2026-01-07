//go:build !remote

package libpod

import (
	"github.com/moby/sys/user"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func (c *Container) setProcessCapabilitiesExec(_ *ExecOptions, _ string, _ *user.ExecUser, _ *spec.Process) error {
	return nil
}
