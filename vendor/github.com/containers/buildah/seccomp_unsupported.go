//go:build !seccomp || !linux

package buildah

import (
	"github.com/opencontainers/runtime-spec/specs-go"
)

func setupSeccomp(spec *specs.Spec, seccompProfilePath string) error {
	if spec.Linux != nil {
		// runtime-tools may have supplied us with a default filter
		spec.Linux.Seccomp = nil
	}
	return nil
}
