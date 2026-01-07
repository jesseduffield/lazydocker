//go:build !remote && (linux || freebsd)

package libpod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	volplugin "github.com/containers/podman/v5/libpod/plugin"
	pluginapi "github.com/docker/go-plugins-helpers/volume"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage"
	"go.podman.io/storage/drivers/quota"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/stringid"
)

const volumeSuffix = "+volume"

// NewVolume creates a new empty volume
func (r *Runtime) NewVolume(ctx context.Context, options ...VolumeCreateOption) (*Volume, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}
	return r.newVolume(ctx, false, options...)
}

// newVolume creates a new empty volume with the given options.
// The createPluginVolume can be set to true to make it not create the volume in the volume plugin,
// this is required for the UpdateVolumePlugins() function. If you are not sure, set this to false.
func (r *Runtime) newVolume(ctx context.Context, noCreatePluginVolume bool, options ...VolumeCreateOption) (_ *Volume, deferredErr error) {
	volume := newVolume(r)
	for _, option := range options {
		if err := option(volume); err != nil {
			return nil, fmt.Errorf("running volume create option: %w", err)
		}
	}

	if volume.config.Name == "" {
		volume.config.Name = stringid.GenerateRandomID()
	}
	if volume.config.Driver == "" {
		volume.config.Driver = define.VolumeDriverLocal
	}
	volume.config.CreatedTime = time.Now()

	// Check if volume with given name exists.
	exists, err := r.state.HasVolume(volume.config.Name)
	if err != nil {
		return nil, fmt.Errorf("checking if volume with name %s exists: %w", volume.config.Name, err)
	}
	if exists {
		if volume.ignoreIfExists {
			existingVolume, err := r.state.Volume(volume.config.Name)
			if err != nil {
				return nil, fmt.Errorf("reading volume from state: %w", err)
			}
			return existingVolume, nil
		} else {
			return nil, fmt.Errorf("volume with name %s already exists: %w", volume.config.Name, define.ErrVolumeExists)
		}
	}

	// Plugin can be nil if driver is local, but that's OK - superfluous
	// assignment doesn't hurt much.
	plugin, err := r.getVolumePlugin(volume.config)
	if err != nil {
		return nil, fmt.Errorf("volume %s uses volume plugin %s but it could not be retrieved: %w", volume.config.Name, volume.config.Driver, err)
	}
	volume.plugin = plugin

	if volume.config.Driver == define.VolumeDriverLocal {
		logrus.Debugf("Validating options for local driver")
		// Validate options
		for key, val := range volume.config.Options {
			switch strings.ToLower(key) {
			case "device":
				if strings.ToLower(volume.config.Options["type"]) == define.TypeBind {
					if err := fileutils.Exists(val); err != nil {
						return nil, fmt.Errorf("invalid volume option %s for driver 'local': %w", key, err)
					}
				}
			case "o", "type", "uid", "gid", "size", "inodes", "noquota", "copy", "nocopy":
				// Do nothing, valid keys
			default:
				return nil, fmt.Errorf("invalid mount option %s for driver 'local': %w", key, define.ErrInvalidArg)
			}
		}
	} else if volume.config.Driver == define.VolumeDriverImage && !volume.UsesVolumeDriver() {
		logrus.Debugf("Creating image-based volume")
		var imgString string
		// Validate options
		for key, val := range volume.config.Options {
			switch strings.ToLower(key) {
			case "image":
				imgString = val
			default:
				return nil, fmt.Errorf("invalid mount option %s for driver 'image': %w", key, define.ErrInvalidArg)
			}
		}

		if imgString == "" {
			return nil, fmt.Errorf("must provide an image name when creating a volume with the image driver: %w", define.ErrInvalidArg)
		}

		// Look up the image
		image, _, err := r.libimageRuntime.LookupImage(imgString, nil)
		if err != nil {
			return nil, fmt.Errorf("looking up image %s to create volume failed: %w", imgString, err)
		}

		// Generate a c/storage name and ID for the volume.
		// Use characters Podman does not allow for the name, to ensure
		// no collision with containers.
		volume.config.StorageID = stringid.GenerateRandomID()
		volume.config.StorageName = volume.config.Name + volumeSuffix
		volume.config.StorageImageID = image.ID()

		// Create a backing container in c/storage.
		storageConfig := storage.ContainerOptions{
			LabelOpts: []string{"filetype:container_file_t", "level:s0"},
		}
		if len(volume.config.MountLabel) > 0 {
			context, err := selinux.NewContext(volume.config.MountLabel)
			if err != nil {
				return nil, fmt.Errorf("failed to get SELinux context from %s: %w", volume.config.MountLabel, err)
			}
			storageConfig.LabelOpts = []string{fmt.Sprintf("filetype:%s", context["type"])}
		}
		if _, err := r.storageService.CreateContainerStorage(ctx, r.imageContext, imgString, image.ID(), volume.config.StorageName, volume.config.StorageID, storageConfig); err != nil {
			return nil, fmt.Errorf("creating backing storage for image driver: %w", err)
		}
		defer func() {
			if deferredErr != nil {
				if err := r.storageService.DeleteContainer(volume.config.StorageID); err != nil {
					logrus.Errorf("Error removing volume %s backing storage: %v", volume.config.Name, err)
				}
			}
		}()
	}

	// Now we get conditional: we either need to make the volume in the
	// volume plugin, or on disk if not using a plugin.
	if volume.plugin != nil && !noCreatePluginVolume {
		// We can't chown, or relabel, or similar the path the volume is
		// using, because it's not managed by us.
		// TODO: reevaluate this once we actually have volume plugins in
		// use in production - it may be safe, but I can't tell without
		// knowing what the actual plugin does...
		if err := makeVolumeInPluginIfNotExist(volume.config.Name, volume.config.Options, volume.plugin); err != nil {
			return nil, err
		}
	} else {
		// Create the mountpoint of this volume
		volPathRoot := filepath.Join(r.config.Engine.VolumePath, volume.config.Name)
		if err := os.MkdirAll(volPathRoot, 0o700); err != nil {
			return nil, fmt.Errorf("creating volume directory %q: %w", volPathRoot, err)
		}
		if err := idtools.SafeChown(volPathRoot, volume.config.UID, volume.config.GID); err != nil {
			return nil, fmt.Errorf("chowning volume directory %q to %d:%d: %w", volPathRoot, volume.config.UID, volume.config.GID, err)
		}

		// Setting quotas must happen *before* the _data inner directory
		// is created, as the volume must be empty for the quota to be
		// properly applied - if any subdirectories exist before the
		// quota is applied, the quota will not be applied to them.
		switch {
		case volume.config.DisableQuota:
			if volume.config.Size > 0 || volume.config.Inodes > 0 {
				return nil, errors.New("volume options size and inodes cannot be used without quota")
			}
		case volume.config.Options["type"] == define.TypeTmpfs:
			// tmpfs only supports Size
			if volume.config.Inodes > 0 {
				return nil, errors.New("volume option inodes not supported on tmpfs filesystem")
			}
		case volume.config.Inodes > 0 || volume.config.Size > 0:
			projectQuotaSupported := false
			q, err := quota.NewControl(r.config.Engine.VolumePath)
			if err == nil {
				projectQuotaSupported = true
			}
			if !projectQuotaSupported {
				return nil, errors.New("volume options size and inodes not supported. Filesystem does not support Project Quota")
			}
			quota := quota.Quota{
				Inodes: volume.config.Inodes,
				Size:   volume.config.Size,
			}
			// Must use volPathRoot not fullVolPath, as we need the
			// base path for the volume - without the `_data`
			// subdirectory - so the quota ID assignment logic works
			// properly.
			if err := q.SetQuota(volPathRoot, quota); err != nil {
				return nil, fmt.Errorf("failed to set size quota size=%d inodes=%d for volume directory %q: %w", volume.config.Size, volume.config.Inodes, volPathRoot, err)
			}
		}

		fullVolPath := filepath.Join(volPathRoot, "_data")
		if err := os.MkdirAll(fullVolPath, 0o755); err != nil {
			return nil, fmt.Errorf("creating volume directory %q: %w", fullVolPath, err)
		}
		if err := idtools.SafeChown(fullVolPath, volume.config.UID, volume.config.GID); err != nil {
			return nil, fmt.Errorf("chowning volume directory %q to %d:%d: %w", fullVolPath, volume.config.UID, volume.config.GID, err)
		}
		if err := LabelVolumePath(fullVolPath, volume.config.MountLabel); err != nil {
			return nil, err
		}
		volume.config.MountPoint = fullVolPath
	}

	lock, err := r.lockManager.AllocateLock()
	if err != nil {
		return nil, fmt.Errorf("allocating lock for new volume: %w", err)
	}
	volume.lock = lock
	volume.config.LockID = volume.lock.ID()

	defer func() {
		if deferredErr != nil {
			if err := volume.lock.Free(); err != nil {
				logrus.Errorf("Freeing volume lock after failed creation: %v", err)
			}
		}
	}()

	volume.valid = true

	// Add the volume to state
	if err := r.state.AddVolume(volume); err != nil {
		return nil, fmt.Errorf("adding volume to state: %w", err)
	}
	defer volume.newVolumeEvent(events.Create)
	return volume, nil
}

// UpdateVolumePlugins reads all volumes from all configured volume plugins and
// imports them into the libpod db. It also checks if existing libpod volumes
// are removed in the plugin, in this case we try to remove it from libpod.
// On errors we continue and try to do as much as possible. all errors are
// returned as array in the returned struct.
// This function has many race conditions, it is best effort but cannot guarantee
// a perfect state since plugins can be modified from the outside at any time.
func (r *Runtime) UpdateVolumePlugins(ctx context.Context) *define.VolumeReload {
	var (
		added            []string
		removed          []string
		errs             []error
		allPluginVolumes = map[string]struct{}{}
	)

	for driverName, socket := range r.config.Engine.VolumePlugins {
		driver, err := volplugin.GetVolumePlugin(driverName, socket, nil, r.config)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		vols, err := driver.ListVolumes()
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to read volumes from plugin %q: %w", driverName, err))
			continue
		}
		for _, vol := range vols {
			allPluginVolumes[vol.Name] = struct{}{}
			if _, err := r.newVolume(ctx, true, WithVolumeName(vol.Name), WithVolumeDriver(driverName)); err != nil {
				// If the volume exists this is not an error, just ignore it and log. It is very likely
				// that the volume from the plugin was already in our db.
				if !errors.Is(err, define.ErrVolumeExists) {
					errs = append(errs, err)
					continue
				}
				logrus.Infof("Volume %q already exists: %v", vol.Name, err)
				continue
			}
			added = append(added, vol.Name)
		}
	}

	libpodVolumes, err := r.state.AllVolumes()
	if err != nil {
		errs = append(errs, fmt.Errorf("cannot delete dangling plugin volumes: failed to read libpod volumes: %w", err))
	}
	for _, vol := range libpodVolumes {
		if vol.UsesVolumeDriver() {
			if _, ok := allPluginVolumes[vol.Name()]; !ok {
				// The volume is no longer in the plugin. Let's remove it from the libpod db.
				if err := r.removeVolume(ctx, vol, false, nil, true); err != nil {
					if errors.Is(err, define.ErrVolumeBeingUsed) {
						// Volume is still used by at least one container. This is very bad,
						// the plugin no longer has this but we still need it.
						errs = append(errs, fmt.Errorf("volume was removed from the plugin %q but containers still require it: %w", vol.config.Driver, err))
						continue
					}
					if errors.Is(err, define.ErrNoSuchVolume) || errors.Is(err, define.ErrVolumeRemoved) || errors.Is(err, define.ErrMissingPlugin) {
						// Volume was already removed, no problem just ignore it and continue.
						continue
					}

					// some other error
					errs = append(errs, err)
					continue
				}
				// Volume was successfully removed
				removed = append(removed, vol.Name())
			}
		}
	}

	return &define.VolumeReload{
		Added:   added,
		Removed: removed,
		Errors:  errs,
	}
}

// makeVolumeInPluginIfNotExist makes a volume in the given volume plugin if it
// does not already exist.
func makeVolumeInPluginIfNotExist(name string, options map[string]string, plugin *volplugin.VolumePlugin) error {
	// Ping the volume plugin to see if it exists first.
	// If it does, use the existing volume in the plugin.
	// Options may not match exactly, but not much we can do about
	// that. Not complaining avoids a lot of the sync issues we see
	// with c/storage and libpod DB.
	needsCreate := true
	getReq := new(pluginapi.GetRequest)
	getReq.Name = name
	if resp, err := plugin.GetVolume(getReq); err == nil {
		// TODO: What do we do if we get a 200 response, but the
		// Volume is nil? The docs on the Plugin API are very
		// nonspecific, so I don't know if this is valid or
		// not...
		if resp != nil {
			needsCreate = false
			logrus.Infof("Volume %q already exists in plugin %q, using existing volume", name, plugin.Name)
		}
	}
	if needsCreate {
		createReq := new(pluginapi.CreateRequest)
		createReq.Name = name
		createReq.Options = options
		if err := plugin.CreateVolume(createReq); err != nil {
			return fmt.Errorf("creating volume %q in plugin %s: %w", name, plugin.Name, err)
		}
	}

	return nil
}

// removeVolume removes the specified volume from state as well tears down its mountpoint and storage.
// ignoreVolumePlugin is used to only remove the volume from the db and not the plugin,
// this is required when the volume was already removed from the plugin, i.e. in UpdateVolumePlugins().
func (r *Runtime) removeVolume(ctx context.Context, v *Volume, force bool, timeout *uint, ignoreVolumePlugin bool) error {
	if !v.valid {
		if ok, _ := r.state.HasVolume(v.Name()); !ok {
			return nil
		}
		return define.ErrVolumeRemoved
	}

	// DANGEROUS: Do not lock here yet because we might needed to remove containers first.
	// In general we must always acquire the ctr lock before a volume lock so we cannot lock.
	// THIS MUST BE DONE to prevent ABBA deadlocks.
	// It also means the are several races around creating containers with volumes and removing
	// them in parallel. However that problem exists regadless of taking the lock here or not.

	deps, err := r.state.VolumeInUse(v)
	if err != nil {
		return err
	}
	if len(deps) != 0 {
		depsStr := strings.Join(deps, ", ")
		if !force {
			return fmt.Errorf("volume %s is being used by the following container(s): %s: %w", v.Name(), depsStr, define.ErrVolumeBeingUsed)
		}

		// We need to remove all containers using the volume
		for _, dep := range deps {
			ctr, err := r.state.Container(dep)
			if err != nil {
				// If the container's removed, no point in
				// erroring.
				if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
					continue
				}

				return fmt.Errorf("removing container %s that depends on volume %s: %w", dep, v.Name(), err)
			}

			logrus.Debugf("Removing container %s (depends on volume %q)", ctr.ID(), v.Name())

			opts := ctrRmOpts{
				Force:   force,
				Timeout: timeout,
			}
			if _, _, err := r.removeContainer(ctx, ctr, opts); err != nil {
				return fmt.Errorf("removing container %s that depends on volume %s: %w", ctr.ID(), v.Name(), err)
			}
		}
	}

	// Ok now we are good to lock.
	v.lock.Lock()
	defer v.lock.Unlock()

	// Update volume status to pick up a potential removal from state
	if err := v.update(); err != nil {
		return err
	}

	// If the volume is still mounted - force unmount it
	if err := v.unmount(true); err != nil {
		if force {
			// If force is set, evict the volume, even if errors
			// occur. Otherwise we'll never be able to get rid of
			// them.
			logrus.Errorf("Unmounting volume %s: %v", v.Name(), err)
		} else {
			return fmt.Errorf("unmounting volume %s: %w", v.Name(), err)
		}
	}

	// Set volume as invalid so it can no longer be used
	v.valid = false

	var removalErr error

	// If we use a volume plugin, we need to remove from the plugin.
	if v.UsesVolumeDriver() && !ignoreVolumePlugin {
		canRemove := true

		// Do we have a volume driver?
		if v.plugin == nil {
			canRemove = false
			removalErr = fmt.Errorf("cannot remove volume %s from plugin %s, but it has been removed from Podman: %w", v.Name(), v.Driver(), define.ErrMissingPlugin)
		} else {
			// Ping the plugin first to verify the volume still
			// exists.
			// We're trying to be very tolerant of missing volumes
			// in the backend, to avoid the problems we see with
			// sync between c/storage and the Libpod DB.
			getReq := new(pluginapi.GetRequest)
			getReq.Name = v.Name()
			if _, err := v.plugin.GetVolume(getReq); err != nil {
				canRemove = false
				removalErr = fmt.Errorf("volume %s could not be retrieved from plugin %s, but it has been removed from Podman: %w", v.Name(), v.Driver(), err)
			}
		}
		if canRemove {
			req := new(pluginapi.RemoveRequest)
			req.Name = v.Name()
			if err := v.plugin.RemoveVolume(req); err != nil {
				return fmt.Errorf("volume %s could not be removed from plugin %s: %w", v.Name(), v.Driver(), err)
			}
		}
	} else if v.config.Driver == define.VolumeDriverImage {
		if err := v.runtime.storageService.DeleteContainer(v.config.StorageID); err != nil {
			// Storage container is already gone, no problem.
			if !errors.Is(err, storage.ErrNotAContainer) && !errors.Is(err, storage.ErrContainerUnknown) {
				return fmt.Errorf("removing volume %s storage: %w", v.Name(), err)
			}
			logrus.Infof("Storage for volume %s already removed", v.Name())
		}
	}

	// Remove the volume from the state
	if err := r.state.RemoveVolume(v); err != nil {
		if removalErr != nil {
			logrus.Errorf("Removing volume %s from plugin %s: %v", v.Name(), v.Driver(), removalErr)
		}
		return fmt.Errorf("removing volume %s: %w", v.Name(), err)
	}

	// Free the volume's lock
	if err := v.lock.Free(); err != nil {
		if removalErr == nil {
			removalErr = fmt.Errorf("freeing lock for volume %s: %w", v.Name(), err)
		} else {
			logrus.Errorf("Freeing lock for volume %q: %v", v.Name(), err)
		}
	}

	// Delete the mountpoint path of the volume, that is delete the volume
	// from /var/lib/containers/storage/volumes
	if err := v.teardownStorage(); err != nil {
		if removalErr == nil {
			removalErr = fmt.Errorf("cleaning up volume storage for %q: %w", v.Name(), err)
		} else {
			logrus.Errorf("Cleaning up volume storage for volume %q: %v", v.Name(), err)
		}
	}

	defer v.newVolumeEvent(events.Remove)
	logrus.Debugf("Removed volume %s", v.Name())
	return removalErr
}
