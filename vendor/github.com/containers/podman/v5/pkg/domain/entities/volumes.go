package entities

import (
	"io"
	"net/url"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

// VolumeCreateOptions provides details for creating volumes
type VolumeCreateOptions = types.VolumeCreateOptions

type VolumeConfigResponse = types.VolumeConfigResponse

type VolumeRmOptions struct {
	All     bool
	Force   bool
	Ignore  bool
	Timeout *uint
}

type VolumeRmReport = types.VolumeRmReport

type VolumeInspectReport = types.VolumeInspectReport

// VolumePruneOptions describes the options needed
// to prune a volume from the CLI
type VolumePruneOptions struct {
	Filters url.Values `json:"filters" schema:"filters"`
}

type VolumeListOptions struct {
	Filter map[string][]string
}

type VolumeListReport = types.VolumeListReport

// VolumeReloadReport describes the response from reload volume plugins
type VolumeReloadReport = types.VolumeReloadReport

// VolumeMountReport describes the response from volume mount
type VolumeMountReport = types.VolumeMountReport

// VolumeUnmountReport describes the response from umounting a volume
type VolumeUnmountReport = types.VolumeUnmountReport

// VolumeExportOptions describes the options required to export a volume.
type VolumeExportOptions struct {
	Output io.Writer
}

// VolumeImportOptions describes the options required to import a volume
type VolumeImportOptions struct {
	// Input will be closed upon being fully consumed
	Input io.Reader
}
