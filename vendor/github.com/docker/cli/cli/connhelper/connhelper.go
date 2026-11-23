// Package connhelper provides helpers for connecting to a remote daemon host with custom logic.
package connhelper

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/docker/cli/cli/connhelper/commandconn"
	"github.com/docker/cli/cli/connhelper/ssh"
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
		sp, err := ssh.NewSpec(u)
		if err != nil {
			return nil, fmt.Errorf("ssh host connection is not valid: %w", err)
		}
		sshFlags = addSSHTimeout(sshFlags)
		sshFlags = disablePseudoTerminalAllocation(sshFlags)

		remoteCommand := []string{"docker", "system", "dial-stdio"}
		socketPath := sp.Path
		if strings.Trim(sp.Path, "/") != "" {
			remoteCommand = []string{"docker", "--host=unix://" + socketPath, "system", "dial-stdio"}
		}
		sshArgs, err := sp.Command(sshFlags, remoteCommand...)
		if err != nil {
			return nil, err
		}
		return &ConnectionHelper{
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return commandconn.New(ctx, "ssh", sshArgs...)
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

// disablePseudoTerminalAllocation disables pseudo-terminal allocation to
// prevent SSH from executing as a login shell
func disablePseudoTerminalAllocation(sshFlags []string) []string {
	for _, flag := range sshFlags {
		if flag == "-T" {
			return sshFlags
		}
	}
	return append(sshFlags, "-T")
}
