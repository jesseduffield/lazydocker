//go:build !remote && (linux || freebsd)

package libpod

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/errorhandling"
	"github.com/moby/term"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/detach"
	"go.podman.io/common/pkg/resize"
	"golang.org/x/sys/unix"
)

/* Sync with stdpipe_t in conmon.c */
const (
	AttachPipeStdin  = 1
	AttachPipeStdout = 2
	AttachPipeStderr = 3
)

// Attach to the given container.
// Does not check if state is appropriate.
// started is only required if startContainer is true.
// It does not wait for the container to be healthy, it is the caller responsibility to do so.
func (r *ConmonOCIRuntime) Attach(c *Container, params *AttachOptions) error {
	passthrough := c.LogDriver() == define.PassthroughLogging || c.LogDriver() == define.PassthroughTTYLogging

	if params == nil || params.Streams == nil {
		return fmt.Errorf("must provide parameters to Attach: %w", define.ErrInternal)
	}

	if !params.Streams.AttachOutput && !params.Streams.AttachError && !params.Streams.AttachInput && !passthrough {
		return fmt.Errorf("must provide at least one stream to attach to: %w", define.ErrInvalidArg)
	}
	if params.Start && params.Started == nil {
		return fmt.Errorf("started chan not passed when startContainer set: %w", define.ErrInternal)
	}

	keys := config.DefaultDetachKeys
	if params.DetachKeys != nil {
		keys = *params.DetachKeys
	}

	detachKeys, err := processDetachKeys(keys)
	if err != nil {
		return err
	}

	var conn *net.UnixConn
	if !passthrough {
		logrus.Debugf("Attaching to container %s", c.ID())

		// If we have a resize, do it.
		if params.InitialSize != nil {
			if err := r.AttachResize(c, *params.InitialSize); err != nil {
				return err
			}
		}

		attachSock, err := c.AttachSocketPath()
		if err != nil {
			return err
		}

		conn, err = openUnixSocket(attachSock)
		if err != nil {
			return fmt.Errorf("failed to connect to container's attach socket: %v: %w", attachSock, err)
		}
		defer func() {
			if err := conn.Close(); err != nil {
				logrus.Errorf("unable to close socket: %q", err)
			}
		}()
	}

	// If starting was requested, start the container and notify when that's
	// done.
	if params.Start {
		if err := c.start(); err != nil {
			return err
		}
		params.Started <- true
	}
	close(params.Started)

	if passthrough {
		return nil
	}

	receiveStdoutError, stdinDone := setupStdioChannels(params.Streams, conn, detachKeys)
	if params.AttachReady != nil {
		params.AttachReady <- true
	}
	return readStdio(conn, params.Streams, receiveStdoutError, stdinDone)
}

// Attach to the given container's exec session.
//
// attachFd and startFd must be open file descriptors. attachFd must be the
// output side of the fd and is used for two things:
//
//  1. conmon will first send a nonce value across the pipe indicating it has
//     set up its side of the console socket this ensures attachToExec gets all of
//     the output of the called process.
//
//  2. conmon will then send the exit code of the exec process, or an error in the exec session.
//
// startFd must be the input side of the fd.
//
// newSize resizes the tty to this size before the process is started, must be
// nil if the exec session has no tty
//
// conmon will wait to start the exec session until the parent process has set up the console socket.
//
// Once attachToExec successfully attaches to the console socket, the child
// conmon process responsible for calling runtime exec will read from the
// output side of start fd, thus learning to start the child process.
//
// Thus, the order goes as follow:
// 1. conmon parent process sets up its console socket. sends on attachFd
// 2. attachToExec attaches to the console socket after reading on attachFd and resizes the tty
// 3. child waits on startFd for attachToExec to attach to said console socket
// 4. attachToExec sends on startFd, signalling it has attached to the socket and child is ready to go
// 5. child receives on startFd, runs the runtime exec command
// attachToExec is responsible for closing startFd and attachFd
func (c *Container) attachToExec(streams *define.AttachStreams, keys *string, sessionID string, startFd, attachFd *os.File, newSize *resize.TerminalSize) error {
	if !streams.AttachOutput && !streams.AttachError && !streams.AttachInput {
		return fmt.Errorf("must provide at least one stream to attach to: %w", define.ErrInvalidArg)
	}
	if startFd == nil || attachFd == nil {
		return fmt.Errorf("start sync pipe and attach sync pipe must be defined for exec attach: %w", define.ErrInvalidArg)
	}

	defer errorhandling.CloseQuiet(startFd)
	defer errorhandling.CloseQuiet(attachFd)

	detachString := config.DefaultDetachKeys
	if keys != nil {
		detachString = *keys
	}
	detachKeys, err := processDetachKeys(detachString)
	if err != nil {
		return err
	}

	logrus.Debugf("Attaching to container %s exec session %s", c.ID(), sessionID)

	// set up the socket path, such that it is the correct length and location for exec
	sockPath, err := c.execAttachSocketPath(sessionID)
	if err != nil {
		return err
	}

	// 2: read from attachFd that the parent process has set up the console socket
	if _, err := readConmonPipeData(c.ociRuntime.Name(), attachFd, ""); err != nil {
		return err
	}

	// resize before we start the container process
	if newSize != nil {
		err = c.ociRuntime.ExecAttachResize(c, sessionID, *newSize)
		if err != nil {
			logrus.Warnf("Resize failed: %v", err)
		}
	}

	// 2: then attach
	conn, err := openUnixSocket(sockPath)
	if err != nil {
		return fmt.Errorf("failed to connect to container's attach socket: %v: %w", sockPath, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logrus.Errorf("Unable to close socket: %q", err)
		}
	}()

	// start listening on stdio of the process
	receiveStdoutError, stdinDone := setupStdioChannels(streams, conn, detachKeys)

	// 4: send start message to child
	if err := writeConmonPipeData(startFd); err != nil {
		return err
	}

	return readStdio(conn, streams, receiveStdoutError, stdinDone)
}

func processDetachKeys(keys string) ([]byte, error) {
	// Check the validity of the provided keys first
	if len(keys) == 0 {
		return []byte{}, nil
	}
	detachKeys, err := term.ToBytes(keys)
	if err != nil {
		return nil, fmt.Errorf("invalid detach keys: %w", err)
	}
	return detachKeys, nil
}

func registerResizeFunc(r <-chan resize.TerminalSize, bundlePath string) {
	resize.HandleResizing(r, func(size resize.TerminalSize) {
		controlPath := filepath.Join(bundlePath, "ctl")
		controlFile, err := os.OpenFile(controlPath, unix.O_WRONLY, 0)
		if err != nil {
			logrus.Debugf("Could not open ctl file: %v", err)
			return
		}
		defer controlFile.Close()

		logrus.Debugf("Received a resize event: %+v", size)
		if _, err = fmt.Fprintf(controlFile, "%d %d %d\n", 1, size.Height, size.Width); err != nil {
			logrus.Warnf("Failed to write to control file to resize terminal: %v", err)
		}
	})
}

func setupStdioChannels(streams *define.AttachStreams, conn *net.UnixConn, detachKeys []byte) (chan error, chan error) {
	receiveStdoutError := make(chan error)
	go func() {
		receiveStdoutError <- redirectResponseToOutputStreams(streams.OutputStream, streams.ErrorStream, streams.AttachOutput, streams.AttachError, conn)
	}()

	stdinDone := make(chan error)
	go func() {
		var err error
		if streams.AttachInput {
			_, err = detach.Copy(conn, streams.InputStream, detachKeys)
		}
		stdinDone <- err
	}()

	return receiveStdoutError, stdinDone
}

func redirectResponseToOutputStreams(outputStream, errorStream io.Writer, writeOutput, writeError bool, conn io.Reader) error {
	var err error
	buf := make([]byte, 8192+1) /* Sync with conmon STDIO_BUF_SIZE */
	for {
		nr, er := conn.Read(buf)
		if nr > 0 {
			var dst io.Writer
			var doWrite bool
			switch buf[0] {
			case AttachPipeStdout:
				dst = outputStream
				doWrite = writeOutput
			case AttachPipeStderr:
				dst = errorStream
				doWrite = writeError
			default:
				logrus.Infof("Received unexpected attach type %+d", buf[0])
			}
			if dst == nil {
				return errors.New("output destination cannot be nil")
			}

			if doWrite {
				nw, ew := dst.Write(buf[1:nr])
				if ew != nil {
					err = ew
					break
				}
				if nr != nw+1 {
					err = io.ErrShortWrite
					break
				}
			}
		}
		if errors.Is(er, io.EOF) || errors.Is(er, syscall.ECONNRESET) {
			break
		}
		if er != nil {
			err = er
			break
		}
	}
	return err
}

func readStdio(conn *net.UnixConn, streams *define.AttachStreams, receiveStdoutError, stdinDone chan error) error {
	var err error
	select {
	case err = <-receiveStdoutError:
		if err := socketCloseWrite(conn); err != nil {
			logrus.Errorf("Failed to close stdin: %v", err)
		}
		return err
	case err = <-stdinDone:
		if err == define.ErrDetach {
			if err := socketCloseWrite(conn); err != nil {
				logrus.Errorf("Failed to close stdin: %v", err)
			}
			return err
		}
		if err == nil {
			// copy stdin is done, close it
			if connErr := socketCloseWrite(conn); connErr != nil {
				logrus.Errorf("Unable to close conn: %v", connErr)
			}
		}
		if streams.AttachOutput || streams.AttachError {
			return <-receiveStdoutError
		}
	}
	return nil
}
