// Copyright 2017 go-dockerclient authors. All rights reserved.
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

// NoSuchConfig is the error returned when a given config does not exist.
type NoSuchConfig struct {
	ID  string
	Err error
}

func (err *NoSuchConfig) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}
	return "No such config: " + err.ID
}

// CreateConfigOptions specify parameters to the CreateConfig function.
//
// See https://goo.gl/KrVjHz for more details.
type CreateConfigOptions struct {
	Auth AuthConfiguration `qs:"-"`
	swarm.ConfigSpec
	Context context.Context
}

// CreateConfig creates a new config, returning the config instance
// or an error in case of failure.
//
// See https://goo.gl/KrVjHz for more details.
func (c *Client) CreateConfig(opts CreateConfigOptions) (*swarm.Config, error) {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return nil, err
	}
	path := "/configs/create?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{
		headers:   headers,
		data:      opts.ConfigSpec,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var config swarm.Config
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// RemoveConfigOptions encapsulates options to remove a config.
//
// See https://goo.gl/Tqrtya for more details.
type RemoveConfigOptions struct {
	ID      string `qs:"-"`
	Context context.Context
}

// RemoveConfig removes a config, returning an error in case of failure.
//
// See https://goo.gl/Tqrtya for more details.
func (c *Client) RemoveConfig(opts RemoveConfigOptions) error {
	path := "/configs/" + opts.ID
	resp, err := c.do(http.MethodDelete, path, doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchConfig{ID: opts.ID}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// UpdateConfigOptions specify parameters to the UpdateConfig function.
//
// See https://goo.gl/wu3MmS for more details.
type UpdateConfigOptions struct {
	Auth AuthConfiguration `qs:"-"`
	swarm.ConfigSpec
	Context context.Context
	Version uint64
}

// UpdateConfig updates the config at ID with the options
//
// Only label can be updated
// https://docs.docker.com/engine/api/v1.33/#operation/ConfigUpdate
// See https://goo.gl/wu3MmS for more details.
func (c *Client) UpdateConfig(id string, opts UpdateConfigOptions) error {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return err
	}
	params := make(url.Values)
	params.Set("version", strconv.FormatUint(opts.Version, 10))
	resp, err := c.do(http.MethodPost, "/configs/"+id+"/update?"+params.Encode(), doOptions{
		headers:   headers,
		data:      opts.ConfigSpec,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchConfig{ID: id}
		}
		return err
	}
	defer resp.Body.Close()
	return nil
}

// InspectConfig returns information about a config by its ID.
//
// See https://goo.gl/dHmr75 for more details.
func (c *Client) InspectConfig(id string) (*swarm.Config, error) {
	path := "/configs/" + id
	resp, err := c.do(http.MethodGet, path, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchConfig{ID: id}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var config swarm.Config
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// ListConfigsOptions specify parameters to the ListConfigs function.
//
// See https://goo.gl/DwvNMd for more details.
type ListConfigsOptions struct {
	Filters map[string][]string
	Context context.Context
}

// ListConfigs returns a slice of configs matching the given criteria.
//
// See https://goo.gl/DwvNMd for more details.
func (c *Client) ListConfigs(opts ListConfigsOptions) ([]swarm.Config, error) {
	path := "/configs?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var configs []swarm.Config
	if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
		return nil, err
	}
	return configs, nil
}
