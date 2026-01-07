package docker

import (
	"errors"
	"fmt"
	"net/http"
)

// UnpauseContainer unpauses the given container.
//
// See https://goo.gl/sZ2faO for more details.
func (c *Client) UnpauseContainer(id string) error {
	path := fmt.Sprintf("/containers/%s/unpause", id)
	resp, err := c.do(http.MethodPost, path, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchContainer{ID: id}
		}
		return err
	}
	resp.Body.Close()
	return nil
}
