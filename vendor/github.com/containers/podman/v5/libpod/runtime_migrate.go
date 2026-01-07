//go:build !remote

package libpod

import (
	"fmt"
	"path/filepath"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
)

// Migrate stops the rootless pause process and performs any necessary database
// migrations that are required. It can also migrate all containers to a new OCI
// runtime, if requested.
func (r *Runtime) Migrate(newRuntime string) error {
	// Acquire the alive lock and hold it.
	// Ensures that we don't let other Podman commands run while we are
	// rewriting things in the DB.
	aliveLock, err := r.getRuntimeAliveLock()
	if err != nil {
		return fmt.Errorf("retrieving alive lock: %w", err)
	}
	aliveLock.Lock()
	defer aliveLock.Unlock()

	if !r.valid {
		return define.ErrRuntimeStopped
	}

	runningContainers, err := r.GetRunningContainers()
	if err != nil {
		return err
	}

	allCtrs, err := r.state.AllContainers(false)
	if err != nil {
		return err
	}

	logrus.Infof("Stopping all containers")
	for _, ctr := range runningContainers {
		fmt.Printf("stopped %s\n", ctr.ID())
		if err := ctr.Stop(); err != nil {
			return fmt.Errorf("cannot stop container %s: %w", ctr.ID(), err)
		}
	}

	// Did the user request a new runtime?
	runtimeChangeRequested := newRuntime != ""
	var requestedRuntime OCIRuntime
	if runtimeChangeRequested {
		runtime, exists := r.ociRuntimes[newRuntime]
		if !exists {
			return fmt.Errorf("change to runtime %q requested but no such runtime is defined: %w", newRuntime, define.ErrInvalidArg)
		}
		requestedRuntime = runtime
	}

	for _, ctr := range allCtrs {
		needsWrite := false

		// Reset pause process location
		oldLocation := filepath.Join(ctr.state.RunDir, "conmon.pid")
		if ctr.config.ConmonPidFile == oldLocation {
			logrus.Infof("Changing conmon PID file for %s", ctr.ID())
			ctr.config.ConmonPidFile = filepath.Join(ctr.config.StaticDir, "conmon.pid")
			needsWrite = true
		}

		// Reset runtime
		if runtimeChangeRequested && ctr.config.OCIRuntime != newRuntime {
			logrus.Infof("Resetting container %s runtime to runtime %s", ctr.ID(), newRuntime)
			ctr.config.OCIRuntime = newRuntime
			ctr.ociRuntime = requestedRuntime

			needsWrite = true
		}

		if needsWrite {
			if err := r.state.RewriteContainerConfig(ctr, ctr.config); err != nil {
				return fmt.Errorf("rewriting config for container %s: %w", ctr.ID(), err)
			}
		}
	}

	return r.stopPauseProcess()
}
