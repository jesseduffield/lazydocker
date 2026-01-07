//go:build !remote

package libpod

import (
	"fmt"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/layers"
	"go.podman.io/storage/pkg/archive"
)

var initInodes = map[string]bool{
	"/dev":                   true,
	"/etc/hostname":          true,
	"/etc/hosts":             true,
	"/etc/resolv.conf":       true,
	"/proc":                  true,
	"/run":                   true,
	"/run/notify":            true,
	"/run/.containerenv":     true,
	"/run/secrets":           true,
	define.ContainerInitPath: true,
	"/sys":                   true,
	"/etc/mtab":              true,
}

// GetDiff returns the differences between the two images, layers, or containers
func (r *Runtime) GetDiff(from, to string, diffType define.DiffType) ([]archive.Change, error) {
	toLayer, err := r.getLayerID(to, diffType)
	if err != nil {
		return nil, err
	}
	fromLayer := ""
	if from != "" {
		fromLayer, err = r.getLayerID(from, diffType)
		if err != nil {
			return nil, err
		}
	}
	var rchanges []archive.Change
	changes, err := r.store.Changes(fromLayer, toLayer)
	if err == nil {
		for _, c := range changes {
			if initInodes[c.Path] {
				continue
			}
			rchanges = append(rchanges, c)
		}
	}
	return rchanges, err
}

// GetLayerID gets a full layer id given a full or partial id
// If the id matches a container or image, the id of the top layer is returned
// If the id matches a layer, the top layer id is returned
func (r *Runtime) getLayerID(id string, diffType define.DiffType) (string, error) {
	var lastErr error
	if diffType&define.DiffImage == define.DiffImage {
		toImage, _, err := r.libimageRuntime.LookupImage(id, nil)
		if err == nil {
			return toImage.TopLayer(), nil
		}
		lastErr = err
	}

	if diffType&define.DiffContainer == define.DiffContainer {
		toCtr, err := r.store.Container(id)
		if err == nil {
			return toCtr.LayerID, nil
		}
		lastErr = err
	}

	if diffType == define.DiffAll {
		toLayer, err := layers.FullID(r.store, id)
		if err == nil {
			return toLayer, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("%s not found: %w", id, lastErr)
}
