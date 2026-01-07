//go:build !remote

package libpod

import (
	"github.com/moby/sys/user"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/common/pkg/capabilities"
)

func (c *Container) setProcessCapabilitiesExec(options *ExecOptions, user string, execUser *user.ExecUser, pspec *spec.Process) error {
	ctrSpec, err := c.specFromState()
	if err != nil {
		return err
	}

	allCaps, err := capabilities.BoundingSet()
	if err != nil {
		return err
	}
	if options.Privileged {
		pspec.Capabilities.Bounding = allCaps
	} else {
		pspec.Capabilities.Bounding = ctrSpec.Process.Capabilities.Bounding
	}

	// Always unset the inheritable capabilities similarly to what the Linux kernel does
	// They are used only when using capabilities with uid != 0.
	pspec.Capabilities.Inheritable = []string{}

	if execUser.Uid == 0 {
		pspec.Capabilities.Effective = pspec.Capabilities.Bounding
		pspec.Capabilities.Permitted = pspec.Capabilities.Bounding
	} else if user == c.config.User {
		pspec.Capabilities.Effective = ctrSpec.Process.Capabilities.Effective
		pspec.Capabilities.Inheritable = ctrSpec.Process.Capabilities.Effective
		pspec.Capabilities.Permitted = ctrSpec.Process.Capabilities.Effective
		pspec.Capabilities.Ambient = ctrSpec.Process.Capabilities.Effective
	}
	return nil
}
