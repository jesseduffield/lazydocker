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

// NoSuchSecret is the error returned when a given secret does not exist.
type NoSuchSecret struct {
	ID  string
	Err error
}

func (err *NoSuchSecret) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}
	return "No such secret: " + err.ID
}

// CreateSecretOptions specify parameters to the CreateSecret function.
//
// See https://goo.gl/KrVjHz for more details.
type CreateSecretOptions struct {
	Auth AuthConfiguration `qs:"-"`
	swarm.SecretSpec
	Context context.Context
}

// CreateSecret creates a new secret, returning the secret instance
// or an error in case of failure.
//
// See https://goo.gl/KrVjHz for more details.
func (c *Client) CreateSecret(opts CreateSecretOptions) (*swarm.Secret, error) {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return nil, err
	}
	path := "/secrets/create?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{
		headers:   headers,
		data:      opts.SecretSpec,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var secret swarm.Secret
	if err := json.NewDecoder(resp.Body).Decode(&secret); err != nil {
		return nil, err
	}
	return &secret, nil
}

// RemoveSecretOptions encapsulates options to remove a secret.
//
// See https://goo.gl/Tqrtya for more details.
type RemoveSecretOptions struct {
	ID      string `qs:"-"`
	Context context.Context
}

// RemoveSecret removes a secret, returning an error in case of failure.
//
// See https://goo.gl/Tqrtya for more details.
func (c *Client) RemoveSecret(opts RemoveSecretOptions) error {
	path := "/secrets/" + opts.ID
	resp, err := c.do(http.MethodDelete, path, doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchSecret{ID: opts.ID}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// UpdateSecretOptions specify parameters to the UpdateSecret function.
//
// Only label can be updated
// See https://docs.docker.com/engine/api/v1.33/#operation/SecretUpdate
// See https://goo.gl/wu3MmS for more details.
type UpdateSecretOptions struct {
	Auth AuthConfiguration `qs:"-"`
	swarm.SecretSpec
	Context context.Context
	Version uint64
}

// UpdateSecret updates the secret at ID with the options
//
// See https://goo.gl/wu3MmS for more details.
func (c *Client) UpdateSecret(id string, opts UpdateSecretOptions) error {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return err
	}
	params := make(url.Values)
	params.Set("version", strconv.FormatUint(opts.Version, 10))
	resp, err := c.do(http.MethodPost, "/secrets/"+id+"/update?"+params.Encode(), doOptions{
		headers:   headers,
		data:      opts.SecretSpec,
		forceJSON: true,
		context:   opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchSecret{ID: id}
		}
		return err
	}
	defer resp.Body.Close()
	return nil
}

// InspectSecret returns information about a secret by its ID.
//
// See https://goo.gl/dHmr75 for more details.
func (c *Client) InspectSecret(id string) (*swarm.Secret, error) {
	path := "/secrets/" + id
	resp, err := c.do(http.MethodGet, path, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchSecret{ID: id}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var secret swarm.Secret
	if err := json.NewDecoder(resp.Body).Decode(&secret); err != nil {
		return nil, err
	}
	return &secret, nil
}

// ListSecretsOptions specify parameters to the ListSecrets function.
//
// See https://goo.gl/DwvNMd for more details.
type ListSecretsOptions struct {
	Filters map[string][]string
	Context context.Context
}

// ListSecrets returns a slice of secrets matching the given criteria.
//
// See https://goo.gl/DwvNMd for more details.
func (c *Client) ListSecrets(opts ListSecretsOptions) ([]swarm.Secret, error) {
	path := "/secrets?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var secrets []swarm.Secret
	if err := json.NewDecoder(resp.Body).Decode(&secrets); err != nil {
		return nil, err
	}
	return secrets, nil
}
