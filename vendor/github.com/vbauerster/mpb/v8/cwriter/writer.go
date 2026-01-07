package cwriter

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
)

// https://github.com/dylanaraps/pure-sh-bible#cursor-movement
const (
	escOpen  = "\x1b["
	cuuAndEd = "A\x1b[J"
)

// ErrNotTTY not a TeleTYpewriter error.
var ErrNotTTY = errors.New("not a terminal")

// New returns a new Writer with defaults.
func New(out io.Writer) *Writer {
	w := &Writer{
		Buffer: new(bytes.Buffer),
		out:    out,
		termSize: func(_ int) (int, int, error) {
			return -1, -1, ErrNotTTY
		},
	}
	if f, ok := out.(*os.File); ok {
		w.fd = int(f.Fd())
		if IsTerminal(w.fd) {
			w.terminal = true
			w.termSize = func(fd int) (int, int, error) {
				return GetSize(fd)
			}
		}
	}
	bb := make([]byte, 16)
	w.ew = escWriter(bb[:copy(bb, []byte(escOpen))])
	return w
}

// IsTerminal tells whether underlying io.Writer is terminal.
func (w *Writer) IsTerminal() bool {
	return w.terminal
}

// GetTermSize returns WxH of underlying terminal.
func (w *Writer) GetTermSize() (width, height int, err error) {
	return w.termSize(w.fd)
}

type escWriter []byte

func (b escWriter) ansiCuuAndEd(out io.Writer, n int) error {
	b = strconv.AppendInt(b, int64(n), 10)
	_, err := out.Write(append(b, []byte(cuuAndEd)...))
	return err
}
