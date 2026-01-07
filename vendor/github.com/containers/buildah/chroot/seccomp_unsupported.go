//go:build (!linux && !freebsd) || !seccomp

package chroot

import (
	"errors"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func setSeccomp(spec *specs.Spec) error {
	if spec.Linux.Seccomp != nil {
		return errors.New("configured a seccomp filter without seccomp support?")
	}
	return nil
}
