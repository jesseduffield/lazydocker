// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Ebitengine Authors

//go:build linux

#include "textflag.h"
#include "go_asm.h"
#include "funcdata.h"
#include "abi_loong64.h"

TEXT callbackasm1(SB), NOSPLIT|NOFRAME, $0
	NO_LOCAL_POINTERS

	SUBV	$(16*8), R3, R14
	MOVD	F0, 0(R14)
	MOVD	F1, 8(R14)
	MOVD	F2, 16(R14)
	MOVD	F3, 24(R14)
	MOVD	F4, 32(R14)
	MOVD	F5, 40(R14)
	MOVD	F6, 48(R14)
	MOVD	F7, 56(R14)
	MOVV	R4, 64(R14)
	MOVV	R5, 72(R14)
	MOVV	R6, 80(R14)
	MOVV	R7, 88(R14)
	MOVV	R8, 96(R14)
	MOVV	R9, 104(R14)
	MOVV	R10, 112(R14)
	MOVV	R11, 120(R14)

	// Adjust SP by frame size.
	SUBV	$(22*8), R3

	// It is important to save R30 because the go assembler
	// uses it for move instructions for a variable.
	// This line:
	// MOVV ·callbackWrap_call(SB), R4
	// Creates the instructions:
	// PCALAU12I	off1(PC), R30
	// MOVV		off2(R30), R4
	// R30 is a callee saved register so we are responsible
	// for ensuring its value doesn't change. So save it and
	// restore it at the end of this function.
	// R1 is the link register. crosscall2 doesn't save it
	// so it's saved here.
	MOVV	R1, 0(R3)
	MOVV	R30, 8(R3)

	// Create a struct callbackArgs on our stack.
	MOVV	$(callbackArgs__size)(R3), R13
	MOVV	R12, callbackArgs_index(R13)    // callback index
	MOVV	R14, callbackArgs_args(R13)     // address of args vector
	MOVV	$0, callbackArgs_result(R13)    // result

	// Move parameters into registers
	// Get the ABIInternal function pointer
	// without <ABIInternal> by using a closure.
	MOVV	·callbackWrap_call(SB), R4
	MOVV	(R4), R4  // fn unsafe.Pointer
	MOVV	R13, R5   // frame (&callbackArgs{...})
	MOVV	$0, R7	  // ctxt uintptr

	JAL	crosscall2(SB)

	// Get callback result.
	MOVV	$(callbackArgs__size)(R3), R13
	MOVV	callbackArgs_result(R13), R4

	// Restore LR and R30
	MOVV	0(R3), R1
	MOVV	8(R3), R30
	ADDV	$(22*8), R3

	RET
