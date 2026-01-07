package docker

import (
	"context"
	"io"
	"net/http"
	"time"
)

// LogsOptions represents the set of options used when getting logs from a
// container.
//
// See https://goo.gl/krK0ZH for more details.
type LogsOptions struct {
	Context           context.Context
	Container         string        `qs:"-"`
	OutputStream      io.Writer     `qs:"-"`
	ErrorStream       io.Writer     `qs:"-"`
	InactivityTimeout time.Duration `qs:"-"`
	Tail              string

	Since      int64
	Follow     bool
	Stdout     bool
	Stderr     bool
	Timestamps bool

	// Use raw terminal? Usually true when the container contains a TTY.
	RawTerminal bool `qs:"-"`
}

// Logs gets stdout and stderr logs from the specified container.
//
// When LogsOptions.RawTerminal is set to false, go-dockerclient will multiplex
// the streams and send the containers stdout to LogsOptions.OutputStream, and
// stderr to LogsOptions.ErrorStream.
//
// When LogsOptions.RawTerminal is true, callers will get the raw stream on
// LogsOptions.OutputStream. The caller can use libraries such as dlog
// (github.com/ahmetalpbalkan/dlog).
//
// See https://goo.gl/krK0ZH for more details.
func (c *Client) Logs(opts LogsOptions) error {
	if opts.Container == "" {
		return &NoSuchContainer{ID: opts.Container}
	}
	if opts.Tail == "" {
		opts.Tail = "all"
	}
	path := "/containers/" + opts.Container + "/logs?" + queryString(opts)
	return c.stream(http.MethodGet, path, streamOptions{
		setRawTerminal:    opts.RawTerminal,
		stdout:            opts.OutputStream,
		stderr:            opts.ErrorStream,
		inactivityTimeout: opts.InactivityTimeout,
		context:           opts.Context,
	})
}
