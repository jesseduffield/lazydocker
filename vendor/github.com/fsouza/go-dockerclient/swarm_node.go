// Copyright 2016 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/docker/docker/api/types/swarm"
)

// NoSuchNode is the error returned when a given node does not exist.
type NoSuchNode struct {
	ID  string
	Err error
}

func (err *NoSuchNode) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}
	return "No such node: " + err.ID
}

// ListNodesOptions specify parameters to the ListNodes function.
//
// See http://goo.gl/3K4GwU for more details.
type ListNodesOptions struct {
	Filters map[string][]string
	Context context.Context
}

// ListNodes returns a slice of nodes matching the given criteria.
//
// See http://goo.gl/3K4GwU for more details.
func (c *Client) ListNodes(opts ListNodesOptions) ([]swarm.Node, error) {
	path := "/nodes?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var nodes []swarm.Node
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// InspectNode returns information about a node by its ID.
//
// See http://goo.gl/WjkTOk for more details.
func (c *Client) InspectNode(id string) (*swarm.Node, error) {
	resp, err := c.do(http.MethodGet, "/nodes/"+id, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchNode{ID: id}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var node swarm.Node
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		return nil, err
	}
	return &node, nil
}

// UpdateNodeOptions specify parameters to the NodeUpdate function.
//
// See http://goo.gl/VPBFgA for more details.
type UpdateNodeOptions struct {
	swarm.NodeSpec
	Version uint64
	Context context.Context
}

// UpdateNode updates a node.
//
// See http://goo.gl/VPBFgA for more details.
func (c *Client) UpdateNode(id string, opts UpdateNodeOptions) error {
	params := make(url.Values)
	params.Set("version", strconv.FormatUint(opts.Version, 10))
	path := "/nodes/" + id + "/update?" + params.Encode()
	resp, err := c.do(http.MethodPost, path, doOptions{
		context:   opts.Context,
		forceJSON: true,
		data:      opts.NodeSpec,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchNode{ID: id}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// RemoveNodeOptions specify parameters to the RemoveNode function.
//
// See http://goo.gl/0SNvYg for more details.
type RemoveNodeOptions struct {
	ID      string
	Force   bool
	Context context.Context
}

// RemoveNode removes a node.
//
// See http://goo.gl/0SNvYg for more details.
func (c *Client) RemoveNode(opts RemoveNodeOptions) error {
	params := make(url.Values)
	params.Set("force", strconv.FormatBool(opts.Force))
	path := "/nodes/" + opts.ID + "?" + params.Encode()
	resp, err := c.do(http.MethodDelete, path, doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchNode{ID: opts.ID}
		}
		return err
	}
	resp.Body.Close()
	return nil
}
