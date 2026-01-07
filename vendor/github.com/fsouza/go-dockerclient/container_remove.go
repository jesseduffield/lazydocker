package docker

import (
	"context"
	"errors"
	"net/http"
)

// RemoveContainerOptions encapsulates options to remove a container.
//
// See https://goo.gl/hL5IPC for more details.
type RemoveContainerOptions struct {
	// The ID of the container.
	ID string `qs:"-"`

	// A flag that indicates whether Docker should remove the volumes
	// associated to the container.
	RemoveVolumes bool `qs:"v"`

	// A flag that indicates whether Docker should remove the container
	// even if it is currently running.
	Force   bool
	Context context.Context
}

// RemoveContainer removes a container, returning an error in case of failure.
//
// See https://goo.gl/hL5IPC for more details.
func (c *Client) RemoveContainer(opts RemoveContainerOptions) error {
	path := "/containers/" + opts.ID + "?" + queryString(opts)
	resp, err := c.do(http.MethodDelete, path, doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchContainer{ID: opts.ID}
		}
		return err
	}
	resp.Body.Close()
	return nil
}
