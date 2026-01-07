//go:build !remote

package libpod

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/systemd"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/cgroups"
)

func checkCgroups2UnifiedMode(runtime *Runtime) {
	unified, _ := cgroups.IsCgroup2UnifiedMode()
	// DELETE ON RHEL9
	if !unified {
		_, ok := os.LookupEnv("PODMAN_IGNORE_CGROUPSV1_WARNING")
		if !ok {
			logrus.Warn("Using cgroups-v1 which is deprecated in favor of cgroups-v2 with Podman v5 and will be removed in a future version. Set environment variable `PODMAN_IGNORE_CGROUPSV1_WARNING` to hide this warning.")
		}
	}
	// DELETE ON RHEL9

	if unified && rootless.IsRootless() && !systemd.IsSystemdSessionValid(rootless.GetRootlessUID()) {
		// If user is rootless and XDG_RUNTIME_DIR is found, podman will not proceed with /tmp directory
		// it will try to use existing XDG_RUNTIME_DIR
		// if current user has no write access to XDG_RUNTIME_DIR we will fail later
		if err := unix.Access(runtime.storageConfig.RunRoot, unix.W_OK); err != nil {
			msg := fmt.Sprintf("RunRoot is pointing to a path (%s) which is not writable. Most likely podman will fail.", runtime.storageConfig.RunRoot)
			if errors.Is(err, os.ErrNotExist) {
				// if dir does not exist, try to create it
				if err := os.MkdirAll(runtime.storageConfig.RunRoot, 0o700); err != nil {
					logrus.Warn(msg)
				}
			} else {
				logrus.Warnf("%s: %v", msg, err)
			}
		}
	}
}

// Check the current boot ID against the ID cached in the runtime alive file.
func (r *Runtime) checkBootID(runtimeAliveFile string) error {
	systemBootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err == nil {
		podmanBootID, err := os.ReadFile(runtimeAliveFile)
		if err != nil {
			return fmt.Errorf("reading boot ID from runtime alive file: %w", err)
		}
		if len(podmanBootID) != 0 {
			if string(systemBootID) != string(podmanBootID) {
				return fmt.Errorf("current system boot ID differs from cached boot ID; an unhandled reboot has occurred. Please delete directories %q and %q and re-run Podman", r.storageConfig.RunRoot, r.config.Engine.TmpDir)
			}
		} else {
			// Write the current boot ID to the alive file.
			if err := os.WriteFile(runtimeAliveFile, systemBootID, 0o644); err != nil {
				return fmt.Errorf("writing boot ID to runtime alive file: %w", err)
			}
		}
	}
	return nil
}
