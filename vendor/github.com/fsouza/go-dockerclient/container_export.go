package docker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ExportContainerOptions is the set of parameters to the ExportContainer
// method.
//
// See https://goo.gl/yGJCIh for more details.
type ExportContainerOptions struct {
	ID                string
	OutputStream      io.Writer
	InactivityTimeout time.Duration `qs:"-"`
	Context           context.Context
}

// ExportContainer export the contents of container id as tar archive
// and prints the exported contents to stdout.
//
// See https://goo.gl/yGJCIh for more details.
func (c *Client) ExportContainer(opts ExportContainerOptions) error {
	if opts.ID == "" {
		return &NoSuchContainer{ID: opts.ID}
	}
	url := fmt.Sprintf("/containers/%s/export", opts.ID)
	return c.stream(http.MethodGet, url, streamOptions{
		setRawTerminal:    true,
		stdout:            opts.OutputStream,
		inactivityTimeout: opts.InactivityTimeout,
		context:           opts.Context,
	})
}
