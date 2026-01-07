// Copyright 2016 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

// NoSuchService is the error returned when a given service does not exist.
type NoSuchService struct {
	ID  string
	Err error
}

func (err *NoSuchService) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}
	return "No such service: " + err.ID
}

// CreateServiceOptions specify parameters to the CreateService function.
//
// See https://goo.gl/KrVjHz for more details.
type CreateServiceOptions struct {
	Auth AuthConfiguration `qs:"-"`
	swarm.ServiceSpec
	Context context.Context
}

// CreateService creates a new service, returning the service instance
// or an error in case of failure.
//
// See https://goo.gl/KrVjHz for more details.
func (c *Client) CreateService(opts CreateServiceOptions) (*swarm.Service, error) {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return nil, err
	}
	path := "/services/create?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{
		headers:   headers,
		data:      opts.ServiceSpec,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var service swarm.Service
	if err := json.NewDecoder(resp.Body).Decode(&service); err != nil {
		return nil, err
	}
	return &service, nil
}

// RemoveServiceOptions encapsulates options to remove a service.
//
// See https://goo.gl/Tqrtya for more details.
type RemoveServiceOptions struct {
	ID      string `qs:"-"`
	Context context.Context
}

// RemoveService removes a service, returning an error in case of failure.
//
// See https://goo.gl/Tqrtya for more details.
func (c *Client) RemoveService(opts RemoveServiceOptions) error {
	path := "/services/" + opts.ID
	resp, err := c.do(http.MethodDelete, path, doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchService{ID: opts.ID}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// UpdateServiceOptions specify parameters to the UpdateService function.
//
// See https://goo.gl/wu3MmS for more details.
type UpdateServiceOptions struct {
	Auth              AuthConfiguration `qs:"-"`
	swarm.ServiceSpec `qs:"-"`
	Context           context.Context
	Version           uint64
	Rollback          string
}

// UpdateService updates the service at ID with the options
//
// See https://goo.gl/wu3MmS for more details.
func (c *Client) UpdateService(id string, opts UpdateServiceOptions) error {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return err
	}
	resp, err := c.do(http.MethodPost, "/services/"+id+"/update?"+queryString(opts), doOptions{
		headers:   headers,
		data:      opts.ServiceSpec,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchService{ID: id}
		}
		return err
	}
	defer resp.Body.Close()
	return nil
}

// InspectService returns information about a service by its ID.
//
// See https://goo.gl/dHmr75 for more details.
func (c *Client) InspectService(id string) (*swarm.Service, error) {
	path := "/services/" + id
	resp, err := c.do(http.MethodGet, path, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchService{ID: id}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var service swarm.Service
	if err := json.NewDecoder(resp.Body).Decode(&service); err != nil {
		return nil, err
	}
	return &service, nil
}

// ListServicesOptions specify parameters to the ListServices function.
//
// See https://goo.gl/DwvNMd for more details.
type ListServicesOptions struct {
	Filters map[string][]string
	Status  bool
	Context context.Context
}

// ListServices returns a slice of services matching the given criteria.
//
// See https://goo.gl/DwvNMd for more details.
func (c *Client) ListServices(opts ListServicesOptions) ([]swarm.Service, error) {
	path := "/services?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var services []swarm.Service
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, err
	}
	return services, nil
}

// LogsServiceOptions represents the set of options used when getting logs from a
// service.
type LogsServiceOptions struct {
	Context           context.Context
	Service           string        `qs:"-"`
	OutputStream      io.Writer     `qs:"-"`
	ErrorStream       io.Writer     `qs:"-"`
	InactivityTimeout time.Duration `qs:"-"`
	Tail              string
	Since             int64

	// Use raw terminal? Usually true when the container contains a TTY.
	RawTerminal bool `qs:"-"`
	Follow      bool
	Stdout      bool
	Stderr      bool
	Timestamps  bool
	Details     bool
}

// GetServiceLogs gets stdout and stderr logs from the specified service.
//
// When LogsServiceOptions.RawTerminal is set to false, go-dockerclient will multiplex
// the streams and send the containers stdout to LogsServiceOptions.OutputStream, and
// stderr to LogsServiceOptions.ErrorStream.
//
// When LogsServiceOptions.RawTerminal is true, callers will get the raw stream on
// LogsServiceOptions.OutputStream.
func (c *Client) GetServiceLogs(opts LogsServiceOptions) error {
	if opts.Service == "" {
		return &NoSuchService{ID: opts.Service}
	}
	if opts.Tail == "" {
		opts.Tail = "all"
	}
	path := "/services/" + opts.Service + "/logs?" + queryString(opts)
	return c.stream(http.MethodGet, path, streamOptions{
		setRawTerminal:    opts.RawTerminal,
		stdout:            opts.OutputStream,
		stderr:            opts.ErrorStream,
		inactivityTimeout: opts.InactivityTimeout,
		context:           opts.Context,
	})
}
