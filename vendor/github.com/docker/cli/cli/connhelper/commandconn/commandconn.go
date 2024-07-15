// Package commandconn provides a net.Conn implementation that can be used for
// proxying (or emulating) stream via a custom command.
//
// For example, to provide an http.Client that can connect to a Docker daemon
// running in a Docker container ("DIND"):
//
//	httpClient := &http.Client{
//		Transport: &http.Transport{
//			DialContext: func(ctx context.Context, _network, _addr string) (net.Conn, error) {
//				return commandconn.New(ctx, "docker", "exec", "-it", containerID, "docker", "system", "dial-stdio")
//			},
//		},
//	}
package commandconn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// New returns net.Conn
func New(_ context.Context, cmd string, args ...string) (net.Conn, error) {
	var (
		c   commandConn
		err error
	)
	c.cmd = exec.Command(cmd, args...)
	// we assume that args never contains sensitive information
	logrus.Debugf("commandconn: starting %s with %v", cmd, args)
	c.cmd.Env = os.Environ()
	c.cmd.SysProcAttr = &syscall.SysProcAttr{}
	setPdeathsig(c.cmd)
	createSession(c.cmd)
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c.cmd.Stderr = &stderrWriter{
		stderrMu:    &c.stderrMu,
		stderr:      &c.stderr,
		debugPrefix: fmt.Sprintf("commandconn (%s):", cmd),
	}
	c.localAddr = dummyAddr{network: "dummy", s: "dummy-0"}
	c.remoteAddr = dummyAddr{network: "dummy", s: "dummy-1"}
	return &c, c.cmd.Start()
}

// commandConn implements net.Conn
type commandConn struct {
	cmdMutex     sync.Mutex // for cmd, cmdWaitErr
	cmd          *exec.Cmd
	cmdWaitErr   error
	cmdExited    atomic.Bool
	stdin        io.WriteCloser
	stdout       io.ReadCloser
	stderrMu     sync.Mutex // for stderr
	stderr       bytes.Buffer
	stdinClosed  atomic.Bool
	stdoutClosed atomic.Bool
	closing      atomic.Bool
	localAddr    net.Addr
	remoteAddr   net.Addr
}

// kill terminates the process. On Windows it kills the process directly,
// whereas on other platforms, a SIGTERM is sent, before forcefully terminating
// the process after 3 seconds.
func (c *commandConn) kill() {
	if c.cmdExited.Load() {
		return
	}
	c.cmdMutex.Lock()
	var werr error
	if runtime.GOOS != "windows" {
		werrCh := make(chan error)
		go func() { werrCh <- c.cmd.Wait() }()
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case werr = <-werrCh:
		case <-time.After(3 * time.Second):
			_ = c.cmd.Process.Kill()
			werr = <-werrCh
		}
	} else {
		_ = c.cmd.Process.Kill()
		werr = c.cmd.Wait()
	}
	c.cmdWaitErr = werr
	c.cmdMutex.Unlock()
	c.cmdExited.Store(true)
}

// handleEOF handles io.EOF errors while reading or writing from the underlying
// command pipes.
//
// When we've received an EOF we expect that the command will
// be terminated soon. As such, we call Wait() on the command
// and return EOF or the error depending on whether the command
// exited with an error.
//
// If Wait() does not return within 10s, an error is returned
func (c *commandConn) handleEOF(err error) error {
	if err != io.EOF {
		return err
	}

	c.cmdMutex.Lock()
	defer c.cmdMutex.Unlock()

	var werr error
	if c.cmdExited.Load() {
		werr = c.cmdWaitErr
	} else {
		werrCh := make(chan error)
		go func() { werrCh <- c.cmd.Wait() }()
		select {
		case werr = <-werrCh:
			c.cmdWaitErr = werr
			c.cmdExited.Store(true)
		case <-time.After(10 * time.Second):
			c.stderrMu.Lock()
			stderr := c.stderr.String()
			c.stderrMu.Unlock()
			return errors.Errorf("command %v did not exit after %v: stderr=%q", c.cmd.Args, err, stderr)
		}
	}

	if werr == nil {
		return err
	}
	c.stderrMu.Lock()
	stderr := c.stderr.String()
	c.stderrMu.Unlock()
	return errors.Errorf("command %v has exited with %v, make sure the URL is valid, and Docker 18.09 or later is installed on the remote host: stderr=%s", c.cmd.Args, werr, stderr)
}

func ignorableCloseError(err error) bool {
	return strings.Contains(err.Error(), os.ErrClosed.Error())
}

func (c *commandConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	// check after the call to Read, since
	// it is blocking, and while waiting on it
	// Close might get called
	if c.closing.Load() {
		// If we're currently closing the connection
		// we don't want to call onEOF
		return n, err
	}

	return n, c.handleEOF(err)
}

func (c *commandConn) Write(p []byte) (int, error) {
	n, err := c.stdin.Write(p)
	// check after the call to Write, since
	// it is blocking, and while waiting on it
	// Close might get called
	if c.closing.Load() {
		// If we're currently closing the connection
		// we don't want to call onEOF
		return n, err
	}

	return n, c.handleEOF(err)
}

// CloseRead allows commandConn to implement halfCloser
func (c *commandConn) CloseRead() error {
	// NOTE: maybe already closed here
	if err := c.stdout.Close(); err != nil && !ignorableCloseError(err) {
		return err
	}
	c.stdoutClosed.Store(true)

	if c.stdinClosed.Load() {
		c.kill()
	}

	return nil
}

// CloseWrite allows commandConn to implement halfCloser
func (c *commandConn) CloseWrite() error {
	// NOTE: maybe already closed here
	if err := c.stdin.Close(); err != nil && !ignorableCloseError(err) {
		return err
	}
	c.stdinClosed.Store(true)

	if c.stdoutClosed.Load() {
		c.kill()
	}
	return nil
}

// Close is the net.Conn func that gets called
// by the transport when a dial is cancelled
// due to it's context timing out. Any blocked
// Read or Write calls will be unblocked and
// return errors. It will block until the underlying
// command has terminated.
func (c *commandConn) Close() error {
	c.closing.Store(true)
	defer c.closing.Store(false)

	if err := c.CloseRead(); err != nil {
		logrus.Warnf("commandConn.Close: CloseRead: %v", err)
		return err
	}
	if err := c.CloseWrite(); err != nil {
		logrus.Warnf("commandConn.Close: CloseWrite: %v", err)
		return err
	}

	return nil
}

func (c *commandConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *commandConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *commandConn) SetDeadline(t time.Time) error {
	logrus.Debugf("unimplemented call: SetDeadline(%v)", t)
	return nil
}

func (c *commandConn) SetReadDeadline(t time.Time) error {
	logrus.Debugf("unimplemented call: SetReadDeadline(%v)", t)
	return nil
}

func (c *commandConn) SetWriteDeadline(t time.Time) error {
	logrus.Debugf("unimplemented call: SetWriteDeadline(%v)", t)
	return nil
}

type dummyAddr struct {
	network string
	s       string
}

func (d dummyAddr) Network() string {
	return d.network
}

func (d dummyAddr) String() string {
	return d.s
}

type stderrWriter struct {
	stderrMu    *sync.Mutex
	stderr      *bytes.Buffer
	debugPrefix string
}

func (w *stderrWriter) Write(p []byte) (int, error) {
	logrus.Debugf("%s%s", w.debugPrefix, string(p))
	w.stderrMu.Lock()
	if w.stderr.Len() > 4096 {
		w.stderr.Reset()
	}
	n, err := w.stderr.Write(p)
	w.stderrMu.Unlock()
	return n, err
}
