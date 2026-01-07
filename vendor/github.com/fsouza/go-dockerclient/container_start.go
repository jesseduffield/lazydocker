package docker

import (
	"context"
	"errors"
	"net/http"
)

// StartContainer starts a container, returning an error in case of failure.
//
// Passing the HostConfig to this method has been deprecated in Docker API 1.22
// (Docker Engine 1.10.x) and totally removed in Docker API 1.24 (Docker Engine
// 1.12.x). The client will ignore the parameter when communicating with Docker
// API 1.24 or greater.
//
// See https://goo.gl/fbOSZy for more details.
func (c *Client) StartContainer(id string, hostConfig *HostConfig) error {
	return c.startContainer(id, hostConfig, doOptions{})
}

// StartContainerWithContext starts a container, returning an error in case of
// failure. The context can be used to cancel the outstanding start container
// request.
//
// Passing the HostConfig to this method has been deprecated in Docker API 1.22
// (Docker Engine 1.10.x) and totally removed in Docker API 1.24 (Docker Engine
// 1.12.x). The client will ignore the parameter when communicating with Docker
// API 1.24 or greater.
//
// See https://goo.gl/fbOSZy for more details.
func (c *Client) StartContainerWithContext(id string, hostConfig *HostConfig, ctx context.Context) error {
	return c.startContainer(id, hostConfig, doOptions{context: ctx})
}

func (c *Client) startContainer(id string, hostConfig *HostConfig, opts doOptions) error {
	path := "/containers/" + id + "/start"
	if c.serverAPIVersion == nil {
		c.checkAPIVersion()
	}
	if c.serverAPIVersion != nil && c.serverAPIVersion.LessThan(apiVersion124) {
		opts.data = hostConfig
		opts.forceJSON = true
	}
	resp, err := c.do(http.MethodPost, path, opts)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchContainer{ID: id, Err: err}
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return &ContainerAlreadyRunning{ID: id}
	}
	return nil
}
