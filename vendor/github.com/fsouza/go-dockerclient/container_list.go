package docker

import (
	"context"
	"encoding/json"
	"net/http"
)

// ListContainersOptions specify parameters to the ListContainers function.
//
// See https://goo.gl/kaOHGw for more details.
type ListContainersOptions struct {
	All     bool
	Size    bool
	Limit   int
	Since   string
	Before  string
	Filters map[string][]string
	Context context.Context
}

// ListContainers returns a slice of containers matching the given criteria.
//
// See https://goo.gl/kaOHGw for more details.
func (c *Client) ListContainers(opts ListContainersOptions) ([]APIContainers, error) {
	path := "/containers/json?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var containers []APIContainers
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, err
	}
	return containers, nil
}
