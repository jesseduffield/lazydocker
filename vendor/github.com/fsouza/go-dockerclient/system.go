package docker

import (
	"context"
	"encoding/json"
	"net/http"
)

// VolumeUsageData represents usage data from the docker system api
// More Info Here https://dockr.ly/2PNzQyO
type VolumeUsageData struct {
	// The number of containers referencing this volume. This field
	// is set to `-1` if the reference-count is not available.
	//
	// Required: true
	RefCount int64 `json:"RefCount"`

	// Amount of disk space used by the volume (in bytes). This information
	// is only available for volumes created with the `"local"` volume
	// driver. For volumes created with other volume drivers, this field
	// is set to `-1` ("not available")
	//
	// Required: true
	Size int64 `json:"Size"`
}

// ImageSummary represents data about what images are
// currently known to docker
// More Info Here https://dockr.ly/2PNzQyO
type ImageSummary struct {
	Containers  int64             `json:"Containers"`
	Created     int64             `json:"Created"`
	ID          string            `json:"Id"`
	Labels      map[string]string `json:"Labels"`
	ParentID    string            `json:"ParentId"`
	RepoDigests []string          `json:"RepoDigests"`
	RepoTags    []string          `json:"RepoTags"`
	SharedSize  int64             `json:"SharedSize"`
	Size        int64             `json:"Size"`
	VirtualSize int64             `json:"VirtualSize"`
}

// DiskUsage holds information about what docker is using disk space on.
// More Info Here https://dockr.ly/2PNzQyO
type DiskUsage struct {
	LayersSize int64
	Images     []*ImageSummary
	Containers []*APIContainers
	Volumes    []*Volume
}

// DiskUsageOptions only contains a context for canceling.
type DiskUsageOptions struct {
	Context context.Context
}

// DiskUsage returns a *DiskUsage describing what docker is using disk on.
//
// More Info Here https://dockr.ly/2PNzQyO
func (c *Client) DiskUsage(opts DiskUsageOptions) (*DiskUsage, error) {
	path := "/system/df"
	resp, err := c.do(http.MethodGet, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var du *DiskUsage
	if err := json.NewDecoder(resp.Body).Decode(&du); err != nil {
		return nil, err
	}
	return du, nil
}
