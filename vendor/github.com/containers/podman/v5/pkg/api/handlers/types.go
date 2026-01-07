package handlers

import (
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/domain/entities"
	docker "github.com/docker/docker/api/types"
	dockerBackend "github.com/docker/docker/api/types/backend"
	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"
	dockerNetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type AuthConfig struct {
	registry.AuthConfig
}

type ImageInspect struct {
	dockerImage.InspectResponse
	// When you embed a struct, the fields of the embedded struct are "promoted" to the outer struct.
	// If a field in the outer struct has the same name as a field in the embedded struct,
	// the outer struct's field will shadow or override the embedded one allowing for a clean way to
	// hide fields from the swagger spec that still exist in the libraries struct.
	Container       string `json:"-"`
	ContainerConfig string `json:"-"`
	VirtualSize     string `json:"-"`
}

type ContainerConfig struct {
	dockerContainer.Config
}

type LibpodImagesPullReport struct {
	entities.ImagePullReport
}

// LibpodImagesRemoveReport is the return type for image removal via the rest
// api.
type LibpodImagesRemoveReport struct {
	entities.ImageRemoveReport
	// Image removal requires is to return data and an error.
	Errors []string
}

// LibpodImagesResolveReport includes a list of fully-qualified image references.
type LibpodImagesResolveReport struct {
	// Fully-qualified image references.
	Names []string
}

type ContainersPruneReport struct {
	dockerContainer.PruneReport
}

type ContainersPruneReportLibpod struct {
	ID             string `json:"Id"`
	SpaceReclaimed int64  `json:"Size"`
	// Error which occurred during prune operation (if any).
	// This field is optional and may be omitted if no error occurred.
	//
	// Extensions:
	// x-omitempty: true
	// x-nullable: true
	PruneError string `json:"Err,omitempty"`
}

type LibpodContainersRmReport struct {
	ID string `json:"Id"`
	// Error which occurred during Rm operation (if any).
	// This field is optional and may be omitted if no error occurred.
	//
	// Extensions:
	// x-omitempty: true
	// x-nullable: true
	RmError string `json:"Err,omitempty"`
}

// UpdateEntities used to wrap the oci resource spec in a swagger model
// swagger:model
type UpdateEntities struct {
	specs.LinuxResources
	define.UpdateHealthCheckConfig
	define.UpdateContainerDevicesLimits
	Env      []string
	UnsetEnv []string
}

type Info struct {
	system.Info
	BuildahVersion     string
	CPURealtimePeriod  bool
	CPURealtimeRuntime bool
	CgroupVersion      string
	Rootless           bool
	SwapFree           int64
	SwapTotal          int64
	Uptime             string
}

type Container struct {
	docker.Container
	dockerBackend.ContainerCreateConfig
}

type DiskUsage struct {
	docker.DiskUsage
}

type VolumesPruneReport struct {
	volume.PruneReport
}

type ImagesPruneReport struct {
	dockerImage.PruneReport
}

type BuildCachePruneReport struct {
	docker.BuildCachePruneReport
}

type NetworkPruneReport struct {
	dockerNetwork.PruneReport
}

type ConfigCreateResponse struct {
	docker.ConfigCreateResponse
}

type PushResult struct {
	docker.PushResult
}

type BuildResult struct {
	docker.BuildResult
}

type ContainerWaitOKBody struct {
	StatusCode int
	Error      *struct {
		Message string
	}
}

// CreateContainerConfig used when compatible endpoint creates a container
// swagger:model
type CreateContainerConfig struct {
	Name                   string                         // container name
	dockerContainer.Config                                // desired container configuration
	HostConfig             dockerContainer.HostConfig     // host dependent configuration for container
	NetworkingConfig       dockerNetwork.NetworkingConfig // network configuration for container
	EnvMerge               []string                       // preprocess env variables from image before injecting into containers
	UnsetEnv               []string                       // unset specified default environment variables
	UnsetEnvAll            bool                           // unset all default environment variables
}

type ContainerTopOKBody struct {
	dockerContainer.ContainerTopOKBody
}

type PodTopOKBody struct {
	dockerContainer.ContainerTopOKBody
}

// HistoryResponse provides details on image layers
type HistoryResponse struct {
	ID        string `json:"Id"`
	Created   int64
	CreatedBy string
	Tags      []string
	Size      int64
	Comment   string
}

type ExecCreateConfig struct {
	dockerContainer.ExecOptions
}

type ExecStartConfig struct {
	Detach bool   `json:"Detach"`
	Tty    bool   `json:"Tty"`
	Height uint16 `json:"h"`
	Width  uint16 `json:"w"`
}

type ExecRemoveConfig struct {
	Force bool `json:"Force"`
}
