//go:build linux || darwin || freebsd || netbsd

package password

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	terminal "golang.org/x/term"
)

var ErrInterrupt = errors.New("interrupted")

// Read reads a password from the terminal without echo.
func Read(fd int) ([]byte, error) {
	// Store and restore the terminal status on interruptions to
	// avoid that the terminal remains in the password state
	// This is necessary as for https://github.com/golang/go/issues/31180

	oldState, err := terminal.GetState(fd)
	if err != nil {
		return make([]byte, 0), err
	}

	type Buffer struct {
		Buffer []byte
		Error  error
	}
	errorChannel := make(chan Buffer, 1)

	// SIGINT and SIGTERM restore the terminal, otherwise the no-echo mode would remain intact
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, syscall.SIGINT, syscall.SIGTERM)
	defer func() {
		signal.Stop(interruptChannel)
		close(interruptChannel)
	}()
	go func() {
		for range interruptChannel {
			if oldState != nil {
				_ = terminal.Restore(fd, oldState)
			}
			errorChannel <- Buffer{Buffer: make([]byte, 0), Error: ErrInterrupt}
		}
	}()

	go func() {
		buf, err := terminal.ReadPassword(fd)
		errorChannel <- Buffer{Buffer: buf, Error: err}
	}()

	buf := <-errorChannel
	return buf.Buffer, buf.Error
}
