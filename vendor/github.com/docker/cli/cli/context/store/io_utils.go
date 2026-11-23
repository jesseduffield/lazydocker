package store

import (
	"errors"
	"io"
)

// limitedReader is a fork of [io.LimitedReader] to override Read.
type limitedReader struct {
	R io.Reader
	N int64 // max bytes remaining
}

// Read is a fork of [io.LimitedReader.Read] that returns an error when limit exceeded.
func (l *limitedReader) Read(p []byte) (n int, err error) {
	if l.N < 0 {
		return 0, errors.New("read exceeds the defined limit")
	}
	if l.N == 0 {
		return 0, io.EOF
	}
	// have to cap N + 1 otherwise we won't hit limit err
	if int64(len(p)) > l.N+1 {
		p = p[0 : l.N+1]
	}
	n, err = l.R.Read(p)
	l.N -= int64(n)
	return n, err
}
