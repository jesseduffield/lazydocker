//go:build !remote

package libpod

import (
	"context"
	"errors"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/pkg/domain/entities/reports"
)

// Contains the public Runtime API for volumes

// A VolumeCreateOption is a functional option which alters the Volume created by
// NewVolume
type VolumeCreateOption func(*Volume) error

// VolumeFilter is a function to determine whether a volume is included in command
// output. Volumes to be outputted are tested using the function. a true return will
// include the volume, a false return will exclude it.
type VolumeFilter func(*Volume) bool

// RemoveVolume removes a volumes
func (r *Runtime) RemoveVolume(ctx context.Context, v *Volume, force bool, timeout *uint) error {
	if !r.valid {
		return define.ErrRuntimeStopped
	}

	return r.removeVolume(ctx, v, force, timeout, false)
}

// GetVolume retrieves a volume given its full name.
func (r *Runtime) GetVolume(name string) (*Volume, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	vol, err := r.state.Volume(name)
	if err != nil {
		return nil, err
	}

	return vol, nil
}

// LookupVolume retrieves a volume by unambiguous partial name.
func (r *Runtime) LookupVolume(name string) (*Volume, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	vol, err := r.state.LookupVolume(name)
	if err != nil {
		return nil, err
	}

	return vol, nil
}

// HasVolume checks to see if a volume with the given name exists
func (r *Runtime) HasVolume(name string) (bool, error) {
	if !r.valid {
		return false, define.ErrRuntimeStopped
	}

	return r.state.HasVolume(name)
}

// Volumes retrieves all volumes
// Filters can be provided which will determine which volumes are included in the
// output. If multiple filters are used, a volume will be returned if
// any of the filters are matched
func (r *Runtime) Volumes(filters ...VolumeFilter) ([]*Volume, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	vols, err := r.state.AllVolumes()
	if err != nil {
		return nil, err
	}

	if len(filters) == 0 {
		return vols, nil
	}

	volsFiltered := make([]*Volume, 0, len(vols))
	for _, vol := range vols {
		include := false
		for _, filter := range filters {
			include = include || filter(vol)
		}

		if include {
			volsFiltered = append(volsFiltered, vol)
		}
	}

	return volsFiltered, nil
}

// GetAllVolumes retrieves all the volumes
func (r *Runtime) GetAllVolumes() ([]*Volume, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	return r.state.AllVolumes()
}

// PruneVolumes removes unused volumes from the system
func (r *Runtime) PruneVolumes(ctx context.Context, filterFuncs []VolumeFilter) ([]*reports.PruneReport, error) {
	preports := make([]*reports.PruneReport, 0)
	vols, err := r.Volumes(filterFuncs...)
	if err != nil {
		return nil, err
	}

	for _, vol := range vols {
		report := new(reports.PruneReport)
		volSize, err := vol.Size()
		if err != nil {
			volSize = 0
		}
		report.Size = volSize
		report.Id = vol.Name()
		var timeout *uint
		if err := r.RemoveVolume(ctx, vol, false, timeout); err != nil {
			if !errors.Is(err, define.ErrVolumeBeingUsed) && !errors.Is(err, define.ErrVolumeRemoved) {
				report.Err = err
			} else {
				// We didn't remove the volume for some reason
				continue
			}
		} else {
			vol.newVolumeEvent(events.Prune)
		}
		preports = append(preports, report)
	}
	return preports, nil
}
