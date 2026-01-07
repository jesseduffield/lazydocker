// Copyright (c) 2021-2022, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"errors"
	"io"
)

// A Buffer is a variable-sized buffer of bytes that implements the sif.ReadWriter interface. The
// zero value for Buffer is an empty buffer ready to use.
type Buffer struct {
	buf []byte
	pos int64
}

// NewBuffer creates and initializes a new Buffer using buf as its initial contents.
func NewBuffer(buf []byte) *Buffer {
	return &Buffer{buf: buf}
}

var errNegativeOffset = errors.New("negative offset")

// ReadAt implements the io.ReaderAt interface.
func (b *Buffer) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errNegativeOffset
	}

	if off >= int64(len(b.buf)) {
		return 0, io.EOF
	}

	n := copy(p, b.buf[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

var errNegativePosition = errors.New("negative position")

// Write implements the io.Writer interface.
func (b *Buffer) Write(p []byte) (int, error) {
	if b.pos < 0 {
		return 0, errNegativePosition
	}

	if have, need := int64(len(b.buf))-b.pos, int64(len(p)); have < need {
		b.buf = append(b.buf, make([]byte, need-have)...)
	}

	n := copy(b.buf[b.pos:], p)
	b.pos += int64(n)
	return n, nil
}

var errInvalidWhence = errors.New("invalid whence")

// Seek implements the io.Seeker interface.
func (b *Buffer) Seek(offset int64, whence int) (int64, error) {
	var abs int64

	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = b.pos + offset
	case io.SeekEnd:
		abs = int64(len(b.buf)) + offset
	default:
		return 0, errInvalidWhence
	}

	if abs < 0 {
		return 0, errNegativePosition
	}

	b.pos = abs
	return abs, nil
}

var errTruncateRange = errors.New("truncation out of range")

// Truncate discards all but the first n bytes from the buffer.
func (b *Buffer) Truncate(n int64) error {
	if n < 0 || n > int64(len(b.buf)) {
		return errTruncateRange
	}

	b.buf = b.buf[:n]
	return nil
}

// Bytes returns the contents of the buffer. The slice is valid for use only until the next buffer
// modification (that is, only until the next call to a method like ReadAt, Write, or Truncate).
func (b *Buffer) Bytes() []byte { return b.buf }

// Len returns the number of bytes in the buffer.
func (b *Buffer) Len() int64 { return int64(len(b.buf)) }
