/*
Copyright 2011 The Perkeep Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package rollsum implements rolling checksums similar to apenwarr's bup, which
// is similar to librsync.
//
// The bup project is at https://github.com/apenwarr/bup and its splitting in
// particular is at https://github.com/apenwarr/bup/blob/master/lib/bup/bupsplit.c
package compressor

import (
	"math/bits"
)

const (
	windowSize = 64 // Roll assumes windowSize is a power of 2
	charOffset = 31
)

const (
	blobBits = 13
	blobSize = 1 << blobBits // 8k
)

type RollSum struct {
	s1, s2 uint32
	window [windowSize]uint8
	wofs   int
}

func NewRollSum() *RollSum {
	return &RollSum{
		s1: windowSize * charOffset,
		s2: windowSize * (windowSize - 1) * charOffset,
	}
}

func (rs *RollSum) add(drop, add uint32) {
	s1 := rs.s1 + add - drop
	rs.s1 = s1
	rs.s2 += s1 - uint32(windowSize)*(drop+charOffset)
}

// Roll adds ch to the rolling sum.
func (rs *RollSum) Roll(ch byte) {
	wp := &rs.window[rs.wofs]
	rs.add(uint32(*wp), uint32(ch))
	*wp = ch
	rs.wofs = (rs.wofs + 1) & (windowSize - 1)
}

// OnSplit reports whether at least 13 consecutive trailing bits of
// the current checksum are set the same way.
func (rs *RollSum) OnSplit() bool {
	return (rs.s2 & (blobSize - 1)) == ((^0) & (blobSize - 1))
}

// OnSplitWithBits reports whether at least n consecutive trailing bits
// of the current checksum are set the same way.
func (rs *RollSum) OnSplitWithBits(n uint32) bool {
	mask := (uint32(1) << n) - 1
	return rs.s2&mask == (^uint32(0))&mask
}

func (rs *RollSum) Bits() int {
	rsum := rs.Digest() >> (blobBits + 1)
	return blobBits + bits.TrailingZeros32(^rsum)
}

func (rs *RollSum) Digest() uint32 {
	return (rs.s1 << 16) | (rs.s2 & 0xffff)
}
