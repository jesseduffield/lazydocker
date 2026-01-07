package docker

import (
	"errors"
	"fmt"
	"net/http"
)

// PauseContainer pauses the given container.
//
// See https://goo.gl/D1Yaii for more details.
func (c *Client) PauseContainer(id string) error {
	path := fmt.Sprintf("/containers/%s/pause", id)
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
