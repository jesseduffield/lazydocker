package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// CopyFromContainerOptions contains the set of options used for copying
// files from a container.
//
// Deprecated: Use DownloadFromContainerOptions and DownloadFromContainer instead.
type CopyFromContainerOptions struct {
	OutputStream io.Writer `json:"-"`
	Container    string    `json:"-"`
	Resource     string
	Context      context.Context `json:"-"`
}

// CopyFromContainer copies files from a container.
//
// Deprecated: Use DownloadFromContainer and DownloadFromContainer instead.
func (c *Client) CopyFromContainer(opts CopyFromContainerOptions) error {
	if opts.Container == "" {
		return &NoSuchContainer{ID: opts.Container}
	}
	if c.serverAPIVersion == nil {
		c.checkAPIVersion()
	}
	if c.serverAPIVersion != nil && c.serverAPIVersion.GreaterThanOrEqualTo(apiVersion124) {
		return errors.New("go-dockerclient: CopyFromContainer is no longer available in Docker >= 1.12, use DownloadFromContainer instead")
	}
	url := fmt.Sprintf("/containers/%s/copy", opts.Container)
	resp, err := c.do(http.MethodPost, url, doOptions{
		data:    opts,
		context: opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchContainer{ID: opts.Container}
		}
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(opts.OutputStream, resp.Body)
	return err
}
