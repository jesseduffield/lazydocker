// Copyright 2014-2022 Ulrich Kunitz. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lzma

import (
	"errors"
	"fmt"
)

// uint32LE reads an uint32 integer from a byte slice
func uint32LE(b []byte) uint32 {
	x := uint32(b[3]) << 24
	x |= uint32(b[2]) << 16
	x |= uint32(b[1]) << 8
	x |= uint32(b[0])
	return x
}

// uint64LE converts the uint64 value stored as little endian to an uint64
// value.
func uint64LE(b []byte) uint64 {
	x := uint64(b[7]) << 56
	x |= uint64(b[6]) << 48
	x |= uint64(b[5]) << 40
	x |= uint64(b[4]) << 32
	x |= uint64(b[3]) << 24
	x |= uint64(b[2]) << 16
	x |= uint64(b[1]) << 8
	x |= uint64(b[0])
	return x
}

// putUint32LE puts an uint32 integer into a byte slice that must have at least
// a length of 4 bytes.
func putUint32LE(b []byte, x uint32) {
	b[0] = byte(x)
	b[1] = byte(x >> 8)
	b[2] = byte(x >> 16)
	b[3] = byte(x >> 24)
}

// putUint64LE puts the uint64 value into the byte slice as little endian
// value. The byte slice b must have at least place for 8 bytes.
func putUint64LE(b []byte, x uint64) {
	b[0] = byte(x)
	b[1] = byte(x >> 8)
	b[2] = byte(x >> 16)
	b[3] = byte(x >> 24)
	b[4] = byte(x >> 32)
	b[5] = byte(x >> 40)
	b[6] = byte(x >> 48)
	b[7] = byte(x >> 56)
}

// noHeaderSize defines the value of the length field in the LZMA header.
const noHeaderSize uint64 = 1<<64 - 1

// HeaderLen provides the length of the LZMA file header.
const HeaderLen = 13

// Header represents the Header of an LZMA file.
type Header struct {
	Properties Properties
	DictSize   uint32
	// uncompressed Size; negative value if no Size is given
	Size int64
}

// marshalBinary marshals the header.
func (h *Header) marshalBinary() (data []byte, err error) {
	if err = h.Properties.verify(); err != nil {
		return nil, err
	}
	if !(h.DictSize <= MaxDictCap) {
		return nil, fmt.Errorf("lzma: DictCap %d out of range",
			h.DictSize)
	}

	data = make([]byte, 13)

	// property byte
	data[0] = h.Properties.Code()

	// dictionary capacity
	putUint32LE(data[1:5], uint32(h.DictSize))

	// uncompressed size
	var s uint64
	if h.Size > 0 {
		s = uint64(h.Size)
	} else {
		s = noHeaderSize
	}
	putUint64LE(data[5:], s)

	return data, nil
}

// unmarshalBinary unmarshals the header.
func (h *Header) unmarshalBinary(data []byte) error {
	if len(data) != HeaderLen {
		return errors.New("lzma.unmarshalBinary: data has wrong length")
	}

	// properties
	var err error
	if h.Properties, err = PropertiesForCode(data[0]); err != nil {
		return err
	}

	// dictionary capacity
	h.DictSize = uint32LE(data[1:])
	if int(h.DictSize) < 0 {
		return errors.New(
			"LZMA header: dictionary capacity exceeds maximum " +
				"integer")
	}

	// uncompressed size
	s := uint64LE(data[5:])
	if s == noHeaderSize {
		h.Size = -1
	} else {
		h.Size = int64(s)
		if h.Size < 0 {
			return errors.New(
				"LZMA header: uncompressed size " +
					"out of int64 range")
		}
	}

	return nil
}

// validDictSize checks whether the dictionary capacity is correct. This
// is used to weed out wrong file headers.
func validDictSize(dictcap int) bool {
	if int64(dictcap) == MaxDictCap {
		return true
	}
	for n := uint(10); n < 32; n++ {
		if dictcap == 1<<n {
			return true
		}
		if dictcap == 1<<n+1<<(n-1) {
			return true
		}
	}
	return false
}

// ValidHeader checks for a valid LZMA file header. It allows only
// dictionary sizes of 2^n or 2^n+2^(n-1) with n >= 10 or 2^32-1. If
// there is an explicit size it must not exceed 256 GiB. The length of
// the data argument must be HeaderLen.
//
// This function should be disregarded because there is no guarantee that LZMA
// files follow the constraints.
func ValidHeader(data []byte) bool {
	var h Header
	if err := h.unmarshalBinary(data); err != nil {
		return false
	}
	if !validDictSize(int(h.DictSize)) {
		return false
	}
	return h.Size < 0 || h.Size <= 1<<38
}
