// Copyright (c) 2016 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

// Package serpent implements the Serpent block cipher
// submitted to the AES challenge. Serpent was designed by
// Ross Anderson, Eli Biham und Lars Knudsen.
// The block cipher takes a 128, 192 or 256 bit key and
// has a block size of 128 bit.
package serpent // import "github.com/aead/serpent"

import (
	"crypto/cipher"
	"errors"
)

// BlockSize is the serpent block size in bytes.
const BlockSize = 16

const phi = 0x9e3779b9 // The Serpent phi constant (sqrt(5) - 1) * 2**31

var errKeySize = errors.New("invalid key size")

// NewCipher returns a new cipher.Block implementing the serpent block cipher.
// The key argument must be 128, 192 or 256 bit (16, 24, 32 byte).
func NewCipher(key []byte) (cipher.Block, error) {
	if k := len(key); k != 16 && k != 24 && k != 32 {
		return nil, errKeySize
	}
	s := &subkeys{}
	s.keySchedule(key)
	return s, nil
}

// The 132 32 bit subkeys of serpent
type subkeys [132]uint32

func (s *subkeys) BlockSize() int { return BlockSize }

func (s *subkeys) Encrypt(dst, src []byte) {
	if len(src) < BlockSize {
		panic("src buffer to small")
	}
	if len(dst) < BlockSize {
		panic("dst buffer to small")
	}
	encryptBlock(dst, src, s)
}

func (s *subkeys) Decrypt(dst, src []byte) {
	if len(src) < BlockSize {
		panic("src buffer to small")
	}
	if len(dst) < BlockSize {
		panic("dst buffer to small")
	}
	decryptBlock(dst, src, s)
}

// The key schedule of serpent.
func (s *subkeys) keySchedule(key []byte) {
	var k [16]uint32
	j := 0
	for i := 0; i+4 <= len(key); i += 4 {
		k[j] = uint32(key[i]) | uint32(key[i+1])<<8 | uint32(key[i+2])<<16 | uint32(key[i+3])<<24
		j++
	}
	if j < 8 {
		k[j] = 1
	}

	for i := 8; i < 16; i++ {
		x := k[i-8] ^ k[i-5] ^ k[i-3] ^ k[i-1] ^ phi ^ uint32(i-8)
		k[i] = (x << 11) | (x >> 21)
		s[i-8] = k[i]
	}
	for i := 8; i < 132; i++ {
		x := s[i-8] ^ s[i-5] ^ s[i-3] ^ s[i-1] ^ phi ^ uint32(i)
		s[i] = (x << 11) | (x >> 21)
	}

	sb3(&s[0], &s[1], &s[2], &s[3])
	sb2(&s[4], &s[5], &s[6], &s[7])
	sb1(&s[8], &s[9], &s[10], &s[11])
	sb0(&s[12], &s[13], &s[14], &s[15])
	sb7(&s[16], &s[17], &s[18], &s[19])
	sb6(&s[20], &s[21], &s[22], &s[23])
	sb5(&s[24], &s[25], &s[26], &s[27])
	sb4(&s[28], &s[29], &s[30], &s[31])

	sb3(&s[32], &s[33], &s[34], &s[35])
	sb2(&s[36], &s[37], &s[38], &s[39])
	sb1(&s[40], &s[41], &s[42], &s[43])
	sb0(&s[44], &s[45], &s[46], &s[47])
	sb7(&s[48], &s[49], &s[50], &s[51])
	sb6(&s[52], &s[53], &s[54], &s[55])
	sb5(&s[56], &s[57], &s[58], &s[59])
	sb4(&s[60], &s[61], &s[62], &s[63])

	sb3(&s[64], &s[65], &s[66], &s[67])
	sb2(&s[68], &s[69], &s[70], &s[71])
	sb1(&s[72], &s[73], &s[74], &s[75])
	sb0(&s[76], &s[77], &s[78], &s[79])
	sb7(&s[80], &s[81], &s[82], &s[83])
	sb6(&s[84], &s[85], &s[86], &s[87])
	sb5(&s[88], &s[89], &s[90], &s[91])
	sb4(&s[92], &s[93], &s[94], &s[95])

	sb3(&s[96], &s[97], &s[98], &s[99])
	sb2(&s[100], &s[101], &s[102], &s[103])
	sb1(&s[104], &s[105], &s[106], &s[107])
	sb0(&s[108], &s[109], &s[110], &s[111])
	sb7(&s[112], &s[113], &s[114], &s[115])
	sb6(&s[116], &s[117], &s[118], &s[119])
	sb5(&s[120], &s[121], &s[122], &s[123])
	sb4(&s[124], &s[125], &s[126], &s[127])

	sb3(&s[128], &s[129], &s[130], &s[131])
}
