package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// InspectContainer returns information about a container by its ID.
//
// Deprecated: Use InspectContainerWithOptions instead.
func (c *Client) InspectContainer(id string) (*Container, error) {
	return c.InspectContainerWithOptions(InspectContainerOptions{ID: id})
}

// InspectContainerWithContext returns information about a container by its ID.
// The context object can be used to cancel the inspect request.
//
// Deprecated: Use InspectContainerWithOptions instead.
func (c *Client) InspectContainerWithContext(id string, ctx context.Context) (*Container, error) {
	return c.InspectContainerWithOptions(InspectContainerOptions{ID: id, Context: ctx})
}

// InspectContainerWithOptions returns information about a container by its ID.
//
// See https://goo.gl/FaI5JT for more details.
func (c *Client) InspectContainerWithOptions(opts InspectContainerOptions) (*Container, error) {
	path := "/containers/" + opts.ID + "/json?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{
		context: opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchContainer{ID: opts.ID}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var container Container
	if err := json.NewDecoder(resp.Body).Decode(&container); err != nil {
		return nil, err
	}
	return &container, nil
}

// InspectContainerOptions specifies parameters for InspectContainerWithOptions.
//
// See https://goo.gl/FaI5JT for more details.
type InspectContainerOptions struct {
	Context context.Context
	ID      string `qs:"-"`
	Size    bool
}
