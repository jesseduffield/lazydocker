//go:build !remote

package libpod

import (
	"fmt"
	"os"
	"runtime"

	"github.com/containers/podman/v5/libpod/define"
	"golang.org/x/sys/unix"
)

// joinMountAndExec executes the specified function `f` inside the container's
// mount and PID namespace.  That allows for having the exact view on the
// container's file system.
//
// Note, if the container is not running `f()` will be executed as is.
func (c *Container) joinMountAndExec(f func() error) error {
	if c.state.State != define.ContainerStateRunning {
		return f()
	}

	// Container's running, so we need to execute `f()` inside its mount NS.
	errChan := make(chan error)
	go func() {
		runtime.LockOSThread()

		// Join the mount and PID NS of the container.
		getFD := func(ns LinuxNS) (*os.File, error) {
			nsPath, err := c.namespacePath(ns)
			if err != nil {
				return nil, err
			}
			return os.Open(nsPath)
		}

		mountFD, err := getFD(MountNS)
		if err != nil {
			errChan <- err
			return
		}
		defer mountFD.Close()

		inHostPidNS, err := c.inHostPidNS()
		if err != nil {
			errChan <- fmt.Errorf("checking inHostPidNS: %w", err)
			return
		}
		var pidFD *os.File
		if !inHostPidNS {
			pidFD, err = getFD(PIDNS)
			if err != nil {
				errChan <- err
				return
			}
			defer pidFD.Close()
		}

		if err := unix.Unshare(unix.CLONE_NEWNS); err != nil {
			errChan <- err
			return
		}

		if pidFD != nil {
			if err := unix.Setns(int(pidFD.Fd()), unix.CLONE_NEWPID); err != nil {
				errChan <- err
				return
			}
		}
		if err := unix.Setns(int(mountFD.Fd()), unix.CLONE_NEWNS); err != nil {
			errChan <- err
			return
		}

		// Last but not least, execute the workload.
		errChan <- f()
	}()
	return <-errChan
}

func (c *Container) resolveCopyTarget(mountPoint string, containerPath string) (string, string, *Volume, error) {
	// If the container is running, we will execute the copy
	// inside the container's mount namespace so we return a path
	// relative to the container's root.
	if c.state.State == define.ContainerStateRunning {
		return "/", c.pathAbs(containerPath), nil, nil
	}
	return c.resolvePath(mountPoint, containerPath)
}
