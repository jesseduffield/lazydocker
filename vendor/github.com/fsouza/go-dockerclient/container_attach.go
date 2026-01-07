package docker

import (
	"io"
	"net/http"
)

// AttachToContainerOptions is the set of options that can be used when
// attaching to a container.
//
// See https://goo.gl/JF10Zk for more details.
type AttachToContainerOptions struct {
	Container    string    `qs:"-"`
	InputStream  io.Reader `qs:"-"`
	OutputStream io.Writer `qs:"-"`
	ErrorStream  io.Writer `qs:"-"`

	// If set, after a successful connect, a sentinel will be sent and then the
	// client will block on receive before continuing.
	//
	// It must be an unbuffered channel. Using a buffered channel can lead
	// to unexpected behavior.
	Success chan struct{}

	// Override the key sequence for detaching a container.
	DetachKeys string

	// Use raw terminal? Usually true when the container contains a TTY.
	RawTerminal bool `qs:"-"`

	// Get container logs, sending it to OutputStream.
	Logs bool

	// Stream the response?
	Stream bool

	// Attach to stdin, and use InputStream.
	Stdin bool

	// Attach to stdout, and use OutputStream.
	Stdout bool

	// Attach to stderr, and use ErrorStream.
	Stderr bool
}

// AttachToContainer attaches to a container, using the given options.
//
// See https://goo.gl/JF10Zk for more details.
func (c *Client) AttachToContainer(opts AttachToContainerOptions) error {
	cw, err := c.AttachToContainerNonBlocking(opts)
	if err != nil {
		return err
	}
	return cw.Wait()
}

// AttachToContainerNonBlocking attaches to a container, using the given options.
// This function does not block.
//
// See https://goo.gl/NKpkFk for more details.
func (c *Client) AttachToContainerNonBlocking(opts AttachToContainerOptions) (CloseWaiter, error) {
	if opts.Container == "" {
		return nil, &NoSuchContainer{ID: opts.Container}
	}
	path := "/containers/" + opts.Container + "/attach?" + queryString(opts)
	return c.hijack(http.MethodPost, path, hijackOptions{
		success:        opts.Success,
		setRawTerminal: opts.RawTerminal,
		in:             opts.InputStream,
		stdout:         opts.OutputStream,
		stderr:         opts.ErrorStream,
	})
}
