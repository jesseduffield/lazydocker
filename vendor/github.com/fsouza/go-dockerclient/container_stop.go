package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// StopContainer stops a container, killing it after the given timeout (in
// seconds).
//
// See https://goo.gl/R9dZcV for more details.
func (c *Client) StopContainer(id string, timeout uint) error {
	return c.stopContainer(id, timeout, doOptions{})
}

// StopContainerWithContext stops a container, killing it after the given
// timeout (in seconds). The context can be used to cancel the stop
// container request.
//
// See https://goo.gl/R9dZcV for more details.
func (c *Client) StopContainerWithContext(id string, timeout uint, ctx context.Context) error {
	return c.stopContainer(id, timeout, doOptions{context: ctx})
}

func (c *Client) stopContainer(id string, timeout uint, opts doOptions) error {
	path := fmt.Sprintf("/containers/%s/stop?t=%d", id, timeout)
	resp, err := c.do(http.MethodPost, path, opts)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchContainer{ID: id}
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return &ContainerNotRunning{ID: id}
	}
	return nil
}
