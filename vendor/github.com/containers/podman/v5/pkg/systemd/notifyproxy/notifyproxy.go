//go:build !windows

package notifyproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	// All constants below are defined by systemd.
	_notifyRcvbufSize = 8 * 1024 * 1024
	_notifyBufferMax  = 4096
	_notifyFdMax      = 768
	_notifyBarrierMsg = "BARRIER=1"
	_notifyRdyMsg     = daemon.SdNotifyReady
)

// SendMessage sends the specified message to the specified socket.
// No message is sent if no socketPath is provided and the NOTIFY_SOCKET
// variable is not set either.
func SendMessage(socketPath string, message string) error {
	if socketPath == "" {
		socketPath, _ = os.LookupEnv("NOTIFY_SOCKET")
		if socketPath == "" {
			return nil
		}
	}
	socketAddr := &net.UnixAddr{
		Name: socketPath,
		Net:  "unixgram",
	}
	conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(message))
	return err
}

// NotifyProxy can be used to proxy notify messages.
type NotifyProxy struct {
	connection *net.UnixConn
	socketPath string
	container  Container // optional

	// Channels for synchronizing the goroutine waiting for the READY
	// message and the one checking if the optional container is still
	// running.
	errorChan chan error
	readyChan chan bool
}

// New creates a NotifyProxy that starts listening immediately.  The specified
// temp directory can be left empty.
func New(tmpDir string) (*NotifyProxy, error) {
	tempFile, err := os.CreateTemp(tmpDir, "-podman-notify-proxy.sock")
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()

	socketPath := tempFile.Name()
	if err := syscall.Unlink(socketPath); err != nil { // Unlink the socket so we can bind it
		return nil, err
	}

	socketAddr := &net.UnixAddr{
		Name: socketPath,
		Net:  "unixgram",
	}
	conn, err := net.ListenUnixgram(socketAddr.Net, socketAddr)
	if err != nil {
		return nil, err
	}

	if err := conn.SetReadBuffer(_notifyRcvbufSize); err != nil {
		return nil, fmt.Errorf("setting read buffer: %w", err)
	}

	errorChan := make(chan error, 1)
	readyChan := make(chan bool, 1)

	proxy := &NotifyProxy{
		connection: conn,
		socketPath: socketPath,
		errorChan:  errorChan,
		readyChan:  readyChan,
	}

	// Start waiting for the READY message in the background.  This way,
	// the proxy can be created prior to starting the container and
	// circumvents a race condition on writing/reading on the socket.
	proxy.listen()

	return proxy, nil
}

// listen waits for the READY message in the background, and process file
// descriptors and barriers send over the NOTIFY_SOCKET. The goroutine returns
// when the socket is closed.
func (p *NotifyProxy) listen() {
	go func() {
		// See https://github.com/containers/podman/issues/16515 for a description of the protocol.
		fdSize := unix.CmsgSpace(4)
		buffer := make([]byte, _notifyBufferMax)
		oob := make([]byte, _notifyFdMax*fdSize)
		sBuilder := strings.Builder{}
		for {
			n, oobn, flags, _, err := p.connection.ReadMsgUnix(buffer, oob)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					p.errorChan <- err
					return
				}
				logrus.Errorf("Error reading unix message on socket %q: %v", p.socketPath, err)
				continue
			}

			if n > _notifyBufferMax || oobn > _notifyFdMax*fdSize {
				logrus.Errorf("Ignoring unix message on socket %q: incorrect number of bytes read (n=%d, oobn=%d)", p.socketPath, n, oobn)
				continue
			}

			if flags&unix.MSG_CTRUNC != 0 {
				logrus.Errorf("Ignoring unix message on socket %q: message truncated", p.socketPath)
				continue
			}

			sBuilder.Reset()
			sBuilder.Write(buffer[:n])
			var isBarrier, isReady bool

			for line := range strings.SplitSeq(sBuilder.String(), "\n") {
				switch line {
				case _notifyRdyMsg:
					isReady = true
				case _notifyBarrierMsg:
					isBarrier = true
				}
			}

			if isBarrier {
				scms, err := unix.ParseSocketControlMessage(oob)
				if err != nil {
					logrus.Errorf("parsing control message on socket %q: %v", p.socketPath, err)
				}
				for _, scm := range scms {
					fds, err := unix.ParseUnixRights(&scm)
					if err != nil {
						logrus.Errorf("parsing unix rights of control message on socket %q: %v", p.socketPath, err)
						continue
					}
					for _, fd := range fds {
						if err := unix.Close(fd); err != nil {
							logrus.Errorf("closing fd passed on socket %q: %v", fd, err)
							continue
						}
					}
				}
				continue
			}

			if isReady {
				p.readyChan <- true
			}
		}
	}()
}

// SocketPath returns the path of the socket the proxy is listening on.
func (p *NotifyProxy) SocketPath() string {
	return p.socketPath
}

// Close closes the listener and removes the socket.
func (p *NotifyProxy) Close() error {
	defer os.Remove(p.socketPath)
	return p.connection.Close()
}

// AddContainer associates a container with the proxy.
func (p *NotifyProxy) AddContainer(container Container) {
	p.container = container
}

// ErrNoReadyMessage is returned when we are waiting for the READY message of a
// container that is not in the running state anymore.
var ErrNoReadyMessage = errors.New("container stopped running before READY message was received")

// Container avoids a circular dependency among this package and libpod.
type Container interface {
	State() (define.ContainerStatus, error)
	ID() string
}

// Wait waits until receiving the `READY` notify message. Note that the
// this function must only be executed inside a systemd service which will kill
// the process after a given timeout. If the (optional) container stopped
// running before the `READY` is received, the waiting gets canceled and
// ErrNoReadyMessage is returned.
func (p *NotifyProxy) Wait() error {
	// If the proxy has a container we need to watch it as it may exit
	// without sending a READY message. The goroutine below returns when
	// the container exits OR when the function returns (see deferred the
	// cancel()) in which case we either we've either received the READY
	// message or encountered an error reading from the socket.
	if p.container != nil {
		// Create a cancellable context to make sure the goroutine
		// below terminates on function return.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
					state, err := p.container.State()
					if err != nil {
						p.errorChan <- err
						return
					}
					if state != define.ContainerStateRunning {
						p.errorChan <- fmt.Errorf("%w: %s", ErrNoReadyMessage, p.container.ID())
						return
					}
				}
			}
		}()
	}

	// Wait for the ready/error channel.
	select {
	case <-p.readyChan:
		return nil
	case err := <-p.errorChan:
		return err
	}
}
