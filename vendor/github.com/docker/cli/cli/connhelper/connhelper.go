// Package connhelper provides helpers for connecting to a remote daemon host with custom logic.
package connhelper

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/docker/cli/cli/connhelper/commandconn"
	"github.com/docker/cli/cli/connhelper/ssh"
	"github.com/pkg/errors"
)

// ConnectionHelper allows to connect to a remote host with custom stream provider binary.
type ConnectionHelper struct {
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)
	Host   string // dummy URL used for HTTP requests. e.g. "http://docker"
}

// GetConnectionHelper returns Docker-specific connection helper for the given URL.
// GetConnectionHelper returns nil without error when no helper is registered for the scheme.
//
// ssh://<user>@<host> URL requires Docker 18.09 or later on the remote host.
func GetConnectionHelper(daemonURL string) (*ConnectionHelper, error) {
	return getConnectionHelper(daemonURL, nil)
}

// GetConnectionHelperWithSSHOpts returns Docker-specific connection helper for
// the given URL, and accepts additional options for ssh connections. It returns
// nil without error when no helper is registered for the scheme.
//
// Requires Docker 18.09 or later on the remote host.
func GetConnectionHelperWithSSHOpts(daemonURL string, sshFlags []string) (*ConnectionHelper, error) {
	return getConnectionHelper(daemonURL, sshFlags)
}

func getConnectionHelper(daemonURL string, sshFlags []string) (*ConnectionHelper, error) {
	u, err := url.Parse(daemonURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "ssh" {
		sp, err := ssh.ParseURL(daemonURL)
		if err != nil {
			return nil, errors.Wrap(err, "ssh host connection is not valid")
		}
		return &ConnectionHelper{
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				args := []string{"docker"}
				if sp.Path != "" {
					args = append(args, "--host", "unix://"+sp.Path)
				}
				sshFlags = addSSHTimeout(sshFlags)
				args = append(args, "system", "dial-stdio")
				return commandconn.New(ctx, "ssh", append(sshFlags, sp.Args(args...)...)...)
			},
			Host: "http://docker.example.com",
		}, nil
	}
	// Future version may support plugins via ~/.docker/config.json. e.g. "dind"
	// See docker/cli#889 for the previous discussion.
	return nil, err
}

// GetCommandConnectionHelper returns Docker-specific connection helper constructed from an arbitrary command.
func GetCommandConnectionHelper(cmd string, flags ...string) (*ConnectionHelper, error) {
	return &ConnectionHelper{
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return commandconn.New(ctx, cmd, flags...)
		},
		Host: "http://docker.example.com",
	}, nil
}

func addSSHTimeout(sshFlags []string) []string {
	if !strings.Contains(strings.Join(sshFlags, ""), "ConnectTimeout") {
		sshFlags = append(sshFlags, "-o ConnectTimeout=30")
	}
	return sshFlags
}
