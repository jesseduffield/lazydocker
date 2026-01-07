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

var (
	// ErrNodeAlreadyInSwarm is the error returned by InitSwarm and JoinSwarm
	// when the node is already part of a Swarm.
	ErrNodeAlreadyInSwarm = errors.New("node already in a Swarm")

	// ErrNodeNotInSwarm is the error returned by LeaveSwarm and UpdateSwarm
	// when the node is not part of a Swarm.
	ErrNodeNotInSwarm = errors.New("node is not in a Swarm")
)

// InitSwarmOptions specify parameters to the InitSwarm function.
// See https://goo.gl/hzkgWu for more details.
type InitSwarmOptions struct {
	swarm.InitRequest
	Context context.Context
}

// InitSwarm initializes a new Swarm and returns the node ID.
// See https://goo.gl/ZWyG1M for more details.
func (c *Client) InitSwarm(opts InitSwarmOptions) (string, error) {
	path := "/swarm/init"
	resp, err := c.do(http.MethodPost, path, doOptions{
		data:      opts.InitRequest,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && (e.Status == http.StatusNotAcceptable || e.Status == http.StatusServiceUnavailable) {
			return "", ErrNodeAlreadyInSwarm
		}
		return "", err
	}
	defer resp.Body.Close()
	var response string
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}
	return response, nil
}

// JoinSwarmOptions specify parameters to the JoinSwarm function.
// See https://goo.gl/TdhJWU for more details.
type JoinSwarmOptions struct {
	swarm.JoinRequest
	Context context.Context
}

// JoinSwarm joins an existing Swarm.
// See https://goo.gl/N59IP1 for more details.
func (c *Client) JoinSwarm(opts JoinSwarmOptions) error {
	path := "/swarm/join"
	resp, err := c.do(http.MethodPost, path, doOptions{
		data:      opts.JoinRequest,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && (e.Status == http.StatusNotAcceptable || e.Status == http.StatusServiceUnavailable) {
			return ErrNodeAlreadyInSwarm
		}
	}
	resp.Body.Close()
	return err
}

// LeaveSwarmOptions specify parameters to the LeaveSwarm function.
// See https://goo.gl/UWDlLg for more details.
type LeaveSwarmOptions struct {
	Force   bool
	Context context.Context
}

// LeaveSwarm leaves a Swarm.
// See https://goo.gl/FTX1aD for more details.
func (c *Client) LeaveSwarm(opts LeaveSwarmOptions) error {
	params := make(url.Values)
	params.Set("force", strconv.FormatBool(opts.Force))
	path := "/swarm/leave?" + params.Encode()
	resp, err := c.do(http.MethodPost, path, doOptions{
		context: opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && (e.Status == http.StatusNotAcceptable || e.Status == http.StatusServiceUnavailable) {
			return ErrNodeNotInSwarm
		}
	}
	resp.Body.Close()
	return err
}

// UpdateSwarmOptions specify parameters to the UpdateSwarm function.
// See https://goo.gl/vFbq36 for more details.
type UpdateSwarmOptions struct {
	Version            int
	RotateWorkerToken  bool
	RotateManagerToken bool
	Swarm              swarm.Spec
	Context            context.Context
}

// UpdateSwarm updates a Swarm.
// See https://goo.gl/iJFnsw for more details.
func (c *Client) UpdateSwarm(opts UpdateSwarmOptions) error {
	params := make(url.Values)
	params.Set("version", strconv.Itoa(opts.Version))
	params.Set("rotateWorkerToken", strconv.FormatBool(opts.RotateWorkerToken))
	params.Set("rotateManagerToken", strconv.FormatBool(opts.RotateManagerToken))
	path := "/swarm/update?" + params.Encode()
	resp, err := c.do(http.MethodPost, path, doOptions{
		data:      opts.Swarm,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && (e.Status == http.StatusNotAcceptable || e.Status == http.StatusServiceUnavailable) {
			return ErrNodeNotInSwarm
		}
	}
	resp.Body.Close()
	return err
}

// InspectSwarm inspects a Swarm.
// See https://goo.gl/MFwgX9 for more details.
func (c *Client) InspectSwarm(ctx context.Context) (swarm.Swarm, error) {
	response := swarm.Swarm{}
	resp, err := c.do(http.MethodGet, "/swarm", doOptions{
		context: ctx,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && (e.Status == http.StatusNotAcceptable || e.Status == http.StatusServiceUnavailable) {
			return response, ErrNodeNotInSwarm
		}
		return response, err
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&response)
	return response, err
}
