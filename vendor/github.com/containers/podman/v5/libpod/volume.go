//go:build !remote

package libpod

import (
	"fmt"
	"io"
	"maps"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/lock"
	"github.com/containers/podman/v5/libpod/plugin"
	"github.com/containers/podman/v5/utils"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/directory"
)

// Volume is a libpod named volume.
// Named volumes may be shared by multiple containers, and may be created using
// more complex options than normal bind mounts. They may be backed by a mounted
// filesystem on the host.
type Volume struct {
	config *VolumeConfig
	state  *VolumeState

	ignoreIfExists bool
	valid          bool
	plugin         *plugin.VolumePlugin
	runtime        *Runtime
	lock           lock.Locker
}

// VolumeConfig holds the volume's immutable configuration.
type VolumeConfig struct {
	// Name of the volume.
	Name string `json:"name"`
	// ID of the volume's lock.
	LockID uint32 `json:"lockID"`
	// Labels for the volume.
	Labels map[string]string `json:"labels"`
	// The volume driver. Empty string or local does not activate a volume
	// driver, all other values will.
	Driver string `json:"volumeDriver"`
	// The location the volume is mounted at.
	MountPoint string `json:"mountPoint"`
	// Time the volume was created.
	CreatedTime time.Time `json:"createdAt"`
	// Options to pass to the volume driver. For the local driver, this is
	// a list of mount options. For other drivers, they are passed to the
	// volume driver handling the volume.
	Options map[string]string `json:"volumeOptions,omitempty"`
	// Whether this volume is anonymous (will be removed on container exit)
	IsAnon bool `json:"isAnon"`
	// UID the volume will be created as.
	UID int `json:"uid"`
	// GID the volume will be created as.
	GID int `json:"gid"`
	// Size maximum of the volume.
	Size uint64 `json:"size"`
	// Inodes maximum of the volume.
	Inodes uint64 `json:"inodes"`
	// DisableQuota indicates that the volume should completely disable using any
	// quota tracking.
	DisableQuota bool `json:"disableQuota,omitempty"`
	// Timeout allows users to override the default driver timeout of 5 seconds
	Timeout *uint `json:"timeout,omitempty"`
	// StorageName is the name of the volume in c/storage. Only used for
	// image volumes.
	StorageName string `json:"storageName,omitempty"`
	// StorageID is the ID of the volume in c/storage. Only used for image
	// volumes.
	StorageID string `json:"storageID,omitempty"`
	// StorageImageID is the ID of the image the volume was based off of.
	// Only used for image volumes.
	StorageImageID string `json:"storageImageID,omitempty"`
	// MountLabel is the SELinux label to assign to mount points
	MountLabel string `json:"mountlabel,omitempty"`
}

// VolumeState holds the volume's mutable state.
// Volumes are not guaranteed to have a state. Only volumes using the Local
// driver that have mount options set will create a state.
type VolumeState struct {
	// Mountpoint is the location where the volume was mounted.
	// This is only used for volumes using a volume plugin, which will mount
	// at non-standard locations.
	MountPoint string `json:"mountPoint,omitempty"`
	// MountCount is the number of times this volume has been requested to
	// be mounted.
	// It is incremented on mount() and decremented on unmount().
	// On incrementing from 0, the volume will be mounted on the host.
	// On decrementing to 0, the volume will be unmounted on the host.
	MountCount uint `json:"mountCount"`
	// NeedsCopyUp indicates that the next time the volume is mounted into
	// a container, the container will "copy up" the contents of the
	// mountpoint into the volume.
	// This should only be done once. As such, this is set at container
	// create time, then cleared after the copy up is done and never set
	// again.
	NeedsCopyUp bool `json:"notYetMounted,omitempty"`
	// NeedsChown indicates that the next time the volume is mounted into
	// a container, the container will chown the volume to the container process
	// UID/GID.
	NeedsChown bool `json:"notYetChowned,omitempty"`
	// Indicates that a copy-up event occurred during the current mount of
	// the volume into a container.
	// We use this to determine if a chown is appropriate.
	CopiedUp bool `json:"copiedUp,omitempty"`
	// UIDChowned is the UID the volume was chowned to.
	UIDChowned int `json:"uidChowned,omitempty"`
	// GIDChowned is the GID the volume was chowned to.
	GIDChowned int `json:"gidChowned,omitempty"`
}

// Name retrieves the volume's name
func (v *Volume) Name() string {
	return v.config.Name
}

// Returns the size on disk of volume
func (v *Volume) Size() (uint64, error) {
	size, err := directory.Size(v.config.MountPoint)
	return uint64(size), err
}

// Driver retrieves the volume's driver.
func (v *Volume) Driver() string {
	return v.config.Driver
}

// Scope retrieves the volume's scope.
// Libpod does not implement volume scoping, and this is provided solely for
// Docker compatibility. It returns only "local".
func (v *Volume) Scope() string {
	return "local"
}

// Labels returns the volume's labels
func (v *Volume) Labels() map[string]string {
	labels := make(map[string]string)
	maps.Copy(labels, v.config.Labels)
	return labels
}

// MountPoint returns the volume's mountpoint on the host
func (v *Volume) MountPoint() (string, error) {
	// For the sake of performance, avoid locking unless we have to.
	if v.UsesVolumeDriver() || v.config.Driver == define.VolumeDriverImage {
		v.lock.Lock()
		defer v.lock.Unlock()

		if err := v.update(); err != nil {
			return "", err
		}
	}

	return v.mountPoint(), nil
}

// MountCount returns the volume's mountcount on the host from state
// Useful in determining if volume is using plugin or a filesystem mount and its mount
func (v *Volume) MountCount() (uint, error) {
	v.lock.Lock()
	defer v.lock.Unlock()
	if err := v.update(); err != nil {
		return 0, err
	}
	return v.state.MountCount, nil
}

// Internal-only helper for volume mountpoint
func (v *Volume) mountPoint() string {
	if v.UsesVolumeDriver() || v.config.Driver == define.VolumeDriverImage {
		return v.state.MountPoint
	}

	return v.config.MountPoint
}

// Options return the volume's options
func (v *Volume) Options() map[string]string {
	options := make(map[string]string)
	maps.Copy(options, v.config.Options)
	return options
}

// Anonymous returns whether this volume is anonymous. Anonymous volumes were
// created with a container, and will be removed when that container is removed.
func (v *Volume) Anonymous() bool {
	return v.config.IsAnon
}

// UID returns the UID the volume will be created as.
func (v *Volume) UID() (int, error) {
	v.lock.Lock()
	defer v.lock.Unlock()

	if err := v.update(); err != nil {
		return -1, err
	}

	return v.uid(), nil
}

// Internal, unlocked accessor for UID.
func (v *Volume) uid() int {
	if v.state.UIDChowned > 0 {
		return v.state.UIDChowned
	}
	return v.config.UID
}

// GID returns the GID the volume will be created as.
func (v *Volume) GID() (int, error) {
	v.lock.Lock()
	defer v.lock.Unlock()

	if err := v.update(); err != nil {
		return -1, err
	}

	return v.gid(), nil
}

// Internal, unlocked accessor for GID.
func (v *Volume) gid() int {
	if v.state.GIDChowned > 0 {
		return v.state.GIDChowned
	}
	return v.config.GID
}

// CreatedTime returns the time the volume was created at. It was not tracked
// for some time, so older volumes may not contain one.
func (v *Volume) CreatedTime() time.Time {
	return v.config.CreatedTime
}

// Config returns the volume's configuration.
func (v *Volume) Config() (*VolumeConfig, error) {
	config := VolumeConfig{}
	err := JSONDeepCopy(v.config, &config)
	return &config, err
}

// VolumeInUse goes through the container dependencies of a volume
// and checks if the volume is being used by any container.
func (v *Volume) VolumeInUse() ([]string, error) {
	v.lock.Lock()
	defer v.lock.Unlock()

	if !v.valid {
		return nil, define.ErrVolumeRemoved
	}
	return v.runtime.state.VolumeInUse(v)
}

// IsDangling returns whether this volume is dangling (unused by any
// containers).
func (v *Volume) IsDangling() (bool, error) {
	ctrs, err := v.VolumeInUse()
	if err != nil {
		return false, err
	}
	return len(ctrs) == 0, nil
}

// UsesVolumeDriver determines whether the volume uses a volume driver. Volume
// drivers are pluggable backends for volumes that will manage the storage and
// mounting.
func (v *Volume) UsesVolumeDriver() bool {
	if v.config.Driver == define.VolumeDriverImage {
		if _, ok := v.runtime.config.Engine.VolumePlugins[v.config.Driver]; ok {
			return true
		}
		return false
	}
	return v.config.Driver != define.VolumeDriverLocal && v.config.Driver != ""
}

func (v *Volume) Mount() (string, error) {
	v.lock.Lock()
	defer v.lock.Unlock()
	err := v.mount()
	return v.config.MountPoint, err
}

func (v *Volume) Unmount() error {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.unmount(false)
}

func (v *Volume) NeedsMount() bool {
	return v.needsMount()
}

// Export volume to tar.
// Returns a ReadCloser which points to a tar of all the volume's contents.
func (v *Volume) Export() (io.ReadCloser, error) {
	v.lock.Lock()
	err := v.mount()
	mountPoint := v.mountPoint()
	v.lock.Unlock()
	if err != nil {
		return nil, err
	}
	defer func() {
		v.lock.Lock()
		defer v.lock.Unlock()

		if err := v.unmount(false); err != nil {
			logrus.Errorf("Error unmounting volume %s: %v", v.Name(), err)
		}
	}()

	volContents, err := utils.TarWithChroot(mountPoint)
	if err != nil {
		return nil, fmt.Errorf("creating tar of volume %s contents: %w", v.Name(), err)
	}

	return volContents, nil
}

// Import a volume from a tar file, provided as an io.Reader.
func (v *Volume) Import(r io.Reader) error {
	v.lock.Lock()
	err := v.mount()
	mountPoint := v.mountPoint()
	v.lock.Unlock()
	if err != nil {
		return err
	}
	defer func() {
		v.lock.Lock()
		defer v.lock.Unlock()

		if err := v.unmount(false); err != nil {
			logrus.Errorf("Error unmounting volume %s: %v", v.Name(), err)
		}
	}()

	if err := archive.Untar(r, mountPoint, nil); err != nil {
		return fmt.Errorf("extracting into volume %s: %w", v.Name(), err)
	}

	return nil
}
