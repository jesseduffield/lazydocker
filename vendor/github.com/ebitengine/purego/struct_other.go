// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2024 The Ebitengine Authors

//go:build !amd64 && !arm64 && !loong64

package purego

import "reflect"

func addStruct(v reflect.Value, numInts, numFloats, numStack *int, addInt, addFloat, addStack func(uintptr), keepAlive []any) []any {
	panic("purego: struct arguments are not supported")
}

func getStruct(outType reflect.Type, syscall syscall15Args) (v reflect.Value) {
	panic("purego: struct returns are not supported")
}

func placeRegisters(v reflect.Value, addFloat func(uintptr), addInt func(uintptr)) {
	panic("purego: not needed on other platforms")
}
