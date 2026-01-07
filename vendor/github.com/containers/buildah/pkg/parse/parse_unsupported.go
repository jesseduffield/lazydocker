//go:build !linux && !darwin

package parse

import (
	"errors"

	"github.com/containers/buildah/define"
)

func getDefaultProcessLimits() []string {
	return []string{}
}

func DeviceFromPath(device string) (define.ContainerDevices, error) {
	return nil, errors.New("devices not supported")
}
