// Copyright (c) 2016 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package serpent

// Encrypts one block with the given 132 sub-keys sk.
func encryptBlock(dst, src []byte, sk *subkeys) {
	// Transform the input block to 4 x 32 bit registers
	r0 := uint32(src[0]) | uint32(src[1])<<8 | uint32(src[2])<<16 | uint32(src[3])<<24
	r1 := uint32(src[4]) | uint32(src[5])<<8 | uint32(src[6])<<16 | uint32(src[7])<<24
	r2 := uint32(src[8]) | uint32(src[9])<<8 | uint32(src[10])<<16 | uint32(src[11])<<24
	r3 := uint32(src[12]) | uint32(src[13])<<8 | uint32(src[14])<<16 | uint32(src[15])<<24

	// Encrypt the block with the 132 sub-keys and 8 S-Boxes
	r0, r1, r2, r3 = r0^sk[0], r1^sk[1], r2^sk[2], r3^sk[3]
	sb0(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[4], r1^sk[5], r2^sk[6], r3^sk[7]
	sb1(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[8], r1^sk[9], r2^sk[10], r3^sk[11]
	sb2(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[12], r1^sk[13], r2^sk[14], r3^sk[15]
	sb3(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[16], r1^sk[17], r2^sk[18], r3^sk[19]
	sb4(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[20], r1^sk[21], r2^sk[22], r3^sk[23]
	sb5(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[24], r1^sk[25], r2^sk[26], r3^sk[27]
	sb6(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[28], r1^sk[29], r2^sk[30], r3^sk[31]
	sb7(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)

	r0, r1, r2, r3 = r0^sk[32], r1^sk[33], r2^sk[34], r3^sk[35]
	sb0(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[36], r1^sk[37], r2^sk[38], r3^sk[39]
	sb1(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[40], r1^sk[41], r2^sk[42], r3^sk[43]
	sb2(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[44], r1^sk[45], r2^sk[46], r3^sk[47]
	sb3(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[48], r1^sk[49], r2^sk[50], r3^sk[51]
	sb4(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[52], r1^sk[53], r2^sk[54], r3^sk[55]
	sb5(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[56], r1^sk[57], r2^sk[58], r3^sk[59]
	sb6(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[60], r1^sk[61], r2^sk[62], r3^sk[63]
	sb7(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)

	r0, r1, r2, r3 = r0^sk[64], r1^sk[65], r2^sk[66], r3^sk[67]
	sb0(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[68], r1^sk[69], r2^sk[70], r3^sk[71]
	sb1(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[72], r1^sk[73], r2^sk[74], r3^sk[75]
	sb2(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[76], r1^sk[77], r2^sk[78], r3^sk[79]
	sb3(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[80], r1^sk[81], r2^sk[82], r3^sk[83]
	sb4(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[84], r1^sk[85], r2^sk[86], r3^sk[87]
	sb5(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[88], r1^sk[89], r2^sk[90], r3^sk[91]
	sb6(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[92], r1^sk[93], r2^sk[94], r3^sk[95]
	sb7(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)

	r0, r1, r2, r3 = r0^sk[96], r1^sk[97], r2^sk[98], r3^sk[99]
	sb0(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[100], r1^sk[101], r2^sk[102], r3^sk[103]
	sb1(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[104], r1^sk[105], r2^sk[106], r3^sk[107]
	sb2(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[108], r1^sk[109], r2^sk[110], r3^sk[111]
	sb3(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[112], r1^sk[113], r2^sk[114], r3^sk[115]
	sb4(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[116], r1^sk[117], r2^sk[118], r3^sk[119]
	sb5(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[120], r1^sk[121], r2^sk[122], r3^sk[123]
	sb6(&r0, &r1, &r2, &r3)
	linear(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[124], r1^sk[125], r2^sk[126], r3^sk[127]
	sb7(&r0, &r1, &r2, &r3)

	// whitening
	r0 ^= sk[128]
	r1 ^= sk[129]
	r2 ^= sk[130]
	r3 ^= sk[131]

	// write the encrypted block to the output

	dst[0] = byte(r0)
	dst[1] = byte(r0 >> 8)
	dst[2] = byte(r0 >> 16)
	dst[3] = byte(r0 >> 24)
	dst[4] = byte(r1)
	dst[5] = byte(r1 >> 8)
	dst[6] = byte(r1 >> 16)
	dst[7] = byte(r1 >> 24)
	dst[8] = byte(r2)
	dst[9] = byte(r2 >> 8)
	dst[10] = byte(r2 >> 16)
	dst[11] = byte(r2 >> 24)
	dst[12] = byte(r3)
	dst[13] = byte(r3 >> 8)
	dst[14] = byte(r3 >> 16)
	dst[15] = byte(r3 >> 24)
}

// Decrypts one block with the given 132 sub-keys sk.
func decryptBlock(dst, src []byte, sk *subkeys) {
	// Transform the input block to 4 x 32 bit registers
	r0 := uint32(src[0]) | uint32(src[1])<<8 | uint32(src[2])<<16 | uint32(src[3])<<24
	r1 := uint32(src[4]) | uint32(src[5])<<8 | uint32(src[6])<<16 | uint32(src[7])<<24
	r2 := uint32(src[8]) | uint32(src[9])<<8 | uint32(src[10])<<16 | uint32(src[11])<<24
	r3 := uint32(src[12]) | uint32(src[13])<<8 | uint32(src[14])<<16 | uint32(src[15])<<24

	// undo whitening
	r0 ^= sk[128]
	r1 ^= sk[129]
	r2 ^= sk[130]
	r3 ^= sk[131]

	// Decrypt the block with the 132 sub-keys and 8 S-Boxes
	sb7Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[124], r1^sk[125], r2^sk[126], r3^sk[127]
	linearInv(&r0, &r1, &r2, &r3)
	sb6Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[120], r1^sk[121], r2^sk[122], r3^sk[123]
	linearInv(&r0, &r1, &r2, &r3)
	sb5Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[116], r1^sk[117], r2^sk[118], r3^sk[119]
	linearInv(&r0, &r1, &r2, &r3)
	sb4Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[112], r1^sk[113], r2^sk[114], r3^sk[115]
	linearInv(&r0, &r1, &r2, &r3)
	sb3Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[108], r1^sk[109], r2^sk[110], r3^sk[111]
	linearInv(&r0, &r1, &r2, &r3)
	sb2Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[104], r1^sk[105], r2^sk[106], r3^sk[107]
	linearInv(&r0, &r1, &r2, &r3)
	sb1Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[100], r1^sk[101], r2^sk[102], r3^sk[103]
	linearInv(&r0, &r1, &r2, &r3)
	sb0Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[96], r1^sk[97], r2^sk[98], r3^sk[99]
	linearInv(&r0, &r1, &r2, &r3)

	sb7Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[92], r1^sk[93], r2^sk[94], r3^sk[95]
	linearInv(&r0, &r1, &r2, &r3)
	sb6Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[88], r1^sk[89], r2^sk[90], r3^sk[91]
	linearInv(&r0, &r1, &r2, &r3)
	sb5Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[84], r1^sk[85], r2^sk[86], r3^sk[87]
	linearInv(&r0, &r1, &r2, &r3)
	sb4Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[80], r1^sk[81], r2^sk[82], r3^sk[83]
	linearInv(&r0, &r1, &r2, &r3)
	sb3Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[76], r1^sk[77], r2^sk[78], r3^sk[79]
	linearInv(&r0, &r1, &r2, &r3)
	sb2Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[72], r1^sk[73], r2^sk[74], r3^sk[75]
	linearInv(&r0, &r1, &r2, &r3)
	sb1Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[68], r1^sk[69], r2^sk[70], r3^sk[71]
	linearInv(&r0, &r1, &r2, &r3)
	sb0Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[64], r1^sk[65], r2^sk[66], r3^sk[67]
	linearInv(&r0, &r1, &r2, &r3)

	sb7Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[60], r1^sk[61], r2^sk[62], r3^sk[63]
	linearInv(&r0, &r1, &r2, &r3)
	sb6Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[56], r1^sk[57], r2^sk[58], r3^sk[59]
	linearInv(&r0, &r1, &r2, &r3)
	sb5Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[52], r1^sk[53], r2^sk[54], r3^sk[55]
	linearInv(&r0, &r1, &r2, &r3)
	sb4Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[48], r1^sk[49], r2^sk[50], r3^sk[51]
	linearInv(&r0, &r1, &r2, &r3)
	sb3Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[44], r1^sk[45], r2^sk[46], r3^sk[47]
	linearInv(&r0, &r1, &r2, &r3)
	sb2Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[40], r1^sk[41], r2^sk[42], r3^sk[43]
	linearInv(&r0, &r1, &r2, &r3)
	sb1Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[36], r1^sk[37], r2^sk[38], r3^sk[39]
	linearInv(&r0, &r1, &r2, &r3)
	sb0Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[32], r1^sk[33], r2^sk[34], r3^sk[35]
	linearInv(&r0, &r1, &r2, &r3)

	sb7Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[28], r1^sk[29], r2^sk[30], r3^sk[31]
	linearInv(&r0, &r1, &r2, &r3)
	sb6Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[24], r1^sk[25], r2^sk[26], r3^sk[27]
	linearInv(&r0, &r1, &r2, &r3)
	sb5Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[20], r1^sk[21], r2^sk[22], r3^sk[23]
	linearInv(&r0, &r1, &r2, &r3)
	sb4Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[16], r1^sk[17], r2^sk[18], r3^sk[19]
	linearInv(&r0, &r1, &r2, &r3)
	sb3Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[12], r1^sk[13], r2^sk[14], r3^sk[15]
	linearInv(&r0, &r1, &r2, &r3)
	sb2Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[8], r1^sk[9], r2^sk[10], r3^sk[11]
	linearInv(&r0, &r1, &r2, &r3)
	sb1Inv(&r0, &r1, &r2, &r3)
	r0, r1, r2, r3 = r0^sk[4], r1^sk[5], r2^sk[6], r3^sk[7]
	linearInv(&r0, &r1, &r2, &r3)
	sb0Inv(&r0, &r1, &r2, &r3)

	r0 ^= sk[0]
	r1 ^= sk[1]
	r2 ^= sk[2]
	r3 ^= sk[3]

	// write the decrypted block to the output
	dst[0] = byte(r0)
	dst[1] = byte(r0 >> 8)
	dst[2] = byte(r0 >> 16)
	dst[3] = byte(r0 >> 24)
	dst[4] = byte(r1)
	dst[5] = byte(r1 >> 8)
	dst[6] = byte(r1 >> 16)
	dst[7] = byte(r1 >> 24)
	dst[8] = byte(r2)
	dst[9] = byte(r2 >> 8)
	dst[10] = byte(r2 >> 16)
	dst[11] = byte(r2 >> 24)
	dst[12] = byte(r3)
	dst[13] = byte(r3 >> 8)
	dst[14] = byte(r3 >> 16)
	dst[15] = byte(r3 >> 24)
}
