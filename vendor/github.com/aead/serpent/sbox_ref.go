// Copyright (c) 2016 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

package serpent

// The linear transformation of serpent
// This version, tries not to minimize the
// number of registers, but maximize parallism.
func linear(v0, v1, v2, v3 *uint32) {
	t0 := ((*v0 << 13) | (*v0 >> (32 - 13)))
	t2 := ((*v2 << 3) | (*v2 >> (32 - 3)))
	t1 := *v1 ^ t0 ^ t2
	t3 := *v3 ^ t2 ^ (t0 << 3)
	*v1 = (t1 << 1) | (t1 >> (32 - 1))
	*v3 = (t3 << 7) | (t3 >> (32 - 7))
	t0 ^= *v1 ^ *v3
	t2 ^= *v3 ^ (*v1 << 7)
	*v0 = (t0 << 5) | (t0 >> (32 - 5))
	*v2 = (t2 << 22) | (t2 >> (32 - 22))
}

// The inverse linear transformation of serpent
// This version, tries not to minimize the
// number of registers, but maximize parallism.
func linearInv(v0, v1, v2, v3 *uint32) {
	t2 := (*v2 >> 22) | (*v2 << (32 - 22))
	t0 := (*v0 >> 5) | (*v0 << (32 - 5))
	t2 ^= *v3 ^ (*v1 << 7)
	t0 ^= *v1 ^ *v3
	t3 := (*v3 >> 7) | (*v3 << (32 - 7))
	t1 := (*v1 >> 1) | (*v1 << (32 - 1))
	*v3 = t3 ^ t2 ^ (t0 << 3)
	*v1 = t1 ^ t0 ^ t2
	*v2 = (t2 >> 3) | (t2 << (32 - 3))
	*v0 = (t0 >> 13) | (t0 << (32 - 13))
}

// The following functions sb0,sb1, ..., sb7 represent the 8 Serpent S-Boxes.
// sb0Inv til sb7Inv are the inverse functions (e.g. sb0Inv is the Inverse to sb0
// and vice versa).
// The S-Boxes differ from the original Serpent definitions. This is for
// optimisation. The functions use the Serpent S-Box improvements for (non x86)
// from Dr. B. R. Gladman and Sam Simpson.

// S-Box 0
func sb0(r0, r1, r2, r3 *uint32) {
	t0 := *r0 ^ *r3
	t1 := *r2 ^ t0
	t2 := *r1 ^ t1
	*r3 = (*r0 & *r3) ^ t2
	t3 := *r0 ^ (*r1 & t0)
	*r2 = t2 ^ (*r2 | t3)
	t4 := *r3 & (t1 ^ t3)
	*r1 = (^t1) ^ t4
	*r0 = t4 ^ (^t3)
}

// Inverse S-Box 0
func sb0Inv(r0, r1, r2, r3 *uint32) {
	t0 := ^(*r0)
	t1 := *r0 ^ *r1
	t2 := *r3 ^ (t0 | t1)
	t3 := *r2 ^ t2
	*r2 = t1 ^ t3
	t4 := t0 ^ (*r3 & t1)
	*r1 = t2 ^ (*r2 & t4)
	*r3 = (*r0 & t2) ^ (t3 | *r1)
	*r0 = *r3 ^ (t3 ^ t4)
}

// S-Box 1
func sb1(r0, r1, r2, r3 *uint32) {
	t0 := *r1 ^ (^(*r0))
	t1 := *r2 ^ (*r0 | t0)
	*r2 = *r3 ^ t1
	t2 := *r1 ^ (*r3 | t0)
	t3 := t0 ^ *r2
	*r3 = t3 ^ (t1 & t2)
	t4 := t1 ^ t2
	*r1 = *r3 ^ t4
	*r0 = t1 ^ (t3 & t4)
}

// Inverse S-Box 1
func sb1Inv(r0, r1, r2, r3 *uint32) {
	t0 := *r1 ^ *r3
	t1 := *r0 ^ (*r1 & t0)
	t2 := t0 ^ t1
	*r3 = *r2 ^ t2
	t3 := *r1 ^ (t0 & t1)
	t4 := *r3 | t3
	*r1 = t1 ^ t4
	t5 := ^(*r1)
	t6 := *r3 ^ t3
	*r0 = t5 ^ t6
	*r2 = t2 ^ (t5 | t6)
}

// S-Box 2
func sb2(r0, r1, r2, r3 *uint32) {
	v0 := *r0 // save r0
	v3 := *r3 // save r3
	t0 := ^v0
	t1 := *r1 ^ v3
	t2 := *r2 & t0
	*r0 = t1 ^ t2
	t3 := *r2 ^ t0
	t4 := *r2 ^ *r0
	t5 := *r1 & t4
	*r3 = t3 ^ t5
	*r2 = v0 ^ ((v3 | t5) & (*r0 | t3))
	*r1 = (t1 ^ *r3) ^ (*r2 ^ (v3 | t0))
}

// Inverse S-Box 2
func sb2Inv(r0, r1, r2, r3 *uint32) {
	v0 := *r0 // save r0
	v3 := *r3 // save r3
	t0 := *r1 ^ v3
	t1 := ^t0
	t2 := v0 ^ *r2
	t3 := *r2 ^ t0
	t4 := *r1 & t3
	*r0 = t2 ^ t4
	t5 := v0 | t1
	t6 := v3 ^ t5
	t7 := t2 | t6
	*r3 = t0 ^ t7
	t8 := ^t3
	t9 := *r0 | *r3
	*r1 = t8 ^ t9
	*r2 = (v3 & t8) ^ (t2 ^ t9)
}

// S-Box 3
func sb3(r0, r1, r2, r3 *uint32) {
	v1 := *r1 // save r1
	v3 := *r3 // save r3
	t0 := *r0 ^ *r1
	t1 := *r0 & *r2
	t2 := *r0 | *r3
	t3 := *r2 ^ *r3
	t4 := t0 & t2
	t5 := t1 | t4
	*r2 = t3 ^ t5
	t6 := *r1 ^ t2
	t7 := t5 ^ t6
	t8 := t3 & t7
	*r0 = t0 ^ t8
	t9 := *r2 & *r0
	*r1 = t7 ^ t9
	*r3 = (v1 | v3) ^ (t3 ^ t9)
}

// Inverse S-Box 3
func sb3Inv(r0, r1, r2, r3 *uint32) {
	t0 := *r0 | *r1
	t1 := *r1 ^ *r2
	t2 := *r1 & t1
	t3 := *r0 ^ t2
	t4 := *r2 ^ t3
	t5 := *r3 | t3
	*r0 = t1 ^ t5
	t6 := t1 | t5
	t7 := *r3 ^ t6
	*r2 = t4 ^ t7
	t8 := t0 ^ t7
	t9 := *r0 & t8
	*r3 = t3 ^ t9
	*r1 = *r3 ^ (*r0 ^ t8)
}

// S-Box 4
func sb4(r0, r1, r2, r3 *uint32) {
	v0 := *r0 // save r0
	t0 := v0 ^ *r3
	t1 := *r3 & t0
	t2 := *r2 ^ t1
	t3 := *r1 | t2
	*r3 = t0 ^ t3
	t4 := ^(*r1)
	t5 := t0 | t4
	*r0 = t2 ^ t5
	t6 := v0 & *r0
	t7 := t0 ^ t4
	t8 := t3 & t7
	*r2 = t6 ^ t8
	*r1 = (v0 ^ t2) ^ (t7 & *r2)
}

// Inverse S-Box 4
func sb4Inv(r0, r1, r2, r3 *uint32) {
	v3 := *r3 // save r3
	t0 := *r2 | v3
	t1 := *r0 & t0
	t2 := *r1 ^ t1
	t3 := *r0 & t2
	t4 := *r2 ^ t3
	*r1 = v3 ^ t4
	t5 := ^(*r0)
	t6 := t4 & *r1
	*r3 = t2 ^ t6
	t7 := *r1 | t5
	t8 := v3 ^ t7
	*r0 = *r3 ^ t8
	*r2 = (t2 & t8) ^ (*r1 ^ t5)
}

// S-Box 5
func sb5(r0, r1, r2, r3 *uint32) {
	v1 := *r1 // save r1
	t0 := ^(*r0)
	t1 := *r0 ^ v1
	t2 := *r0 ^ *r3
	t3 := *r2 ^ t0
	t4 := t1 | t2
	*r0 = t3 ^ t4
	t5 := *r3 & *r0
	t6 := t1 ^ *r0
	*r1 = t5 ^ t6
	t7 := t0 | *r0
	t8 := t1 | t5
	t9 := t2 ^ t7
	*r2 = t8 ^ t9
	*r3 = (v1 ^ t5) ^ (*r1 & t9)
}

// Inverse S-Box 5
func sb5Inv(r0, r1, r2, r3 *uint32) {
	v0 := *r0 // save r0
	v1 := *r1 // save r1
	v3 := *r3 // save r3
	t0 := ^(*r2)
	t1 := v1 & t0
	t2 := v3 ^ t1
	t3 := v0 & t2
	t4 := v1 ^ t0
	*r3 = t3 ^ t4
	t5 := v1 | *r3
	t6 := v0 & t5
	*r1 = t2 ^ t6
	t7 := v0 | v3
	t8 := t0 ^ t5
	*r0 = t7 ^ t8
	*r2 = (v1 & t7) ^ (t3 | (v0 ^ *r2))
}

// S-Box 6
func sb6(r0, r1, r2, r3 *uint32) {
	t0 := ^(*r0)
	t1 := *r0 ^ *r3
	t2 := *r1 ^ t1
	t3 := t0 | t1
	t4 := *r2 ^ t3
	*r1 = *r1 ^ t4
	t5 := t1 | *r1
	t6 := *r3 ^ t5
	t7 := t4 & t6
	*r2 = t2 ^ t7
	t8 := t4 ^ t6
	*r0 = *r2 ^ t8
	*r3 = (^t4) ^ (t2 & t8)
}

// Inverse S-Box 6
func sb6Inv(r0, r1, r2, r3 *uint32) {
	v1 := *r1 // save r1
	v3 := *r3 // save r3
	t0 := ^(*r0)
	t1 := *r0 ^ v1
	t2 := *r2 ^ t1
	t3 := *r2 | t0
	t4 := v3 ^ t3
	*r1 = t2 ^ t4
	t5 := t2 & t4
	t6 := t1 ^ t5
	t7 := v1 | t6
	*r3 = t4 ^ t7
	t8 := v1 | *r3
	*r0 = t6 ^ t8
	*r2 = (v3 & t0) ^ (t2 ^ t8)
}

// S-Box 7
func sb7(r0, r1, r2, r3 *uint32) {
	t0 := *r1 ^ *r2
	t1 := *r2 & t0
	t2 := *r3 ^ t1
	t3 := *r0 ^ t2
	t4 := *r3 | t0
	t5 := t3 & t4
	*r1 = *r1 ^ t5
	t6 := t2 | *r1
	t7 := *r0 & t3
	*r3 = t0 ^ t7
	t8 := t3 ^ t6
	t9 := *r3 & t8
	*r2 = t2 ^ t9
	*r0 = (^t8) ^ (*r3 & *r2)
}

// Inverse S-Box 7
func sb7Inv(r0, r1, r2, r3 *uint32) {
	v0 := *r0 // save r0
	v3 := *r3 // save r3
	t0 := *r2 | (v0 & *r1)
	t1 := v3 & (v0 | *r1)
	*r3 = t0 ^ t1
	t2 := ^v3
	t3 := *r1 ^ t1
	t4 := t3 | (*r3 ^ t2)
	*r1 = v0 ^ t4
	*r0 = (*r2 ^ t3) ^ (v3 | *r1)
	*r2 = (t0 ^ *r1) ^ (*r0 ^ (v0 & *r3))
}
