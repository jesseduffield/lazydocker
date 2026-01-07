// Copyright 2016 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows

package docker

import (
	"context"
	"net"
	"net/http"
)

const defaultHost = "unix:///var/run/docker.sock"

// initializeNativeClient initializes the native Unix domain socket client on
// Unix-style operating systems
func (c *Client) initializeNativeClient(trFunc func() *http.Transport) {
	if c.endpointURL.Scheme != unixProtocol {
		return
	}
	sockPath := c.endpointURL.Path

	tr := trFunc()
	tr.Proxy = nil
	tr.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
		return c.Dialer.Dial(unixProtocol, sockPath)
	}
	c.HTTPClient.Transport = tr
}
