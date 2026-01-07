//go:build !remote

package libpod

import (
	"errors"
	"os"
	"os/exec"
)

func (r *ConmonOCIRuntime) createRootlessContainer(_ *Container, _ *ContainerCheckpointOptions, _ bool) (int64, error) {
	return -1, errors.New("unsupported (*ConmonOCIRuntime) createRootlessContainer")
}

// Run the closure with the container's socket label set
func (r *ConmonOCIRuntime) withContainerSocketLabel(_ *Container, closure func() error) error {
	// No label support yet
	return closure()
}

// moveConmonToCgroupAndSignal gets a container's cgroupParent and moves the conmon process to that cgroup
// it then signals for conmon to start by sending nonce data down the start fd
func (r *ConmonOCIRuntime) moveConmonToCgroupAndSignal(_ *Container, _ *exec.Cmd, startFd *os.File) error {
	// No equivalent to cgroup on FreeBSD, just signal conmon to start
	if err := writeConmonPipeData(startFd); err != nil {
		return err
	}
	return nil
}

func moveToRuntimeCgroup() error {
	return errors.New("moveToRuntimeCgroup not supported on freebsd")
}
