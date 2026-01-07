// Copyright 2017 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"encoding/json"
	"net/http"

	"github.com/docker/docker/api/types/registry"
)

// InspectDistribution returns image digest and platform information by contacting the registry
func (c *Client) InspectDistribution(name string) (*registry.DistributionInspect, error) {
	path := "/distribution/" + name + "/json"
	resp, err := c.do(http.MethodGet, path, doOptions{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var distributionInspect registry.DistributionInspect
	if err := json.NewDecoder(resp.Body).Decode(&distributionInspect); err != nil {
		return nil, err
	}
	return &distributionInspect, nil
}
