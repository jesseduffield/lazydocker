//go:build !remote

package libpod

import (
	"errors"
	"fmt"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage"
)

// StorageContainer represents a container present in c/storage but not in
// libpod.
type StorageContainer struct {
	ID              string
	Names           []string
	Image           string
	CreateTime      time.Time
	PresentInLibpod bool
}

// ListStorageContainers lists all containers visible to c/storage.
func (r *Runtime) ListStorageContainers() ([]*StorageContainer, error) {
	finalCtrs := []*StorageContainer{}

	ctrs, err := r.store.Containers()
	if err != nil {
		return nil, err
	}

	for _, ctr := range ctrs {
		storageCtr := new(StorageContainer)
		storageCtr.ID = ctr.ID
		storageCtr.Names = ctr.Names
		storageCtr.Image = ctr.ImageID
		storageCtr.CreateTime = ctr.Created

		// Look up if container is in state
		hasCtr, err := r.state.HasContainer(ctr.ID)
		if err != nil {
			return nil, fmt.Errorf("looking up container %s in state: %w", ctr.ID, err)
		}

		storageCtr.PresentInLibpod = hasCtr

		finalCtrs = append(finalCtrs, storageCtr)
	}

	return finalCtrs, nil
}

func (r *Runtime) StorageContainer(idOrName string) (*storage.Container, error) {
	return r.store.Container(idOrName)
}

// RemoveStorageContainer removes a container from c/storage.
// The container WILL NOT be removed if it exists in libpod.
// Accepts ID or full name of container.
// If force is set, the container will be unmounted first to ensure removal.
func (r *Runtime) RemoveStorageContainer(idOrName string, force bool) error {
	targetID, err := r.store.Lookup(idOrName)
	if err != nil {
		if errors.Is(err, storage.ErrLayerUnknown) {
			return fmt.Errorf("no container with ID or name %q found: %w", idOrName, define.ErrNoSuchCtr)
		}
		return fmt.Errorf("looking up container %q: %w", idOrName, err)
	}

	// Lookup returns an ID but it's not guaranteed to be a container ID.
	// So we can still error here.
	ctr, err := r.store.Container(targetID)
	if err != nil {
		if errors.Is(err, storage.ErrContainerUnknown) {
			return fmt.Errorf("%q does not refer to a container: %w", idOrName, define.ErrNoSuchCtr)
		}
		return fmt.Errorf("retrieving container %q: %w", idOrName, err)
	}

	// Error out if the container exists in libpod
	exists, err := r.state.HasContainer(ctr.ID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("refusing to remove %q as it exists in libpod as container %s: %w", idOrName, ctr.ID, define.ErrCtrExists)
	}

	// Error out if this is an image-backed volume
	allVols, err := r.state.AllVolumes()
	if err != nil {
		return err
	}
	for _, vol := range allVols {
		if vol.config.Driver == define.VolumeDriverImage && vol.config.StorageID == ctr.ID {
			return fmt.Errorf("refusing to remove %q as it exists in libpod as an image-backed volume %s: %w", idOrName, vol.Name(), define.ErrCtrExists)
		}
	}

	if !force {
		timesMounted, err := r.store.Mounted(ctr.ID)
		if err != nil {
			if errors.Is(err, storage.ErrContainerUnknown) {
				// Container was removed from under us.
				// It's gone, so don't bother erroring.
				logrus.Infof("Storage for container %s already removed", ctr.ID)
				return nil
			}
			logrus.Warnf("Checking if container %q is mounted, attempting to delete: %v", idOrName, err)
		}
		if timesMounted > 0 {
			return fmt.Errorf("container %q is mounted and cannot be removed without using force: %w", idOrName, define.ErrCtrStateInvalid)
		}
	} else if _, err := r.store.Unmount(ctr.ID, true); err != nil {
		if errors.Is(err, storage.ErrContainerUnknown) {
			// Container again gone, no error
			logrus.Infof("Storage for container %s already removed", ctr.ID)
			return nil
		}
		logrus.Warnf("Unmounting container %q while attempting to delete storage: %v", idOrName, err)
	}

	if err := r.store.DeleteContainer(ctr.ID); err != nil {
		if errors.Is(err, storage.ErrNotAContainer) || errors.Is(err, storage.ErrContainerUnknown) {
			// Container again gone, no error
			logrus.Infof("Storage for container %s already removed", ctr.ID)
			return nil
		}
		return fmt.Errorf("removing storage for container %q: %w", idOrName, err)
	}

	return nil
}
