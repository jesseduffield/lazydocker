// Copyright 2014-2022 Ulrich Kunitz. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package lzma supports the decoding and encoding of LZMA streams.
// Reader and Writer support the classic LZMA format. Reader2 and
// Writer2 support the decoding and encoding of LZMA2 streams.
//
// The package is written completely in Go and does not rely on any external
// library.
package lzma

import (
	"errors"
	"fmt"
	"io"
)

// ReaderConfig stores the parameters for the reader of the classic LZMA
// format.
type ReaderConfig struct {
	// Since v0.5.14 this parameter sets an upper limit for a .lzma file's
	// dictionary size. This helps to mitigate problems with mangled
	// headers.
	DictCap int
}

// fill converts the zero values of the configuration to the default values.
func (c *ReaderConfig) fill() {
	if c.DictCap == 0 {
		// set an upper limit of 2 GiB-1 for dictionary capacity
		// to address the zero prefix security issue.
		c.DictCap = (1 << 31) - 1
		// original: c.DictCap = 8 * 1024 * 1024
	}
}

// Verify checks the reader configuration for errors. Zero values will
// be replaced by default values.
func (c *ReaderConfig) Verify() error {
	c.fill()
	if !(MinDictCap <= c.DictCap && int64(c.DictCap) <= MaxDictCap) {
		return errors.New("lzma: dictionary capacity is out of range")
	}
	return nil
}

// Reader provides a reader for LZMA files or streams.
//
// # Security concerns
//
// Note that LZMA format doesn't support a magic marker in the header. So
// [NewReader] cannot determine whether it reads the actual header. For instance
// the LZMA stream might have a zero byte in front of the reader, leading to
// larger dictionary sizes and file sizes. The code will detect later that there
// are problems with the stream, but the dictionary has already been allocated
// and this might consume a lot of memory.
//
// Version 0.5.14 introduces built-in mitigations:
//
//   - The [ReaderConfig] DictCap field is now interpreted as a limit for the
//     dictionary size.
//   - The default is 2 Gigabytes minus 1 byte (2^31-1 bytes).
//   - Users can check with the [Reader.Header] method what the actual values are in
//     their LZMA files and set a smaller limit using [ReaderConfig].
//   - The dictionary size doesn't exceed the larger of the file size and
//     the minimum dictionary size. This is another measure to prevent huge
//     memory allocations for the dictionary.
//   - The code supports stream sizes only up to a pebibyte (1024^5).
type Reader struct {
	lzma   io.Reader
	header Header
	// headerOrig stores the original header read from the stream.
	headerOrig Header
	d          *decoder
}

// NewReader creates a new reader for an LZMA stream using the classic
// format. NewReader reads and checks the header of the LZMA stream.
func NewReader(lzma io.Reader) (r *Reader, err error) {
	return ReaderConfig{}.NewReader(lzma)
}

// ErrDictSize reports about an error of the dictionary size.
type ErrDictSize struct {
	ConfigDictCap  int
	HeaderDictSize uint32
	Message        string
}

// Error returns the error message.
func (e *ErrDictSize) Error() string {
	return e.Message
}

func newErrDictSize(messageformat string,
	configDictCap int, headerDictSize uint32,
	args ...interface{}) *ErrDictSize {
	newArgs := make([]interface{}, len(args)+2)
	newArgs[0] = configDictCap
	newArgs[1] = headerDictSize
	copy(newArgs[2:], args)
	return &ErrDictSize{
		ConfigDictCap:  configDictCap,
		HeaderDictSize: headerDictSize,
		Message:        fmt.Sprintf(messageformat, newArgs...),
	}
}

// We support only files not larger than 1 << 50 bytes (a pebibyte, 1024^5).
const maxStreamSize = 1 << 50

// NewReader creates a new reader for an LZMA stream in the classic
// format. The function reads and verifies the header of the LZMA
// stream.
func (c ReaderConfig) NewReader(lzma io.Reader) (r *Reader, err error) {
	if err = c.Verify(); err != nil {
		return nil, err
	}
	data := make([]byte, HeaderLen)
	if _, err := io.ReadFull(lzma, data); err != nil {
		if err == io.EOF {
			return nil, errors.New("lzma: unexpected EOF")
		}
		return nil, err
	}
	r = &Reader{lzma: lzma}
	if err = r.header.unmarshalBinary(data); err != nil {
		return nil, err
	}
	r.headerOrig = r.header
	dictSize := int64(r.header.DictSize)
	if int64(c.DictCap) < dictSize {
		return nil, newErrDictSize(
			"lzma: header dictionary size %[2]d exceeds configured dictionary capacity %[1]d",
			c.DictCap, uint32(dictSize),
		)
	}
	if dictSize < MinDictCap {
		dictSize = MinDictCap
	}
	// original code: disabled this because there is no point in increasing
	// the dictionary above what is stated in the file.
	/*
		if int64(c.DictCap) > int64(dictSize) {
			dictSize = int64(c.DictCap)
		}
	*/
	size := r.header.Size
	if size >= 0 && size < dictSize {
		dictSize = size
	}
	// Protect against modified or malicious headers.
	if size > maxStreamSize {
		return nil, fmt.Errorf(
			"lzma: stream size %d exceeds a pebibyte (1024^5)",
			size)
	}
	if dictSize < MinDictCap {
		dictSize = MinDictCap
	}

	r.header.DictSize = uint32(dictSize)

	state := newState(r.header.Properties)
	dict, err := newDecoderDict(int(dictSize))
	if err != nil {
		return nil, err
	}
	r.d, err = newDecoder(ByteReader(lzma), state, dict, r.header.Size)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Header returns the header as read from the LZMA stream. It is intended to
// allow the user to understand what parameters are typically provided in the
// headers of the LZMA files and set the DictCap field in [ReaderConfig]
// accordingly.
func (r *Reader) Header() (h Header, ok bool) {
	return r.headerOrig, r.d != nil
}

// EOSMarker indicates that an EOS marker has been encountered.
func (r *Reader) EOSMarker() bool {
	return r.d.eosMarker
}

// Read returns uncompressed data.
func (r *Reader) Read(p []byte) (n int, err error) {
	return r.d.Read(p)
}
