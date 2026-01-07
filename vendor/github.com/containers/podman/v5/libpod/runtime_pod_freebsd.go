//go:build !remote

package libpod

import (
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func (r *Runtime) platformMakePod(_ *Pod, _ *spec.LinuxResources) (string, error) {
	return "", nil
}

func (p *Pod) removePodCgroup() error {
	return nil
}
