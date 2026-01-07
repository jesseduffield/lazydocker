//go:build !remote

package libpod

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/podman/v5/libpod/define"
)

// Creates a new volume
func newVolume(runtime *Runtime) *Volume {
	volume := new(Volume)
	volume.config = new(VolumeConfig)
	volume.state = new(VolumeState)
	volume.runtime = runtime
	volume.config.Labels = make(map[string]string)
	volume.config.Options = make(map[string]string)
	volume.state.NeedsCopyUp = true
	volume.state.NeedsChown = true
	return volume
}

// teardownStorage deletes the volume from volumePath
func (v *Volume) teardownStorage() error {
	if v.UsesVolumeDriver() {
		return nil
	}

	// TODO: Should this be converted to use v.config.MountPoint?
	return os.RemoveAll(filepath.Join(v.runtime.config.Engine.VolumePath, v.Name()))
}

// Volumes with options set, or a filesystem type, or a device to mount need to
// be mounted and unmounted.
func (v *Volume) needsMount() bool {
	// Non-local driver always needs mount
	if v.UsesVolumeDriver() {
		return true
	}

	// Image driver always needs mount
	if v.config.Driver == define.VolumeDriverImage {
		return true
	}

	// Commit 28138dafcc added the UID and GID options to this map
	// However we should only mount when options other than uid and gid are set.
	// see https://github.com/containers/podman/issues/10620
	index := 0
	if _, ok := v.config.Options["UID"]; ok {
		index++
	}
	if _, ok := v.config.Options["GID"]; ok {
		index++
	}
	if _, ok := v.config.Options["SIZE"]; ok {
		index++
	}
	if _, ok := v.config.Options["NOQUOTA"]; ok {
		index++
	}
	if _, ok := v.config.Options["nocopy"]; ok {
		index++
	}
	if _, ok := v.config.Options["copy"]; ok {
		index++
	}
	// when uid or gid is set there is also the "o" option
	// set so we have to ignore this one as well
	if index > 0 {
		index++
	}
	// Local driver with options other than uid,gid needs mount
	return len(v.config.Options) > index
}

// update() updates the volume state from the DB.
func (v *Volume) update() error {
	if err := v.runtime.state.UpdateVolume(v); err != nil {
		return err
	}
	if !v.valid {
		return define.ErrVolumeRemoved
	}
	return nil
}

// save() saves the volume state to the DB
func (v *Volume) save() error {
	return v.runtime.state.SaveVolume(v)
}

// Refresh volume state after a restart.
func (v *Volume) refresh() error {
	lock, err := v.runtime.lockManager.AllocateAndRetrieveLock(v.config.LockID)
	if err != nil {
		return fmt.Errorf("acquiring lock %d for volume %s: %w", v.config.LockID, v.Name(), err)
	}
	v.lock = lock

	return nil
}

// resetVolumeState resets state fields to default values.
// It is performed before a refresh and clears the state after a reboot.
// It does not save the results - assumes the database will do that for us.
func resetVolumeState(state *VolumeState) {
	state.MountCount = 0
	state.MountPoint = ""
	state.CopiedUp = false
}
