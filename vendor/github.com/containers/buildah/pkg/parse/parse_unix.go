//go:build linux || darwin

package parse

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/buildah/define"
	"github.com/opencontainers/cgroups/devices/config"
	"github.com/opencontainers/runc/libcontainer/devices"
)

func DeviceFromPath(device string) (define.ContainerDevices, error) {
	var devs define.ContainerDevices
	src, dst, permissions, err := Device(device)
	if err != nil {
		return nil, err
	}
	if linkTarget, err := os.Readlink(src); err == nil {
		if filepath.IsAbs(linkTarget) {
			src = linkTarget
		} else {
			src = filepath.Join(filepath.Dir(src), linkTarget)
		}
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("getting info of source device %s: %w", src, err)
	}

	if !srcInfo.IsDir() {
		dev, err := devices.DeviceFromPath(src, permissions)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid device: %w", src, err)
		}
		dev.Path = dst
		device := define.BuildahDevice{Device: *dev, Source: src, Destination: dst}
		devs = append(devs, device)
		return devs, nil
	}

	// If source device is a directory
	srcDevices, err := devices.GetDevices(src)
	if err != nil {
		return nil, fmt.Errorf("getting source devices from directory %s: %w", src, err)
	}
	for _, d := range srcDevices {
		d.Path = filepath.Join(dst, filepath.Base(d.Path))
		d.Permissions = config.Permissions(permissions)
		device := define.BuildahDevice{Device: *d, Source: src, Destination: dst}
		devs = append(devs, device)
	}
	return devs, nil
}
