//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/errorhandling"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	"go.podman.io/common/libnetwork/types"
	blobinfocache "go.podman.io/image/v5/pkg/blobinfocache"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/lockfile"
	stypes "go.podman.io/storage/types"
)

// removeAllDirs removes all Podman storage directories. It is intended to be
// used as a backup for reset() when that function cannot be used due to
// failures in initializing libpod.
// It does not expect that all the directories match what is in use by Podman,
// as this is a common failure point for `system reset`. As such, our ability to
// interface with containers and pods is somewhat limited.
// This function assumes that we do not have a working c/storage store.
func (r *Runtime) removeAllDirs() error {
	var lastErr error

	// Grab the runtime alive lock.
	// This ensures that no other Podman process can run while we are doing
	// a reset, so no race conditions with containers/pods/etc being created
	// while we are resetting storage.
	// TODO: maybe want a helper for getting the path? This is duped from
	// runtime.go
	runtimeAliveLock := filepath.Join(r.config.Engine.TmpDir, "alive.lck")
	aliveLock, err := lockfile.GetLockFile(runtimeAliveLock)
	if err != nil {
		logrus.Errorf("Lock runtime alive lock %s: %v", runtimeAliveLock, err)
	} else {
		aliveLock.Lock()
		defer aliveLock.Unlock()
	}

	// We do not have a store - so we can't really try and remove containers
	// or pods or volumes...
	// Try and remove the directories, in hopes that they are unmounted.
	// This is likely to fail but it's the best we can do.

	// Volume path
	if err := os.RemoveAll(r.config.Engine.VolumePath); err != nil {
		lastErr = fmt.Errorf("removing volume path: %w", err)
	}

	// Tmpdir
	if err := os.RemoveAll(r.config.Engine.TmpDir); err != nil {
		if lastErr != nil {
			logrus.Errorf("Reset: %v", lastErr)
		}
		lastErr = fmt.Errorf("removing tmp dir: %w", err)
	}

	// Runroot
	if err := os.RemoveAll(r.storageConfig.RunRoot); err != nil {
		if lastErr != nil {
			logrus.Errorf("Reset: %v", lastErr)
		}
		lastErr = fmt.Errorf("removing run root: %w", err)
	}

	// Static dir
	if err := os.RemoveAll(r.config.Engine.StaticDir); err != nil {
		if lastErr != nil {
			logrus.Errorf("Reset: %v", lastErr)
		}
		lastErr = fmt.Errorf("removing static dir: %w", err)
	}

	// Graph root
	if err := os.RemoveAll(r.storageConfig.GraphRoot); err != nil {
		if lastErr != nil {
			logrus.Errorf("Reset: %v", lastErr)
		}
		lastErr = fmt.Errorf("removing graph root: %w", err)
	}

	return lastErr
}

// Reset removes all Libpod files.
// All containers, images, volumes, pods, and networks will be removed.
// Calls Shutdown(), rendering the runtime unusable after this is run.
func (r *Runtime) Reset(ctx context.Context) error {
	// Acquire the alive lock and hold it.
	// Ensures that we don't let other Podman commands run while we are
	// removing everything.
	aliveLock, err := r.getRuntimeAliveLock()
	if err != nil {
		return fmt.Errorf("retrieving alive lock: %w", err)
	}
	aliveLock.Lock()
	defer aliveLock.Unlock()

	if !r.valid {
		return define.ErrRuntimeStopped
	}

	var timeout uint = 0
	pods, err := r.GetAllPods()
	if err != nil {
		return err
	}
	for _, p := range pods {
		if ctrs, err := r.RemovePod(ctx, p, true, true, &timeout); err != nil {
			if errors.Is(err, define.ErrNoSuchPod) {
				continue
			}
			for ctr, err := range ctrs {
				if err != nil {
					logrus.Errorf("Error removing pod %s container %s: %v", p.ID(), ctr, err)
				}
			}
			logrus.Errorf("Removing Pod %s: %v", p.ID(), err)
		}
	}

	ctrs, err := r.GetAllContainers()
	if err != nil {
		return err
	}

	for _, c := range ctrs {
		if ctrs, _, err := r.RemoveContainerAndDependencies(ctx, c, true, true, &timeout); err != nil {
			for ctr, err := range ctrs {
				logrus.Errorf("Error removing container %s: %v", ctr, err)
			}

			if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
				continue
			}

			logrus.Errorf("Removing container %s: %v", c.ID(), err)

			// There's no point trying a storage container removal
			// here. High likelihood it doesn't work
			// (RemoveStorageContainer will refuse to remove if a
			// container exists in the Libpod database) and, even if
			// it would, we'll get the container later on during
			// image removal.
		}
	}

	if err := r.stopPauseProcess(); err != nil {
		logrus.Errorf("Stopping pause process: %v", err)
	}

	// Volumes before images, as volumes can mount images.
	volumes, err := r.state.AllVolumes()
	if err != nil {
		return err
	}
	for _, v := range volumes {
		if err := r.RemoveVolume(ctx, v, true, &timeout); err != nil {
			if errors.Is(err, define.ErrNoSuchVolume) {
				continue
			}
			logrus.Errorf("Removing volume %s: %v", v.config.Name, err)
		}
	}

	// Set force and ignore.
	// Ignore shouldn't be necessary, but it seems safer. We want everything
	// gone anyways...
	rmiOptions := &libimage.RemoveImagesOptions{
		Force:               true,
		Ignore:              true,
		RemoveContainerFunc: r.RemoveContainersForImageCallback(ctx, true),
		Filters:             []string{"readonly=false"},
	}
	if _, rmiErrors := r.LibimageRuntime().RemoveImages(ctx, nil, rmiOptions); rmiErrors != nil {
		return errorhandling.JoinErrors(rmiErrors)
	}

	// remove all networks
	nets, err := r.network.NetworkList()
	if err != nil {
		return err
	}
	for _, net := range nets {
		// do not delete the default network
		if net.Name == r.network.DefaultNetworkName() {
			continue
		}
		// ignore not exists errors because of the TOCTOU problem
		if err := r.network.NetworkRemove(net.Name); err != nil && !errors.Is(err, types.ErrNoSuchNetwork) {
			logrus.Errorf("Removing network %s: %v", net.Name, err)
		}
	}

	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir != "" {
		xdgRuntimeDir, err = filepath.EvalSymlinks(xdgRuntimeDir)
		if err != nil {
			return err
		}
	}
	_, prevError := r.store.Shutdown(true)
	graphRoot := filepath.Clean(r.store.GraphRoot())
	if graphRoot == xdgRuntimeDir {
		if prevError != nil {
			logrus.Error(prevError)
		}
		prevError = fmt.Errorf("failed to remove runtime graph root dir %s, since it is the same as XDG_RUNTIME_DIR", graphRoot)
	} else {
		if err := os.RemoveAll(graphRoot); err != nil {
			if prevError != nil {
				logrus.Error(prevError)
			}
			prevError = err
		}
	}
	runRoot := filepath.Clean(r.store.RunRoot())
	if runRoot == xdgRuntimeDir {
		if prevError != nil {
			logrus.Error(prevError)
		}
		prevError = fmt.Errorf("failed to remove runtime root dir %s, since it is the same as XDG_RUNTIME_DIR", runRoot)
	} else {
		if err := os.RemoveAll(runRoot); err != nil {
			if prevError != nil {
				logrus.Error(prevError)
			}
			prevError = err
		}
	}
	runtimeDir, err := util.GetRootlessRuntimeDir()
	if err != nil {
		return err
	}
	tempDir := r.config.Engine.TmpDir
	if tempDir == runtimeDir {
		tempDir = filepath.Join(tempDir, "containers")
	}
	if filepath.Clean(tempDir) == xdgRuntimeDir {
		if prevError != nil {
			logrus.Error(prevError)
		}
		prevError = fmt.Errorf("failed to remove runtime tmpdir %s, since it is the same as XDG_RUNTIME_DIR", tempDir)
	} else {
		if err := os.RemoveAll(tempDir); err != nil {
			if prevError != nil {
				logrus.Error(prevError)
			}
			prevError = err
		}
	}

	if err := blobinfocache.CleanupDefaultCache(nil); err != nil {
		if prevError != nil {
			logrus.Error(prevError)
		}
		prevError = err
	}

	if storageConfPath, err := storage.DefaultConfigFile(); err == nil {
		switch storageConfPath {
		case stypes.SystemConfigFile:
			break
		default:
			if _, err = os.Stat(storageConfPath); err == nil {
				fmt.Printf(" A %q config file exists.\n", storageConfPath)
				fmt.Println("Remove this file if you did not modify the configuration.")
			}
		}
	} else {
		if prevError != nil {
			logrus.Error(prevError)
		}
		prevError = err
	}

	// Shut down the runtime, it's no longer usable after mass-deletion.
	if err := r.Shutdown(false); err != nil {
		if prevError != nil {
			logrus.Error(prevError)
		}
		prevError = err
	}

	return prevError
}
