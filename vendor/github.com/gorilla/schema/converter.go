// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"reflect"
	"strconv"
)

type Converter func(string) reflect.Value

var (
	invalidValue = reflect.Value{}
	boolType     = reflect.Bool
	float32Type  = reflect.Float32
	float64Type  = reflect.Float64
	intType      = reflect.Int
	int8Type     = reflect.Int8
	int16Type    = reflect.Int16
	int32Type    = reflect.Int32
	int64Type    = reflect.Int64
	stringType   = reflect.String
	uintType     = reflect.Uint
	uint8Type    = reflect.Uint8
	uint16Type   = reflect.Uint16
	uint32Type   = reflect.Uint32
	uint64Type   = reflect.Uint64
)

// Default converters for basic types.
var builtinConverters = map[reflect.Kind]Converter{
	boolType:    convertBool,
	float32Type: convertFloat32,
	float64Type: convertFloat64,
	intType:     convertInt,
	int8Type:    convertInt8,
	int16Type:   convertInt16,
	int32Type:   convertInt32,
	int64Type:   convertInt64,
	stringType:  convertString,
	uintType:    convertUint,
	uint8Type:   convertUint8,
	uint16Type:  convertUint16,
	uint32Type:  convertUint32,
	uint64Type:  convertUint64,
}

func convertBool(value string) reflect.Value {
	if value == "on" {
		return reflect.ValueOf(true)
	} else if v, err := strconv.ParseBool(value); err == nil {
		return reflect.ValueOf(v)
	}
	return invalidValue
}

func convertFloat32(value string) reflect.Value {
	if v, err := strconv.ParseFloat(value, 32); err == nil {
		return reflect.ValueOf(float32(v))
	}
	return invalidValue
}

func convertFloat64(value string) reflect.Value {
	if v, err := strconv.ParseFloat(value, 64); err == nil {
		return reflect.ValueOf(v)
	}
	return invalidValue
}

func convertInt(value string) reflect.Value {
	if v, err := strconv.ParseInt(value, 10, 0); err == nil {
		return reflect.ValueOf(int(v))
	}
	return invalidValue
}

func convertInt8(value string) reflect.Value {
	if v, err := strconv.ParseInt(value, 10, 8); err == nil {
		return reflect.ValueOf(int8(v))
	}
	return invalidValue
}

func convertInt16(value string) reflect.Value {
	if v, err := strconv.ParseInt(value, 10, 16); err == nil {
		return reflect.ValueOf(int16(v))
	}
	return invalidValue
}

func convertInt32(value string) reflect.Value {
	if v, err := strconv.ParseInt(value, 10, 32); err == nil {
		return reflect.ValueOf(int32(v))
	}
	return invalidValue
}

func convertInt64(value string) reflect.Value {
	if v, err := strconv.ParseInt(value, 10, 64); err == nil {
		return reflect.ValueOf(v)
	}
	return invalidValue
}

func convertString(value string) reflect.Value {
	return reflect.ValueOf(value)
}

func convertUint(value string) reflect.Value {
	if v, err := strconv.ParseUint(value, 10, 0); err == nil {
		return reflect.ValueOf(uint(v))
	}
	return invalidValue
}

func convertUint8(value string) reflect.Value {
	if v, err := strconv.ParseUint(value, 10, 8); err == nil {
		return reflect.ValueOf(uint8(v))
	}
	return invalidValue
}

func convertUint16(value string) reflect.Value {
	if v, err := strconv.ParseUint(value, 10, 16); err == nil {
		return reflect.ValueOf(uint16(v))
	}
	return invalidValue
}

func convertUint32(value string) reflect.Value {
	if v, err := strconv.ParseUint(value, 10, 32); err == nil {
		return reflect.ValueOf(uint32(v))
	}
	return invalidValue
}

func convertUint64(value string) reflect.Value {
	if v, err := strconv.ParseUint(value, 10, 64); err == nil {
		return reflect.ValueOf(v)
	}
	return invalidValue
}

func convertPointer(k reflect.Kind, value string) reflect.Value {
	switch k {
	case boolType:
		if v := convertBool(value); v.IsValid() {
			converted := v.Bool()
			return reflect.ValueOf(&converted)
		}
	case float32Type:
		if v := convertFloat32(value); v.IsValid() {
			converted := float32(v.Float())
			return reflect.ValueOf(&converted)
		}
	case float64Type:
		if v := convertFloat64(value); v.IsValid() {
			converted := float64(v.Float())
			return reflect.ValueOf(&converted)
		}
	case intType:
		if v := convertInt(value); v.IsValid() {
			converted := int(v.Int())
			return reflect.ValueOf(&converted)
		}
	case int8Type:
		if v := convertInt8(value); v.IsValid() {
			converted := int8(v.Int())
			return reflect.ValueOf(&converted)
		}
	case int16Type:
		if v := convertInt16(value); v.IsValid() {
			converted := int16(v.Int())
			return reflect.ValueOf(&converted)
		}
	case int32Type:
		if v := convertInt32(value); v.IsValid() {
			converted := int32(v.Int())
			return reflect.ValueOf(&converted)
		}
	case int64Type:
		if v := convertInt64(value); v.IsValid() {
			converted := int64(v.Int())
			return reflect.ValueOf(&converted)
		}
	case stringType:
		if v := convertString(value); v.IsValid() {
			converted := v.String()
			return reflect.ValueOf(&converted)
		}
	case uintType:
		if v := convertUint(value); v.IsValid() {
			converted := uint(v.Uint())
			return reflect.ValueOf(&converted)
		}
	case uint8Type:
		if v := convertUint8(value); v.IsValid() {
			converted := uint8(v.Uint())
			return reflect.ValueOf(&converted)
		}
	case uint16Type:
		if v := convertUint16(value); v.IsValid() {
			converted := uint16(v.Uint())
			return reflect.ValueOf(&converted)
		}
	case uint32Type:
		if v := convertUint32(value); v.IsValid() {
			converted := uint32(v.Uint())
			return reflect.ValueOf(&converted)
		}
	case uint64Type:
		if v := convertUint64(value); v.IsValid() {
			converted := uint64(v.Uint())
			return reflect.ValueOf(&converted)
		}
	}

	return invalidValue
}
