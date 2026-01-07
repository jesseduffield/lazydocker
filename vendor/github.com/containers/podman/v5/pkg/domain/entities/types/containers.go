package types

import (
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type ContainerCopyFunc func() error

type ContainerStatReport struct {
	define.FileInfo
}

type CheckpointReport struct {
	Err             error                                   `json:"-"`
	Id              string                                  `json:"Id"`
	RawInput        string                                  `json:"-"`
	RuntimeDuration int64                                   `json:"runtime_checkpoint_duration"`
	CRIUStatistics  *define.CRIUCheckpointRestoreStatistics `json:"criu_statistics"`
}

type RestoreReport struct {
	Err             error                                   `json:"-"`
	Id              string                                  `json:"Id"`
	RawInput        string                                  `json:"-"`
	RuntimeDuration int64                                   `json:"runtime_restore_duration"`
	CRIUStatistics  *define.CRIUCheckpointRestoreStatistics `json:"criu_statistics"`
}

// ContainerStatsReport is used for streaming container stats.
type ContainerStatsReport struct {
	// Error from reading stats.
	Error error
	// Results, set when there is no error.
	Stats []define.ContainerStats
}

type ContainerUpdateOptions struct {
	NameOrID string
	// This individual items of Specgen are used to update container configuration:
	// - ResourceLimits
	// - RestartPolicy
	// - RestartRetries
	//
	// Deprecated: Specgen should not be used to change the container configuration.
	// Specgen is processed first, so values will be overwritten by values from other fields.
	//
	// To change configuration use other fields in ContainerUpdateOptions struct:
	// - Resources to change resource configuration
	// - DevicesLimits to Limit device
	// - RestartPolicy to change restart policy
	// - RestartRetries to change restart retries
	// - Env to change the environment variables.
	// - UntsetEnv to unset the environment variables.
	Specgen                         *specgen.SpecGenerator
	Resources                       *specs.LinuxResources
	DevicesLimits                   *define.UpdateContainerDevicesLimits
	ChangedHealthCheckConfiguration *define.UpdateHealthCheckConfig
	RestartPolicy                   *string
	RestartRetries                  *uint
	Env                             []string
	UnsetEnv                        []string
	Latest                          bool
}

func (u *ContainerUpdateOptions) ProcessSpecgen() {
	if u.Specgen == nil {
		return
	}

	if u.Resources == nil {
		u.Resources = u.Specgen.ResourceLimits
	}

	if u.DevicesLimits == nil {
		u.DevicesLimits = new(define.UpdateContainerDevicesLimits)
		u.DevicesLimits.SetBlkIOWeightDevice(u.Specgen.WeightDevice)
		u.DevicesLimits.SetDeviceReadBPs(u.Specgen.ThrottleReadBpsDevice)
		u.DevicesLimits.SetDeviceWriteBPs(u.Specgen.ThrottleWriteBpsDevice)
		u.DevicesLimits.SetDeviceReadIOPs(u.Specgen.ThrottleReadIOPSDevice)
		u.DevicesLimits.SetDeviceWriteIOPs(u.Specgen.ThrottleWriteIOPSDevice)
	}

	if u.RestartPolicy == nil {
		u.RestartPolicy = &u.Specgen.RestartPolicy
	}
	if u.RestartRetries == nil {
		u.RestartRetries = u.Specgen.RestartRetries
	}
}
