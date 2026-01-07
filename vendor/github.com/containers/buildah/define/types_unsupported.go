//go:build !linux && !darwin

package define

// ContainerDevices is currently not implemented.
type ContainerDevices = []struct{}
