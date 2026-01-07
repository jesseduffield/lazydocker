// Copyright 2015 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

var (
	// ErrNoSuchVolume is the error returned when the volume does not exist.
	ErrNoSuchVolume = errors.New("no such volume")

	// ErrVolumeInUse is the error returned when the volume requested to be removed is still in use.
	ErrVolumeInUse = errors.New("volume in use and cannot be removed")
)

// Volume represents a volume.
//
// See https://goo.gl/3wgTsd for more details.
type Volume struct {
	Name       string            `json:"Name" yaml:"Name" toml:"Name"`
	Driver     string            `json:"Driver,omitempty" yaml:"Driver,omitempty" toml:"Driver,omitempty"`
	Mountpoint string            `json:"Mountpoint,omitempty" yaml:"Mountpoint,omitempty" toml:"Mountpoint,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty" yaml:"Labels,omitempty" toml:"Labels,omitempty"`
	Options    map[string]string `json:"Options,omitempty" yaml:"Options,omitempty" toml:"Options,omitempty"`
	CreatedAt  time.Time         `json:"CreatedAt,omitempty" yaml:"CreatedAt,omitempty" toml:"CreatedAt,omitempty"`
}

// ListVolumesOptions specify parameters to the ListVolumes function.
//
// See https://goo.gl/3wgTsd for more details.
type ListVolumesOptions struct {
	Filters map[string][]string
	Context context.Context
}

// ListVolumes returns a list of available volumes in the server.
//
// See https://goo.gl/3wgTsd for more details.
func (c *Client) ListVolumes(opts ListVolumesOptions) ([]Volume, error) {
	resp, err := c.do(http.MethodGet, "/volumes?"+queryString(opts), doOptions{
		context: opts.Context,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	m := make(map[string]any)
	if err = json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	var volumes []Volume
	volumesJSON, ok := m["Volumes"]
	if !ok {
		return volumes, nil
	}
	data, err := json.Marshal(volumesJSON)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &volumes); err != nil {
		return nil, err
	}
	return volumes, nil
}

// CreateVolumeOptions specify parameters to the CreateVolume function.
//
// See https://goo.gl/qEhmEC for more details.
type CreateVolumeOptions struct {
	Name       string
	Driver     string
	DriverOpts map[string]string
	Context    context.Context `json:"-"`
	Labels     map[string]string
}

// CreateVolume creates a volume on the server.
//
// See https://goo.gl/qEhmEC for more details.
func (c *Client) CreateVolume(opts CreateVolumeOptions) (*Volume, error) {
	resp, err := c.do(http.MethodPost, "/volumes/create", doOptions{
		data:    opts,
		context: opts.Context,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var volume Volume
	if err := json.NewDecoder(resp.Body).Decode(&volume); err != nil {
		return nil, err
	}
	return &volume, nil
}

// InspectVolume returns a volume by its name.
//
// See https://goo.gl/GMjsMc for more details.
func (c *Client) InspectVolume(name string) (*Volume, error) {
	resp, err := c.do(http.MethodGet, "/volumes/"+name, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, ErrNoSuchVolume
		}
		return nil, err
	}
	defer resp.Body.Close()
	var volume Volume
	if err := json.NewDecoder(resp.Body).Decode(&volume); err != nil {
		return nil, err
	}
	return &volume, nil
}

// RemoveVolume removes a volume by its name.
//
// Deprecated: Use RemoveVolumeWithOptions instead.
func (c *Client) RemoveVolume(name string) error {
	return c.RemoveVolumeWithOptions(RemoveVolumeOptions{Name: name})
}

// RemoveVolumeOptions specify parameters to the RemoveVolumeWithOptions
// function.
//
// See https://goo.gl/nvd6qj for more details.
type RemoveVolumeOptions struct {
	Context context.Context
	Name    string `qs:"-"`
	Force   bool
}

// RemoveVolumeWithOptions removes a volume by its name and takes extra
// parameters.
//
// See https://goo.gl/nvd6qj for more details.
func (c *Client) RemoveVolumeWithOptions(opts RemoveVolumeOptions) error {
	path := "/volumes/" + opts.Name
	resp, err := c.do(http.MethodDelete, path+"?"+queryString(opts), doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			if e.Status == http.StatusNotFound {
				return ErrNoSuchVolume
			}
			if e.Status == http.StatusConflict {
				return ErrVolumeInUse
			}
		}
		return err
	}
	defer resp.Body.Close()
	return nil
}

// PruneVolumesOptions specify parameters to the PruneVolumes function.
//
// See https://goo.gl/f9XDem for more details.
type PruneVolumesOptions struct {
	Filters map[string][]string
	Context context.Context
}

// PruneVolumesResults specify results from the PruneVolumes function.
//
// See https://goo.gl/f9XDem for more details.
type PruneVolumesResults struct {
	VolumesDeleted []string
	SpaceReclaimed int64
}

// PruneVolumes deletes volumes which are unused.
//
// See https://goo.gl/f9XDem for more details.
func (c *Client) PruneVolumes(opts PruneVolumesOptions) (*PruneVolumesResults, error) {
	path := "/volumes/prune?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var results PruneVolumesResults
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	return &results, nil
}
