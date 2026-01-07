// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package schema

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

const (
	defaultMaxSize = 16000
)

// NewDecoder returns a new Decoder.
func NewDecoder() *Decoder {
	return &Decoder{cache: newCache(), maxSize: defaultMaxSize}
}

// Decoder decodes values from a map[string][]string to a struct.
type Decoder struct {
	cache             *cache
	zeroEmpty         bool
	ignoreUnknownKeys bool
	maxSize           int
}

// SetAliasTag changes the tag used to locate custom field aliases.
// The default tag is "schema".
func (d *Decoder) SetAliasTag(tag string) {
	d.cache.tag = tag
}

// ZeroEmpty controls the behaviour when the decoder encounters empty values
// in a map.
// If z is true and a key in the map has the empty string as a value
// then the corresponding struct field is set to the zero value.
// If z is false then empty strings are ignored.
//
// The default value is false, that is empty values do not change
// the value of the struct field.
func (d *Decoder) ZeroEmpty(z bool) {
	d.zeroEmpty = z
}

// IgnoreUnknownKeys controls the behaviour when the decoder encounters unknown
// keys in the map.
// If i is true and an unknown field is encountered, it is ignored. This is
// similar to how unknown keys are handled by encoding/json.
// If i is false then Decode will return an error. Note that any valid keys
// will still be decoded in to the target struct.
//
// To preserve backwards compatibility, the default value is false.
func (d *Decoder) IgnoreUnknownKeys(i bool) {
	d.ignoreUnknownKeys = i
}

// MaxSize limits the size of slices for URL nested arrays or object arrays.
// Choose MaxSize carefully; large values may create many zero-value slice elements.
// Example: "items.100000=apple" would create a slice with 100,000 empty strings.
func (d *Decoder) MaxSize(size int) {
	d.maxSize = size
}

// RegisterConverter registers a converter function for a custom type.
func (d *Decoder) RegisterConverter(value interface{}, converterFunc Converter) {
	d.cache.registerConverter(value, converterFunc)
}

// Decode decodes a map[string][]string to a struct.
//
// The first parameter must be a pointer to a struct.
//
// The second parameter is a map, typically url.Values from an HTTP request.
// Keys are "paths" in dotted notation to the struct fields and nested structs.
//
// See the package documentation for a full explanation of the mechanics.
func (d *Decoder) Decode(dst interface{}, src map[string][]string) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("schema: interface must be a pointer to struct")
	}
	v = v.Elem()
	t := v.Type()
	errors := MultiError{}
	for path, values := range src {
		if parts, err := d.cache.parsePath(path, t); err == nil {
			if err = d.decode(v, path, parts, values); err != nil {
				errors[path] = err
			}
		} else if !d.ignoreUnknownKeys {
			errors[path] = UnknownKeyError{Key: path}
		}
	}
	errors.merge(d.setDefaults(t, v))
	errors.merge(d.checkRequired(t, src))
	if len(errors) > 0 {
		return errors
	}
	return nil
}

// setDefaults sets the default values when the `default` tag is specified,
// default is supported on basic/primitive types and their pointers,
// nested structs can also have default tags
func (d *Decoder) setDefaults(t reflect.Type, v reflect.Value) MultiError {
	struc := d.cache.get(t)
	if struc == nil {
		// unexpect, cache.get never return nil
		return MultiError{"default-" + t.Name(): errors.New("cache fail")}
	}

	errs := MultiError{}

	if v.Type().Kind() == reflect.Struct {
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.Type().Kind() == reflect.Ptr && field.IsNil() && v.Type().Field(i).Anonymous {
				field.Set(reflect.New(field.Type().Elem()))
			}
		}
	}

	for _, f := range struc.fields {
		vCurrent := v.FieldByName(f.name)

		if vCurrent.Type().Kind() == reflect.Struct && f.defaultValue == "" {
			errs.merge(d.setDefaults(vCurrent.Type(), vCurrent))
		} else if isPointerToStruct(vCurrent) && f.defaultValue == "" {
			errs.merge(d.setDefaults(vCurrent.Elem().Type(), vCurrent.Elem()))
		}

		if f.defaultValue != "" && f.isRequired {
			errs.merge(MultiError{"default-" + f.name: errors.New("required fields cannot have a default value")})
		} else if f.defaultValue != "" && vCurrent.IsZero() && !f.isRequired {
			if f.typ.Kind() == reflect.Struct {
				errs.merge(MultiError{"default-" + f.name: errors.New("default option is supported only on: bool, float variants, string, unit variants types or their corresponding pointers or slices")})
			} else if f.typ.Kind() == reflect.Slice {
				vals := strings.Split(f.defaultValue, "|")

				// check if slice has one of the supported types for defaults
				if _, ok := builtinConverters[f.typ.Elem().Kind()]; !ok {
					errs.merge(MultiError{"default-" + f.name: errors.New("default option is supported only on: bool, float variants, string, unit variants types or their corresponding pointers or slices")})
					continue
				}

				defaultSlice := reflect.MakeSlice(f.typ, 0, cap(vals))
				for _, val := range vals {
					// this check is to handle if the wrong value is provided
					convertedVal := builtinConverters[f.typ.Elem().Kind()](val)
					if !convertedVal.IsValid() {
						errs.merge(MultiError{"default-" + f.name: fmt.Errorf("failed setting default: %s is not compatible with field %s type", val, f.name)})
						break
					}
					defaultSlice = reflect.Append(defaultSlice, convertedVal)
				}
				vCurrent.Set(defaultSlice)
			} else if f.typ.Kind() == reflect.Ptr {
				t1 := f.typ.Elem()

				if t1.Kind() == reflect.Struct || t1.Kind() == reflect.Slice {
					errs.merge(MultiError{"default-" + f.name: errors.New("default option is supported only on: bool, float variants, string, unit variants types or their corresponding pointers or slices")})
				}

				// this check is to handle if the wrong value is provided
				if convertedVal := convertPointer(t1.Kind(), f.defaultValue); convertedVal.IsValid() {
					vCurrent.Set(convertedVal)
				}
			} else {
				// this check is to handle if the wrong value is provided
				if convertedVal := builtinConverters[f.typ.Kind()](f.defaultValue); convertedVal.IsValid() {
					vCurrent.Set(builtinConverters[f.typ.Kind()](f.defaultValue))
				}
			}
		}
	}

	return errs
}

func isPointerToStruct(v reflect.Value) bool {
	return !v.IsZero() && v.Type().Kind() == reflect.Ptr && v.Elem().Type().Kind() == reflect.Struct
}

// checkRequired checks whether required fields are empty
//
// check type t recursively if t has struct fields.
//
// src is the source map for decoding, we use it here to see if those required fields are included in src
func (d *Decoder) checkRequired(t reflect.Type, src map[string][]string) MultiError {
	m, errs := d.findRequiredFields(t, "", "")
	for key, fields := range m {
		if isEmptyFields(fields, src) {
			errs[key] = EmptyFieldError{Key: key}
		}
	}
	return errs
}

// findRequiredFields recursively searches the struct type t for required fields.
//
// canonicalPrefix and searchPrefix are used to resolve full paths in dotted notation
// for nested struct fields. canonicalPrefix is a complete path which never omits
// any embedded struct fields. searchPrefix is a user-friendly path which may omit
// some embedded struct fields to point promoted fields.
func (d *Decoder) findRequiredFields(t reflect.Type, canonicalPrefix, searchPrefix string) (map[string][]fieldWithPrefix, MultiError) {
	struc := d.cache.get(t)
	if struc == nil {
		// unexpect, cache.get never return nil
		return nil, MultiError{canonicalPrefix + "*": errors.New("cache fail")}
	}

	m := map[string][]fieldWithPrefix{}
	errs := MultiError{}
	for _, f := range struc.fields {
		if f.typ.Kind() == reflect.Struct {
			fcprefix := canonicalPrefix + f.canonicalAlias + "."
			for _, fspath := range f.paths(searchPrefix) {
				fm, ferrs := d.findRequiredFields(f.typ, fcprefix, fspath+".")
				for key, fields := range fm {
					m[key] = append(m[key], fields...)
				}
				errs.merge(ferrs)
			}
		}
		if f.isRequired {
			key := canonicalPrefix + f.canonicalAlias
			m[key] = append(m[key], fieldWithPrefix{
				fieldInfo: f,
				prefix:    searchPrefix,
			})
		}
	}
	return m, errs
}

type fieldWithPrefix struct {
	*fieldInfo
	prefix string
}

// isEmptyFields returns true if all of specified fields are empty.
func isEmptyFields(fields []fieldWithPrefix, src map[string][]string) bool {
	for _, f := range fields {
		for _, path := range f.paths(f.prefix) {
			v, ok := src[path]
			if ok && !isEmpty(f.typ, v) {
				return false
			}
			for key := range src {
				if !isEmpty(f.typ, src[key]) && strings.HasPrefix(key, path) {
					return false
				}
			}
		}
	}
	return true
}

// isEmpty returns true if value is empty for specific type
func isEmpty(t reflect.Type, value []string) bool {
	if len(value) == 0 {
		return true
	}
	switch t.Kind() {
	case boolType, float32Type, float64Type, intType, int8Type, int32Type, int64Type, stringType, uint8Type, uint16Type, uint32Type, uint64Type:
		return len(value[0]) == 0
	}
	return false
}

// decode fills a struct field using a parsed path.
func (d *Decoder) decode(v reflect.Value, path string, parts []pathPart, values []string) error {
	// Get the field walking the struct fields by index.
	for _, name := range parts[0].path {
		if v.Type().Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}

		// alloc embedded structs
		if v.Type().Kind() == reflect.Struct {
			for i := 0; i < v.NumField(); i++ {
				field := v.Field(i)
				if field.Type().Kind() == reflect.Ptr && field.IsNil() && v.Type().Field(i).Anonymous {
					field.Set(reflect.New(field.Type().Elem()))
				}
			}
		}

		v = v.FieldByName(name)
	}
	// Don't even bother for unexported fields.
	if !v.CanSet() {
		return nil
	}

	// Dereference if needed.
	t := v.Type()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		if v.IsNil() {
			v.Set(reflect.New(t))
		}
		v = v.Elem()
	}

	// Slice of structs. Let's go recursive.
	if len(parts) > 1 {
		idx := parts[0].index
		// a defensive check to avoid creating a large slice based on user input index
		if idx > d.maxSize {
			return fmt.Errorf("%v index %d is larger than the configured maxSize %d", v.Kind(), idx, d.maxSize)
		}
		if v.IsNil() || v.Len() < idx+1 {
			value := reflect.MakeSlice(t, idx+1, idx+1)
			if v.Len() < idx+1 {
				// Resize it.
				reflect.Copy(value, v)
			}
			v.Set(value)
		}
		return d.decode(v.Index(idx), path, parts[1:], values)
	}

	// Get the converter early in case there is one for a slice type.
	conv := d.cache.converter(t)
	m := isTextUnmarshaler(v)
	if conv == nil && t.Kind() == reflect.Slice && m.IsSliceElement {
		var items []reflect.Value
		elemT := t.Elem()
		isPtrElem := elemT.Kind() == reflect.Ptr
		if isPtrElem {
			elemT = elemT.Elem()
		}

		// Try to get a converter for the element type.
		conv := d.cache.converter(elemT)
		if conv == nil {
			conv = builtinConverters[elemT.Kind()]
			if conv == nil {
				// As we are not dealing with slice of structs here, we don't need to check if the type
				// implements TextUnmarshaler interface
				return fmt.Errorf("schema: converter not found for %v", elemT)
			}
		}

		for key, value := range values {
			if value == "" {
				if d.zeroEmpty {
					items = append(items, reflect.Zero(elemT))
				}
			} else if m.IsValid {
				u := reflect.New(elemT)
				if m.IsSliceElementPtr {
					u = reflect.New(reflect.PtrTo(elemT).Elem())
				}
				if err := u.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(value)); err != nil {
					return ConversionError{
						Key:   path,
						Type:  t,
						Index: key,
						Err:   err,
					}
				}
				if m.IsSliceElementPtr {
					items = append(items, u.Elem().Addr())
				} else if u.Kind() == reflect.Ptr {
					items = append(items, u.Elem())
				} else {
					items = append(items, u)
				}
			} else if item := conv(value); item.IsValid() {
				if isPtrElem {
					ptr := reflect.New(elemT)
					ptr.Elem().Set(item)
					item = ptr
				}
				if item.Type() != elemT && !isPtrElem {
					item = item.Convert(elemT)
				}
				items = append(items, item)
			} else {
				if strings.Contains(value, ",") {
					values := strings.Split(value, ",")
					for _, value := range values {
						if value == "" {
							if d.zeroEmpty {
								items = append(items, reflect.Zero(elemT))
							}
						} else if item := conv(value); item.IsValid() {
							if isPtrElem {
								ptr := reflect.New(elemT)
								ptr.Elem().Set(item)
								item = ptr
							}
							if item.Type() != elemT && !isPtrElem {
								item = item.Convert(elemT)
							}
							items = append(items, item)
						} else {
							return ConversionError{
								Key:   path,
								Type:  elemT,
								Index: key,
							}
						}
					}
				} else {
					return ConversionError{
						Key:   path,
						Type:  elemT,
						Index: key,
					}
				}
			}
		}
		value := reflect.Append(reflect.MakeSlice(t, 0, 0), items...)
		v.Set(value)
	} else {
		val := ""
		// Use the last value provided if any values were provided
		if len(values) > 0 {
			val = values[len(values)-1]
		}

		if conv != nil {
			if value := conv(val); value.IsValid() {
				v.Set(value.Convert(t))
			} else {
				return ConversionError{
					Key:   path,
					Type:  t,
					Index: -1,
				}
			}
		} else if m.IsValid {
			if m.IsPtr {
				u := reflect.New(v.Type())
				if err := u.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(val)); err != nil {
					return ConversionError{
						Key:   path,
						Type:  t,
						Index: -1,
						Err:   err,
					}
				}
				v.Set(reflect.Indirect(u))
			} else {
				// If the value implements the encoding.TextUnmarshaler interface
				// apply UnmarshalText as the converter
				if err := m.Unmarshaler.UnmarshalText([]byte(val)); err != nil {
					return ConversionError{
						Key:   path,
						Type:  t,
						Index: -1,
						Err:   err,
					}
				}
			}
		} else if val == "" {
			if d.zeroEmpty {
				v.Set(reflect.Zero(t))
			}
		} else if conv := builtinConverters[t.Kind()]; conv != nil {
			if value := conv(val); value.IsValid() {
				v.Set(value.Convert(t))
			} else {
				return ConversionError{
					Key:   path,
					Type:  t,
					Index: -1,
				}
			}
		} else {
			return fmt.Errorf("schema: converter not found for %v", t)
		}
	}
	return nil
}

func isTextUnmarshaler(v reflect.Value) unmarshaler {
	// Create a new unmarshaller instance
	m := unmarshaler{}
	if m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler); m.IsValid {
		return m
	}
	// As the UnmarshalText function should be applied to the pointer of the
	// type, we check that type to see if it implements the necessary
	// method.
	if m.Unmarshaler, m.IsValid = reflect.New(v.Type()).Interface().(encoding.TextUnmarshaler); m.IsValid {
		m.IsPtr = true
		return m
	}

	// if v is []T or *[]T create new T
	t := v.Type()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		// Check if the slice implements encoding.TextUnmarshaller
		if m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler); m.IsValid {
			return m
		}
		// If t is a pointer slice, check if its elements implement
		// encoding.TextUnmarshaler
		m.IsSliceElement = true
		if t = t.Elem(); t.Kind() == reflect.Ptr {
			t = reflect.PtrTo(t.Elem())
			v = reflect.Zero(t)
			m.IsSliceElementPtr = true
			m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler)
			return m
		}
	}

	v = reflect.New(t)
	m.Unmarshaler, m.IsValid = v.Interface().(encoding.TextUnmarshaler)
	return m
}

// TextUnmarshaler helpers ----------------------------------------------------
// unmarshaller contains information about a TextUnmarshaler type
type unmarshaler struct {
	Unmarshaler encoding.TextUnmarshaler
	// IsValid indicates whether the resolved type indicated by the other
	// flags implements the encoding.TextUnmarshaler interface.
	IsValid bool
	// IsPtr indicates that the resolved type is the pointer of the original
	// type.
	IsPtr bool
	// IsSliceElement indicates that the resolved type is a slice element of
	// the original type.
	IsSliceElement bool
	// IsSliceElementPtr indicates that the resolved type is a pointer to a
	// slice element of the original type.
	IsSliceElementPtr bool
}

// Errors ---------------------------------------------------------------------

// ConversionError stores information about a failed conversion.
type ConversionError struct {
	Key   string       // key from the source map.
	Type  reflect.Type // expected type of elem
	Index int          // index for multi-value fields; -1 for single-value fields.
	Err   error        // low-level error (when it exists)
}

func (e ConversionError) Error() string {
	var output string

	if e.Index < 0 {
		output = fmt.Sprintf("schema: error converting value for %q", e.Key)
	} else {
		output = fmt.Sprintf("schema: error converting value for index %d of %q",
			e.Index, e.Key)
	}

	if e.Err != nil {
		output = fmt.Sprintf("%s. Details: %s", output, e.Err)
	}

	return output
}

// UnknownKeyError stores information about an unknown key in the source map.
type UnknownKeyError struct {
	Key string // key from the source map.
}

func (e UnknownKeyError) Error() string {
	return fmt.Sprintf("schema: invalid path %q", e.Key)
}

// EmptyFieldError stores information about an empty required field.
type EmptyFieldError struct {
	Key string // required key in the source map.
}

func (e EmptyFieldError) Error() string {
	return fmt.Sprintf("%v is empty", e.Key)
}

// MultiError stores multiple decoding errors.
//
// Borrowed from the App Engine SDK.
type MultiError map[string]error

func (e MultiError) Error() string {
	s := ""
	for _, err := range e {
		s = err.Error()
		break
	}
	switch len(e) {
	case 0:
		return "(0 errors)"
	case 1:
		return s
	case 2:
		return s + " (and 1 other error)"
	}
	return fmt.Sprintf("%s (and %d other errors)", s, len(e)-1)
}

func (e MultiError) merge(errors MultiError) {
	for key, err := range errors {
		if e[key] == nil {
			e[key] = err
		}
	}
}
