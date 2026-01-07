// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Ebitengine Authors

//go:build linux

#include "textflag.h"
#include "go_asm.h"
#include "funcdata.h"

#define STACK_SIZE 64
#define PTR_ADDRESS (STACK_SIZE - 8)

// syscall15X calls a function in libc on behalf of the syscall package.
// syscall15X takes a pointer to a struct like:
// struct {
//	fn    uintptr
//	a1    uintptr
//	a2    uintptr
//	a3    uintptr
//	a4    uintptr
//	a5    uintptr
//	a6    uintptr
//	a7    uintptr
//	a8    uintptr
//	a9    uintptr
//	a10    uintptr
//	a11    uintptr
//	a12    uintptr
//	a13    uintptr
//	a14    uintptr
//	a15    uintptr
//	r1    uintptr
//	r2    uintptr
//	err   uintptr
// }
// syscall15X must be called on the g0 stack with the
// C calling convention (use libcCall).
GLOBL ·syscall15XABI0(SB), NOPTR|RODATA, $8
DATA ·syscall15XABI0(SB)/8, $syscall15X(SB)
TEXT syscall15X(SB), NOSPLIT, $0
	// push structure pointer
	SUBV	$STACK_SIZE, R3
	MOVV	R4, PTR_ADDRESS(R3)
	MOVV	R4, R13

	MOVD	syscall15Args_f1(R13), F0	// f1
	MOVD	syscall15Args_f2(R13), F1	// f2
	MOVD	syscall15Args_f3(R13), F2	// f3
	MOVD	syscall15Args_f4(R13), F3	// f4
	MOVD	syscall15Args_f5(R13), F4	// f5
	MOVD	syscall15Args_f6(R13), F5	// f6
	MOVD	syscall15Args_f7(R13), F6	// f7
	MOVD	syscall15Args_f8(R13), F7	// f8

	MOVV	syscall15Args_a1(R13), R4	// a1
	MOVV	syscall15Args_a2(R13), R5	// a2
	MOVV	syscall15Args_a3(R13), R6	// a3
	MOVV	syscall15Args_a4(R13), R7	// a4
	MOVV	syscall15Args_a5(R13), R8	// a5
	MOVV	syscall15Args_a6(R13), R9	// a6
	MOVV	syscall15Args_a7(R13), R10	// a7
	MOVV	syscall15Args_a8(R13), R11	// a8

	// push a9-a15 onto stack
	MOVV	syscall15Args_a9(R13), R12
	MOVV	R12, 0(R3)
	MOVV	syscall15Args_a10(R13), R12
	MOVV	R12, 8(R3)
	MOVV	syscall15Args_a11(R13), R12
	MOVV	R12, 16(R3)
	MOVV	syscall15Args_a12(R13), R12
	MOVV	R12, 24(R3)
	MOVV	syscall15Args_a13(R13), R12
	MOVV	R12, 32(R3)
	MOVV	syscall15Args_a14(R13), R12
	MOVV	R12, 40(R3)
	MOVV	syscall15Args_a15(R13), R12
	MOVV	R12, 48(R3)

	MOVV	syscall15Args_fn(R13), R12
	JAL	(R12)

	// pop structure pointer
	MOVV	PTR_ADDRESS(R3), R13
	ADDV	$STACK_SIZE, R3

	// save R4, R5
	MOVV	R4, syscall15Args_a1(R13)
	MOVV	R5, syscall15Args_a2(R13)

	// save f0-f3
	MOVD	F0, syscall15Args_f1(R13)
	MOVD	F1, syscall15Args_f2(R13)
	MOVD	F2, syscall15Args_f3(R13)
	MOVD	F3, syscall15Args_f4(R13)
	RET
