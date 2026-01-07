package docker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// UploadToContainerOptions is the set of options that can be used when
// uploading an archive into a container.
//
// See https://goo.gl/g25o7u for more details.
type UploadToContainerOptions struct {
	InputStream          io.Reader `json:"-" qs:"-"`
	Path                 string    `qs:"path"`
	NoOverwriteDirNonDir bool      `qs:"noOverwriteDirNonDir"`
	Context              context.Context
}

// UploadToContainer uploads a tar archive to be extracted to a path in the
// filesystem of the container.
//
// See https://goo.gl/g25o7u for more details.
func (c *Client) UploadToContainer(id string, opts UploadToContainerOptions) error {
	url := fmt.Sprintf("/containers/%s/archive?", id) + queryString(opts)

	return c.stream(http.MethodPut, url, streamOptions{
		in:      opts.InputStream,
		context: opts.Context,
	})
}

// DownloadFromContainerOptions is the set of options that can be used when
// downloading resources from a container.
//
// See https://goo.gl/W49jxK for more details.
type DownloadFromContainerOptions struct {
	OutputStream      io.Writer     `json:"-" qs:"-"`
	Path              string        `qs:"path"`
	InactivityTimeout time.Duration `qs:"-"`
	Context           context.Context
}

// DownloadFromContainer downloads a tar archive of files or folders in a container.
//
// See https://goo.gl/W49jxK for more details.
func (c *Client) DownloadFromContainer(id string, opts DownloadFromContainerOptions) error {
	url := fmt.Sprintf("/containers/%s/archive?", id) + queryString(opts)

	return c.stream(http.MethodGet, url, streamOptions{
		setRawTerminal:    true,
		stdout:            opts.OutputStream,
		inactivityTimeout: opts.InactivityTimeout,
		context:           opts.Context,
	})
}
