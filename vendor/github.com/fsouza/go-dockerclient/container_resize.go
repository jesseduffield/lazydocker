package docker

import (
	"net/http"
	"net/url"
	"strconv"
)

// ResizeContainerTTY resizes the terminal to the given height and width.
//
// See https://goo.gl/FImjeq for more details.
func (c *Client) ResizeContainerTTY(id string, height, width int) error {
	params := make(url.Values)
	params.Set("h", strconv.Itoa(height))
	params.Set("w", strconv.Itoa(width))
	resp, err := c.do(http.MethodPost, "/containers/"+id+"/resize?"+params.Encode(), doOptions{})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
