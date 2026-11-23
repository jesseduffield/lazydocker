package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime"
	"time"
)

// we only need these two methods from our OSCommand struct, for killing commands
type CmdKiller interface {
	Kill(cmd *exec.Cmd) error
	PrepareForChildren(cmd *exec.Cmd)
}

type SSHHandler struct {
	oSCommand CmdKiller

	dialContext  func(ctx context.Context, network, addr string) (io.Closer, error)
	startCmd     func(*exec.Cmd) error
	tempDir      func(dir string, pattern string) (name string, err error)
	findFreePort func() (int, error)
	getenv       func(key string) string
	setenv       func(key, value string) error
}

func NewSSHHandler(oSCommand CmdKiller) *SSHHandler {
	return &SSHHandler{
		oSCommand: oSCommand,

		dialContext: func(ctx context.Context, network, addr string) (io.Closer, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		startCmd: func(cmd *exec.Cmd) error { return cmd.Start() },
		tempDir:  os.MkdirTemp,
		findFreePort: func() (int, error) {
			listener, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				return 0, err
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			return port, nil
		},
		getenv: os.Getenv,
		setenv: os.Setenv,
	}
}

// HandleSSHDockerHost overrides the DOCKER_HOST environment variable
// to point towards a local unix socket tunneled over SSH to the specified ssh host.
func (self *SSHHandler) HandleSSHDockerHost() (io.Closer, error) {
	const key = "DOCKER_HOST"
	ctx := context.Background()
	u, err := url.Parse(self.getenv(key))
	if err != nil {
		// if no or an invalid docker host is specified, continue nominally
		return noopCloser{}, nil
	}

	// if the docker host scheme is "ssh", forward the docker socket before creating the client
	if u.Scheme == "ssh" {
		tunnel, err := self.createDockerHostTunnel(ctx, u.Host)
		if err != nil {
			return noopCloser{}, fmt.Errorf("tunnel ssh docker host: %w", err)
		}
		err = self.setenv(key, tunnel.socketPath)
		if err != nil {
			return noopCloser{}, fmt.Errorf("override DOCKER_HOST to tunneled socket: %w", err)
		}

		return tunnel, nil
	}
	return noopCloser{}, nil
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

type tunneledDockerHost struct {
	socketPath string
	cmd        *exec.Cmd
	oSCommand  CmdKiller
}

var _ io.Closer = (*tunneledDockerHost)(nil)

func (t *tunneledDockerHost) Close() error {
	return t.oSCommand.Kill(t.cmd)
}

const socketTunnelTimeout = 8 * time.Second

func (self *SSHHandler) createDockerHostTunnel(ctx context.Context, remoteHost string) (*tunneledDockerHost, error) {
	if runtime.GOOS == "windows" {
		return self.createDockerHostTunnelTCP(ctx, remoteHost)
	}
	return self.createDockerHostTunnelUnix(ctx, remoteHost)
}

func (self *SSHHandler) createDockerHostTunnelUnix(ctx context.Context, remoteHost string) (*tunneledDockerHost, error) {
	socketDir, err := self.tempDir(os.TempDir(), "lazydocker-sshtunnel-")
	if err != nil {
		return nil, fmt.Errorf("create ssh tunnel tmp file: %w", err)
	}
	localSocket := path.Join(socketDir, "dockerhost.sock")

	cmd, err := self.tunnelSSH(ctx, remoteHost, localSocket)
	if err != nil {
		return nil, fmt.Errorf("tunnel docker host over ssh: %w", err)
	}

	// set a reasonable timeout, then wait for the socket to dial successfully
	// before attempting to create a new docker client
	ctx, cancel := context.WithTimeout(ctx, socketTunnelTimeout)
	defer cancel()

	err = self.retrySocketDial(ctx, "unix", localSocket)
	if err != nil {
		return nil, fmt.Errorf("ssh tunneled socket never became available: %w", err)
	}

	// construct the new DOCKER_HOST url with the proper scheme
	newDockerHostURL := url.URL{Scheme: "unix", Path: localSocket}
	return &tunneledDockerHost{
		socketPath: newDockerHostURL.String(),
		cmd:        cmd,
		oSCommand:  self.oSCommand,
	}, nil
}

// Attempt to dial the socket until it becomes available.
// The retry loop will continue until the parent context is canceled.
func (self *SSHHandler) retrySocketDial(ctx context.Context, network, address string) error {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		// attempt to dial the socket, exit on success
		err := self.tryDial(ctx, network, address)
		if err != nil {
			continue
		}
		return nil
	}
}

// Try to dial the specified socket, immediately close the connection if successfully created.
func (self *SSHHandler) tryDial(ctx context.Context, network, address string) error {
	conn, err := self.dialContext(ctx, network, address)
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}

func (self *SSHHandler) tunnelSSH(ctx context.Context, host, localSocket string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "ssh", "-L", localSocket+":/var/run/docker.sock", host, "-N")
	self.oSCommand.PrepareForChildren(cmd)
	err := self.startCmd(cmd)
	if err != nil {
		return nil, err
	}
	return cmd, nil
}

func (self *SSHHandler) createDockerHostTunnelTCP(ctx context.Context, remoteHost string) (*tunneledDockerHost, error) {
	port, err := self.findFreePort()
	if err != nil {
		return nil, fmt.Errorf("find free port for ssh tunnel: %w", err)
	}

	localAddr := fmt.Sprintf("localhost:%d", port)

	cmd, err := self.tunnelSSH(ctx, remoteHost, localAddr)
	if err != nil {
		return nil, fmt.Errorf("tunnel docker host over ssh: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, socketTunnelTimeout)
	defer cancel()

	err = self.retrySocketDial(ctx, "tcp", localAddr)
	if err != nil {
		self.oSCommand.Kill(cmd)
		return nil, fmt.Errorf("ssh tunneled socket never became available: %w", err)
	}

	newDockerHostURL := url.URL{Scheme: "tcp", Host: localAddr}
	return &tunneledDockerHost{
		socketPath: newDockerHostURL.String(),
		cmd:        cmd,
		oSCommand:  self.oSCommand,
	}, nil
}
