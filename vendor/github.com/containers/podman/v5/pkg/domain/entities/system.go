package entities

import (
	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

// ServiceOptions provides the input for starting an API and sidecar pprof services
type ServiceOptions = types.ServiceOptions
type SystemPruneOptions = types.SystemPruneOptions
type SystemPruneReport = types.SystemPruneReport
type SystemMigrateOptions = types.SystemMigrateOptions
type SystemCheckOptions = types.SystemCheckOptions
type SystemCheckReport = types.SystemCheckReport
type SystemDfOptions = types.SystemDfOptions
type SystemDfReport = types.SystemDfReport
type SystemDfImageReport = types.SystemDfImageReport
type SystemDfContainerReport = types.SystemDfContainerReport
type SystemDfVolumeReport = types.SystemDfVolumeReport
type SystemVersionReport = types.SystemVersionReport
type SystemUnshareOptions = types.SystemUnshareOptions
type ComponentVersion = types.SystemComponentVersion
type ListRegistriesReport = types.ListRegistriesReport

type AuthConfig = types.AuthConfig
type AuthReport = types.AuthReport
type LocksReport = types.LocksReport
