//go:build !linux && !freebsd

package chroot

import (
	"errors"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func setSelinuxLabel(spec *specs.Spec) error {
	if spec.Linux.MountLabel != "" {
		return errors.New("configured an SELinux mount label without SELinux support?")
	}
	if spec.Process.SelinuxLabel != "" {
		return errors.New("configured an SELinux process label without SELinux support?")
	}
	return nil
}
