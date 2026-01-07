package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// ErrContainerAlreadyExists is the error returned by CreateContainer when the
// container already exists.
var ErrContainerAlreadyExists = errors.New("container already exists")

// CreateContainerOptions specify parameters to the CreateContainer function.
//
// See https://goo.gl/tyzwVM for more details.
type CreateContainerOptions struct {
	Name             string
	Platform         string
	Config           *Config           `qs:"-"`
	HostConfig       *HostConfig       `qs:"-"`
	NetworkingConfig *NetworkingConfig `qs:"-"`
	Context          context.Context
}

// CreateContainer creates a new container, returning the container instance,
// or an error in case of failure.
//
// The returned container instance contains only the container ID. To get more
// details about the container after creating it, use InspectContainer.
//
// See https://goo.gl/tyzwVM for more details.
func (c *Client) CreateContainer(opts CreateContainerOptions) (*Container, error) {
	path := "/containers/create?" + queryString(opts)
	resp, err := c.do(
		http.MethodPost,
		path,
		doOptions{
			data: struct {
				*Config
				HostConfig       *HostConfig       `json:"HostConfig,omitempty" yaml:"HostConfig,omitempty" toml:"HostConfig,omitempty"`
				NetworkingConfig *NetworkingConfig `json:"NetworkingConfig,omitempty" yaml:"NetworkingConfig,omitempty" toml:"NetworkingConfig,omitempty"`
			}{
				opts.Config,
				opts.HostConfig,
				opts.NetworkingConfig,
			},
			context: opts.Context,
		},
	)

	var e *Error
	if errors.As(err, &e) {
		if e.Status == http.StatusNotFound && strings.Contains(e.Message, "No such image") {
			return nil, ErrNoSuchImage
		}
		if e.Status == http.StatusConflict {
			return nil, ErrContainerAlreadyExists
		}
		// Workaround for 17.09 bug returning 400 instead of 409.
		// See https://github.com/moby/moby/issues/35021
		if e.Status == http.StatusBadRequest && strings.Contains(e.Message, "Conflict.") {
			return nil, ErrContainerAlreadyExists
		}
	}

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var container Container
	if err := json.NewDecoder(resp.Body).Decode(&container); err != nil {
		return nil, err
	}

	container.Name = opts.Name

	return &container, nil
}
