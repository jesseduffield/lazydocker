package docker

import (
	"context"
	"encoding/json"
	"net/http"
)

// PruneContainersOptions specify parameters to the PruneContainers function.
//
// See https://goo.gl/wnkgDT for more details.
type PruneContainersOptions struct {
	Filters map[string][]string
	Context context.Context
}

// PruneContainersResults specify results from the PruneContainers function.
//
// See https://goo.gl/wnkgDT for more details.
type PruneContainersResults struct {
	ContainersDeleted []string
	SpaceReclaimed    int64
}

// PruneContainers deletes containers which are stopped.
//
// See https://goo.gl/wnkgDT for more details.
func (c *Client) PruneContainers(opts PruneContainersOptions) (*PruneContainersResults, error) {
	path := "/containers/prune?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var results PruneContainersResults
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	return &results, nil
}
