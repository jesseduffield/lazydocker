package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// CommitContainerOptions aggregates parameters to the CommitContainer method.
//
// See https://goo.gl/CzIguf for more details.
type CommitContainerOptions struct {
	Container  string
	Repository string `qs:"repo"`
	Tag        string
	Message    string `qs:"comment"`
	Author     string
	Changes    []string `qs:"changes"`
	Run        *Config  `qs:"-"`
	Context    context.Context
}

// CommitContainer creates a new image from a container's changes.
//
// See https://goo.gl/CzIguf for more details.
func (c *Client) CommitContainer(opts CommitContainerOptions) (*Image, error) {
	path := "/commit?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{
		data:    opts.Run,
		context: opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchContainer{ID: opts.Container}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var image Image
	if err := json.NewDecoder(resp.Body).Decode(&image); err != nil {
		return nil, err
	}
	return &image, nil
}
