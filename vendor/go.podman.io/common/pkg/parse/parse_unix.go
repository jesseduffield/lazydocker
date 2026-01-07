//go:build linux || darwin || freebsd

package parse

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencontainers/cgroups/devices/config"
	"github.com/opencontainers/runc/libcontainer/devices"
	"go.podman.io/storage/pkg/unshare"
)

func DeviceFromPath(device string) ([]config.Device, error) {
	src, dst, permissions, err := Device(device)
	if err != nil {
		return nil, err
	}
	if unshare.IsRootless() && src != dst {
		return nil, fmt.Errorf("renaming device %s to %s is not supported in rootless containers", src, dst)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil, err
	}

	if !srcInfo.IsDir() {
		devs := make([]config.Device, 0, 1)
		dev, err := devices.DeviceFromPath(src, permissions)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid device: %w", src, err)
		}
		dev.Path = dst
		devs = append(devs, *dev)
		return devs, nil
	}

	// If source device is a directory
	srcDevices, err := devices.GetDevices(src)
	if err != nil {
		return nil, fmt.Errorf("getting source devices from directory %s: %w", src, err)
	}
	devs := make([]config.Device, 0, len(srcDevices))
	for _, d := range srcDevices {
		d.Path = filepath.Join(dst, filepath.Base(d.Path))
		d.Permissions = config.Permissions(permissions)
		devs = append(devs, *d)
	}
	return devs, nil
}
