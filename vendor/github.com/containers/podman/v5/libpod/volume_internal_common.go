//go:build !remote && (linux || freebsd)

package libpod

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	pluginapi "github.com/docker/go-plugins-helpers/volume"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// This is a pseudo-container ID to use when requesting a mount or unmount from
// the volume plugins.
// This is the shas256 of the string "placeholder\n".
const pseudoCtrID = "2f73349cfc4630255319c6c8dfc1b46a8996ace9d14d8e07563b165915918ec2"

// mount mounts the volume if necessary.
// A mount is necessary if a volume has any options set.
// If a mount is necessary, v.state.MountCount will be incremented.
// If it was 0 when the increment occurred, the volume will be mounted on the
// host. Otherwise, we assume it is already mounted.
// Must be done while the volume is locked.
// Is a no-op on volumes that do not require a mount (as defined by
// volumeNeedsMount()).
func (v *Volume) mount() error {
	if !v.needsMount() {
		return nil
	}

	// Update the volume from the DB to get an accurate mount counter.
	if err := v.update(); err != nil {
		return err
	}

	// If the count is non-zero, the volume is already mounted.
	// Nothing to do.
	if v.state.MountCount > 0 {
		v.state.MountCount++
		logrus.Debugf("Volume %s mount count now at %d", v.Name(), v.state.MountCount)
		return v.save()
	}

	// Volume plugins implement their own mount counter, based on the ID of
	// the mounting container. But we already have one, and honestly I trust
	// ours more. So hardcode container ID to something reasonable, and use
	// the same one for everything.
	if v.UsesVolumeDriver() {
		if v.plugin == nil {
			return fmt.Errorf("volume plugin %s (needed by volume %s) missing: %w", v.Driver(), v.Name(), define.ErrMissingPlugin)
		}

		req := new(pluginapi.MountRequest)
		req.Name = v.Name()
		req.ID = pseudoCtrID
		mountPoint, err := v.plugin.MountVolume(req)
		if err != nil {
			return err
		}

		v.state.MountCount++
		v.state.MountPoint = mountPoint
		return v.save()
	} else if v.config.Driver == define.VolumeDriverImage {
		mountPoint, err := v.runtime.storageService.MountContainerImage(v.config.StorageID)
		if err != nil {
			return fmt.Errorf("mounting volume %s image failed: %w", v.Name(), err)
		}

		v.state.MountCount++
		v.state.MountPoint = mountPoint
		return v.save()
	}

	volDevice := v.config.Options["device"]
	volType := v.config.Options["type"]
	volOptions := v.config.Options["o"]

	// Some filesystems (tmpfs) don't have a device, but we still need to
	// give the kernel something.
	if volDevice == "" && volType != "" {
		volDevice = volType
	}

	// We need to use the actual mount command.
	// Convincing unix.Mount to use the same semantics as the mount command
	// itself seems prohibitively difficult.
	// TODO: might want to cache this path in the runtime?
	mountPath, err := exec.LookPath("mount")
	if err != nil {
		return fmt.Errorf("locating 'mount' binary: %w", err)
	}
	mountArgs := []string{}
	if volOptions != "" {
		mountArgs = append(mountArgs, "-o", volOptions)
	}
	switch volType {
	case "":
	case define.TypeBind:
		mountArgs = append(mountArgs, "-o", volType)
	default:
		mountArgs = append(mountArgs, "-t", volType)
	}

	mountArgs = append(mountArgs, volDevice, v.config.MountPoint)
	mountCmd := exec.Command(mountPath, mountArgs...)

	logrus.Debugf("Running mount command: %s %s", mountPath, strings.Join(mountArgs, " "))
	if output, err := mountCmd.CombinedOutput(); err != nil {
		logrus.Debugf("Mount %v failed with %v", mountCmd, err)
		return errors.New(string(output))
	}

	logrus.Debugf("Mounted volume %s", v.Name())

	// Increment the mount counter
	v.state.MountCount++
	logrus.Debugf("Volume %s mount count now at %d", v.Name(), v.state.MountCount)
	return v.save()
}

// unmount unmounts the volume if necessary.
// Unmounting a volume that is not mounted is a no-op.
// Unmounting a volume that does not require a mount is a no-op.
// The volume must be locked for this to occur.
// The mount counter will be decremented if non-zero. If the counter reaches 0,
// the volume will really be unmounted, as no further containers are using the
// volume.
// If force is set, the volume will be unmounted regardless of mount counter.
func (v *Volume) unmount(force bool) error {
	if !v.needsMount() {
		return nil
	}

	// Update the volume from the DB to get an accurate mount counter.
	if err := v.update(); err != nil {
		return err
	}

	if v.state.MountCount == 0 {
		logrus.Debugf("Volume %s already unmounted", v.Name())
		return nil
	}

	if !force {
		v.state.MountCount--
	} else {
		v.state.MountCount = 0
	}

	logrus.Debugf("Volume %s mount count now at %d", v.Name(), v.state.MountCount)

	if v.state.MountCount == 0 {
		if v.UsesVolumeDriver() {
			if v.plugin == nil {
				return fmt.Errorf("volume plugin %s (needed by volume %s) missing: %w", v.Driver(), v.Name(), define.ErrMissingPlugin)
			}

			req := new(pluginapi.UnmountRequest)
			req.Name = v.Name()
			req.ID = pseudoCtrID
			if err := v.plugin.UnmountVolume(req); err != nil {
				return err
			}

			v.state.MountPoint = ""
			return v.save()
		} else if v.config.Driver == define.VolumeDriverImage {
			if _, err := v.runtime.storageService.UnmountContainerImage(v.config.StorageID, force); err != nil {
				return fmt.Errorf("unmounting volume %s image: %w", v.Name(), err)
			}

			v.state.MountPoint = ""
			return v.save()
		}

		// Unmount the volume
		if err := detachUnmount(v.config.MountPoint); err != nil {
			if err == unix.EINVAL {
				// Ignore EINVAL - the mount no longer exists.
				return nil
			}
			return fmt.Errorf("unmounting volume %s: %w", v.Name(), err)
		}
		logrus.Debugf("Unmounted volume %s", v.Name())
	}

	return v.save()
}
