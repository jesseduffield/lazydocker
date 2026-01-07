package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// WaitContainer blocks until the given container stops, return the exit code
// of the container status.
//
// See https://goo.gl/4AGweZ for more details.
func (c *Client) WaitContainer(id string) (int, error) {
	return c.waitContainer(id, doOptions{})
}

// WaitContainerWithContext blocks until the given container stops, return the exit code
// of the container status. The context object can be used to cancel the
// inspect request.
//
// See https://goo.gl/4AGweZ for more details.
func (c *Client) WaitContainerWithContext(id string, ctx context.Context) (int, error) {
	return c.waitContainer(id, doOptions{context: ctx})
}

func (c *Client) waitContainer(id string, opts doOptions) (int, error) {
	resp, err := c.do(http.MethodPost, "/containers/"+id+"/wait", opts)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return 0, &NoSuchContainer{ID: id}
		}
		return 0, err
	}
	defer resp.Body.Close()
	var r struct{ StatusCode int }
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return 0, err
	}
	return r.StatusCode, nil
}
