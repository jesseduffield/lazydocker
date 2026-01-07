package config

import (
	"fmt"
	"os"
)

type BtrfsOptionsConfig struct {
	// MinSpace is the minimal spaces allocated to the device
	MinSpace string `toml:"min_space,omitempty"`
	// Size
	Size string `toml:"size,omitempty"`
}

type OverlayOptionsConfig struct {
	// IgnoreChownErrors is a flag for whether chown errors should be
	// ignored when building an image.
	IgnoreChownErrors string `toml:"ignore_chown_errors,omitempty"`
	// MountOpt specifies extra mount options used when mounting
	MountOpt string `toml:"mountopt,omitempty"`
	// Alternative program to use for the mount of the file system
	MountProgram string `toml:"mount_program,omitempty"`
	// Size
	Size string `toml:"size,omitempty"`
	// Inodes is used to set a maximum inodes of the container image.
	Inodes string `toml:"inodes,omitempty"`
	// Do not create a bind mount on the storage home
	SkipMountHome string `toml:"skip_mount_home,omitempty"`
	// Specify whether composefs must be used to mount the data layers
	UseComposefs string `toml:"use_composefs,omitempty"`
	// ForceMask indicates the permissions mask (e.g. "0755") to use for new
	// files and directories
	ForceMask string `toml:"force_mask,omitempty"`
}

type VfsOptionsConfig struct {
	// IgnoreChownErrors is a flag for whether chown errors should be
	// ignored when building an image.
	IgnoreChownErrors string `toml:"ignore_chown_errors,omitempty"`
}

type ZfsOptionsConfig struct {
	// MountOpt specifies extra mount options used when mounting
	MountOpt string `toml:"mountopt,omitempty"`
	// Name is the File System name of the ZFS File system
	Name string `toml:"fsname,omitempty"`
	// Size
	Size string `toml:"size,omitempty"`
}

// OptionsConfig represents the "storage.options" TOML config table.
type OptionsConfig struct {
	// AdditionalImagesStores is the location of additional read/only
	// Image stores.  Usually used to access Networked File System
	// for shared image content
	AdditionalImageStores []string `toml:"additionalimagestores,omitempty"`

	// ImageStore is the location of image store which is separated from the
	// container store. Usually this is not recommended unless users wants
	// separate store for image and containers.
	ImageStore string `toml:"imagestore,omitempty"`

	// AdditionalLayerStores is the location of additional read/only
	// Layer stores.  Usually used to access Networked File System
	// for shared image content
	// This API is experimental and can be changed without bumping the
	// major version number.
	AdditionalLayerStores []string `toml:"additionallayerstores,omitempty"`

	// Size
	Size string `toml:"size,omitempty"`

	// IgnoreChownErrors is a flag for whether chown errors should be
	// ignored when building an image.
	IgnoreChownErrors string `toml:"ignore_chown_errors,omitempty"`

	// Specify whether composefs must be used to mount the data layers
	UseComposefs string `toml:"use_composefs,omitempty"`

	// ForceMask indicates the permissions mask (e.g. "0755") to use for new
	// files and directories.
	ForceMask os.FileMode `toml:"force_mask,omitempty"`

	// RootAutoUsernsUser is the name of one or more entries in /etc/subuid and
	// /etc/subgid which should be used to set up automatically a userns.
	RootAutoUsernsUser string `toml:"root-auto-userns-user,omitempty"`

	// AutoUsernsMinSize is the minimum size for a user namespace that is
	// created automatically.
	AutoUsernsMinSize uint32 `toml:"auto-userns-min-size,omitempty"`

	// AutoUsernsMaxSize is the maximum size for a user namespace that is
	// created automatically.
	AutoUsernsMaxSize uint32 `toml:"auto-userns-max-size,omitempty"`

	// Btrfs container options to be handed to btrfs drivers
	Btrfs struct{ BtrfsOptionsConfig } `toml:"btrfs,omitempty"`

	// Thinpool container options to be handed to thinpool drivers (NOP)
	Thinpool struct{} `toml:"thinpool,omitempty"`

	// Overlay container options to be handed to overlay drivers
	Overlay struct{ OverlayOptionsConfig } `toml:"overlay,omitempty"`

	// Vfs container options to be handed to VFS drivers
	Vfs struct{ VfsOptionsConfig } `toml:"vfs,omitempty"`

	// Zfs container options to be handed to ZFS drivers
	Zfs struct{ ZfsOptionsConfig } `toml:"zfs,omitempty"`

	// Do not create a bind mount on the storage home
	SkipMountHome string `toml:"skip_mount_home,omitempty"`

	// Alternative program to use for the mount of the file system
	MountProgram string `toml:"mount_program,omitempty"`

	// MountOpt specifies extra mount options used when mounting
	MountOpt string `toml:"mountopt,omitempty"`

	// PullOptions specifies options to be handed to pull managers
	// This API is experimental and can be changed without bumping the major version number.
	PullOptions map[string]string `toml:"pull_options,omitempty"`

	// DisableVolatile doesn't allow volatile mounts when it is set.
	DisableVolatile bool `toml:"disable-volatile,omitempty"`
}

// GetGraphDriverOptions returns the driver specific options
func GetGraphDriverOptions(driverName string, options OptionsConfig) []string {
	var doptions []string
	switch driverName {
	case "btrfs":
		if options.Btrfs.MinSpace != "" {
			return append(doptions, fmt.Sprintf("%s.min_space=%s", driverName, options.Btrfs.MinSpace))
		}
		if options.Btrfs.Size != "" {
			doptions = append(doptions, fmt.Sprintf("%s.size=%s", driverName, options.Btrfs.Size))
		} else if options.Size != "" {
			doptions = append(doptions, fmt.Sprintf("%s.size=%s", driverName, options.Size))
		}

	case "overlay", "overlay2":
		// Specify whether composefs must be used to mount the data layers
		if options.Overlay.IgnoreChownErrors != "" {
			doptions = append(doptions, fmt.Sprintf("%s.ignore_chown_errors=%s", driverName, options.Overlay.IgnoreChownErrors))
		} else if options.IgnoreChownErrors != "" {
			doptions = append(doptions, fmt.Sprintf("%s.ignore_chown_errors=%s", driverName, options.IgnoreChownErrors))
		}
		if options.Overlay.MountProgram != "" {
			doptions = append(doptions, fmt.Sprintf("%s.mount_program=%s", driverName, options.Overlay.MountProgram))
		} else if options.MountProgram != "" {
			doptions = append(doptions, fmt.Sprintf("%s.mount_program=%s", driverName, options.MountProgram))
		}
		if options.Overlay.MountOpt != "" {
			doptions = append(doptions, fmt.Sprintf("%s.mountopt=%s", driverName, options.Overlay.MountOpt))
		} else if options.MountOpt != "" {
			doptions = append(doptions, fmt.Sprintf("%s.mountopt=%s", driverName, options.MountOpt))
		}
		if options.Overlay.Size != "" {
			doptions = append(doptions, fmt.Sprintf("%s.size=%s", driverName, options.Overlay.Size))
		} else if options.Size != "" {
			doptions = append(doptions, fmt.Sprintf("%s.size=%s", driverName, options.Size))
		}
		if options.Overlay.Inodes != "" {
			doptions = append(doptions, fmt.Sprintf("%s.inodes=%s", driverName, options.Overlay.Inodes))
		}
		if options.Overlay.SkipMountHome != "" {
			doptions = append(doptions, fmt.Sprintf("%s.skip_mount_home=%s", driverName, options.Overlay.SkipMountHome))
		} else if options.SkipMountHome != "" {
			doptions = append(doptions, fmt.Sprintf("%s.skip_mount_home=%s", driverName, options.SkipMountHome))
		}
		if options.Overlay.ForceMask != "" {
			doptions = append(doptions, fmt.Sprintf("%s.force_mask=%s", driverName, options.Overlay.ForceMask))
		} else if options.ForceMask != 0 {
			doptions = append(doptions, fmt.Sprintf("%s.force_mask=%s", driverName, options.ForceMask))
		}
		if options.Overlay.UseComposefs != "" {
			doptions = append(doptions, fmt.Sprintf("%s.use_composefs=%s", driverName, options.Overlay.UseComposefs))
		}
	case "vfs":
		if options.Vfs.IgnoreChownErrors != "" {
			doptions = append(doptions, fmt.Sprintf("%s.ignore_chown_errors=%s", driverName, options.Vfs.IgnoreChownErrors))
		} else if options.IgnoreChownErrors != "" {
			doptions = append(doptions, fmt.Sprintf("%s.ignore_chown_errors=%s", driverName, options.IgnoreChownErrors))
		}

	case "zfs":
		if options.Zfs.Name != "" {
			doptions = append(doptions, fmt.Sprintf("%s.fsname=%s", driverName, options.Zfs.Name))
		}
		if options.Zfs.MountOpt != "" {
			doptions = append(doptions, fmt.Sprintf("%s.mountopt=%s", driverName, options.Zfs.MountOpt))
		} else if options.MountOpt != "" {
			doptions = append(doptions, fmt.Sprintf("%s.mountopt=%s", driverName, options.MountOpt))
		}
		if options.Zfs.Size != "" {
			doptions = append(doptions, fmt.Sprintf("%s.size=%s", driverName, options.Zfs.Size))
		} else if options.Size != "" {
			doptions = append(doptions, fmt.Sprintf("%s.size=%s", driverName, options.Size))
		}
	}
	return doptions
}
