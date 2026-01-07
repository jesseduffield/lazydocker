package define

import (
	"fmt"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// Valid restart policy types.
const (
	// RestartPolicyNone indicates that no restart policy has been requested
	// by a container.
	RestartPolicyNone = ""
	// RestartPolicyNo is identical in function to RestartPolicyNone.
	RestartPolicyNo = "no"
	// RestartPolicyAlways unconditionally restarts the container.
	RestartPolicyAlways = "always"
	// RestartPolicyOnFailure restarts the container on non-0 exit code,
	// with an optional maximum number of retries.
	RestartPolicyOnFailure = "on-failure"
	// RestartPolicyUnlessStopped unconditionally restarts unless stopped
	// by the user. It is identical to Always except with respect to
	// handling of system restart, which Podman does not yet support.
	RestartPolicyUnlessStopped = "unless-stopped"
)

// RestartPolicyMap maps between restart-policy valid values to restart policy types
var RestartPolicyMap = map[string]string{
	"none":                     RestartPolicyNone,
	RestartPolicyNo:            RestartPolicyNo,
	RestartPolicyAlways:        RestartPolicyAlways,
	RestartPolicyOnFailure:     RestartPolicyOnFailure,
	RestartPolicyUnlessStopped: RestartPolicyUnlessStopped,
}

// Validate that the given string is a valid restart policy.
func ValidateRestartPolicy(policy string) error {
	switch policy {
	case RestartPolicyNone, RestartPolicyNo, RestartPolicyOnFailure, RestartPolicyAlways, RestartPolicyUnlessStopped:
		return nil
	default:
		return fmt.Errorf("%q is not a valid restart policy: %w", policy, ErrInvalidArg)
	}
}

// InitContainerTypes
const (
	// AlwaysInitContainer is an init container that runs on each
	// pod start (including restart)
	AlwaysInitContainer = "always"
	// OneShotInitContainer is a container that only runs as init once
	// and is then deleted.
	OneShotInitContainer = "once"
	// ContainerInitPath is the default path of the mounted container init.
	ContainerInitPath = "/run/podman-init"
)

// Kubernetes Kinds
const (
	// A Pod kube yaml spec
	K8sKindPod = "pod"
	// A Deployment kube yaml spec
	K8sKindDeployment = "deployment"
	// A DaemonSet kube yaml spec
	K8sKindDaemonSet = "daemonset"
	// a Job kube yaml spec
	K8sKindJob = "job"
)

type WeightDevice struct {
	Path   string
	Weight uint16
}

type ThrottleDevice struct {
	Path string
	Rate uint64
}

type UpdateContainerDevicesLimits struct {
	// Block IO weight (relative device weight) in the form:
	// ```[{"Path": "device_path", "Weight": weight}]```
	BlkIOWeightDevice []WeightDevice `json:",omitempty"`
	// Limit read rate (bytes per second) from a device, in the form:
	// ```[{"Path": "device_path", "Rate": rate}]```
	DeviceReadBPs []ThrottleDevice `json:",omitempty"`
	// Limit write rate (bytes per second) to a device, in the form:
	// ```[{"Path": "device_path", "Rate": rate}]```
	DeviceWriteBPs []ThrottleDevice `json:",omitempty"`
	// Limit read rate (IO per second) from a device, in the form:
	// ```[{"Path": "device_path", "Rate": rate}]```
	DeviceReadIOPs []ThrottleDevice `json:",omitempty"`
	// Limit write rate (IO per second) to a device, in the form:
	// ```[{"Path": "device_path", "Rate": rate}]```
	DeviceWriteIOPs []ThrottleDevice `json:",omitempty"`
}

func (d *WeightDevice) addToLinuxWeightDevice(wd map[string]specs.LinuxWeightDevice) {
	wd[d.Path] = specs.LinuxWeightDevice{
		Weight:     &d.Weight,
		LeafWeight: nil,
	}
}

func LinuxWeightDeviceToWeightDevice(path string, d specs.LinuxWeightDevice) WeightDevice {
	return WeightDevice{Path: path, Weight: *d.Weight}
}

func (d *ThrottleDevice) addToLinuxThrottleDevice(td map[string]specs.LinuxThrottleDevice) {
	td[d.Path] = specs.LinuxThrottleDevice{Rate: d.Rate}
}

func LinuxThrottleDevicesToThrottleDevices(path string, d specs.LinuxThrottleDevice) ThrottleDevice {
	return ThrottleDevice{Path: path, Rate: d.Rate}
}

func (u *UpdateContainerDevicesLimits) SetBlkIOWeightDevice(wd map[string]specs.LinuxWeightDevice) {
	for path, dev := range wd {
		u.BlkIOWeightDevice = append(u.BlkIOWeightDevice, LinuxWeightDeviceToWeightDevice(path, dev))
	}
}

func copyLinuxThrottleDevicesFromMapToThrottleDevicesArray(source map[string]specs.LinuxThrottleDevice, dest []ThrottleDevice) []ThrottleDevice {
	for path, dev := range source {
		dest = append(dest, LinuxThrottleDevicesToThrottleDevices(path, dev))
	}
	return dest
}

func (u *UpdateContainerDevicesLimits) SetDeviceReadBPs(td map[string]specs.LinuxThrottleDevice) {
	u.DeviceReadBPs = copyLinuxThrottleDevicesFromMapToThrottleDevicesArray(td, u.DeviceReadBPs)
}

func (u *UpdateContainerDevicesLimits) SetDeviceWriteBPs(td map[string]specs.LinuxThrottleDevice) {
	u.DeviceWriteBPs = copyLinuxThrottleDevicesFromMapToThrottleDevicesArray(td, u.DeviceWriteBPs)
}

func (u *UpdateContainerDevicesLimits) SetDeviceReadIOPs(td map[string]specs.LinuxThrottleDevice) {
	u.DeviceReadIOPs = copyLinuxThrottleDevicesFromMapToThrottleDevicesArray(td, u.DeviceReadIOPs)
}

func (u *UpdateContainerDevicesLimits) SetDeviceWriteIOPs(td map[string]specs.LinuxThrottleDevice) {
	u.DeviceWriteIOPs = copyLinuxThrottleDevicesFromMapToThrottleDevicesArray(td, u.DeviceWriteIOPs)
}

func (u *UpdateContainerDevicesLimits) GetMapOfLinuxWeightDevice() map[string]specs.LinuxWeightDevice {
	wd := make(map[string]specs.LinuxWeightDevice)
	for _, dev := range u.BlkIOWeightDevice {
		dev.addToLinuxWeightDevice(wd)
	}
	return wd
}

func getMapOfLinuxThrottleDevices(source []ThrottleDevice) map[string]specs.LinuxThrottleDevice {
	td := make(map[string]specs.LinuxThrottleDevice)
	for _, dev := range source {
		dev.addToLinuxThrottleDevice(td)
	}
	return td
}

func (u *UpdateContainerDevicesLimits) GetMapOfDeviceReadBPs() map[string]specs.LinuxThrottleDevice {
	return getMapOfLinuxThrottleDevices(u.DeviceReadBPs)
}

func (u *UpdateContainerDevicesLimits) GetMapOfDeviceWriteBPs() map[string]specs.LinuxThrottleDevice {
	return getMapOfLinuxThrottleDevices(u.DeviceWriteBPs)
}

func (u *UpdateContainerDevicesLimits) GetMapOfDeviceReadIOPs() map[string]specs.LinuxThrottleDevice {
	return getMapOfLinuxThrottleDevices(u.DeviceReadIOPs)
}

func (u *UpdateContainerDevicesLimits) GetMapOfDeviceWriteIOPs() map[string]specs.LinuxThrottleDevice {
	return getMapOfLinuxThrottleDevices(u.DeviceWriteIOPs)
}
