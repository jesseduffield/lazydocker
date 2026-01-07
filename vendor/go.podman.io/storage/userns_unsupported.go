//go:build !linux

package storage

import (
	"errors"

	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/types"
)

func (s *store) getAutoUserNS(_ *types.AutoUserNsOptions, _ *Image, _ rwLayerStore, _ []roLayerStore) ([]idtools.IDMap, []idtools.IDMap, error) {
	return nil, nil, errors.New("user namespaces are not supported on this platform")
}
