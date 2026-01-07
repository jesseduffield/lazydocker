package driver

import (
	"github.com/containers/podman/v5/libpod/define"
	"go.podman.io/storage"
)

// GetDriverData returns information on a given store's running graph driver.
func GetDriverData(store storage.Store, layerID string) (*define.DriverData, error) {
	driver, err := store.GraphDriver()
	if err != nil {
		return nil, err
	}
	metaData, err := driver.Metadata(layerID)
	if err != nil {
		return nil, err
	}
	if mountTimes, err := store.Mounted(layerID); mountTimes == 0 || err != nil {
		delete(metaData, "MergedDir")
	}

	return &define.DriverData{
		Name: driver.String(),
		Data: metaData,
	}, nil
}
