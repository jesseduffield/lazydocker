package yaml

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"sync"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/internal/errors"
	"golang.org/x/xerrors"
)

// BytesMarshaler interface may be implemented by types to customize their
// behavior when being marshaled into a YAML document. The returned value
// is marshaled in place of the original value implementing Marshaler.
//
// If an error is returned by MarshalYAML, the marshaling procedure stops
// and returns with the provided error.
type BytesMarshaler interface {
	MarshalYAML() ([]byte, error)
}

// BytesMarshalerContext interface use BytesMarshaler with context.Context.
type BytesMarshalerContext interface {
	MarshalYAML(context.Context) ([]byte, error)
}

// InterfaceMarshaler interface has MarshalYAML compatible with github.com/go-yaml/yaml package.
type InterfaceMarshaler interface {
	MarshalYAML() (interface{}, error)
}

// InterfaceMarshalerContext interface use InterfaceMarshaler with context.Context.
type InterfaceMarshalerContext interface {
	MarshalYAML(context.Context) (interface{}, error)
}

// BytesUnmarshaler interface may be implemented by types to customize their
// behavior when being unmarshaled from a YAML document.
type BytesUnmarshaler interface {
	UnmarshalYAML([]byte) error
}

// BytesUnmarshalerContext interface use BytesUnmarshaler with context.Context.
type BytesUnmarshalerContext interface {
	UnmarshalYAML(context.Context, []byte) error
}

// InterfaceUnmarshaler interface has UnmarshalYAML compatible with github.com/go-yaml/yaml package.
type InterfaceUnmarshaler interface {
	UnmarshalYAML(func(interface{}) error) error
}

// InterfaceUnmarshalerContext interface use InterfaceUnmarshaler with context.Context.
type InterfaceUnmarshalerContext interface {
	UnmarshalYAML(context.Context, func(interface{}) error) error
}

// MapItem is an item in a MapSlice.
type MapItem struct {
	Key, Value interface{}
}

// MapSlice encodes and decodes as a YAML map.
// The order of keys is preserved when encoding and decoding.
type MapSlice []MapItem

// ToMap convert to map[interface{}]interface{}.
func (s MapSlice) ToMap() map[interface{}]interface{} {
	v := map[interface{}]interface{}{}
	for _, item := range s {
		v[item.Key] = item.Value
	}
	return v
}

// Marshal serializes the value provided into a YAML document. The structure
// of the generated document will reflect the structure of the value itself.
// Maps and pointers (to struct, string, int, etc) are accepted as the in value.
//
// Struct fields are only marshalled if they are exported (have an upper case
// first letter), and are marshalled using the field name lowercased as the
// default key. Custom keys may be defined via the "yaml" name in the field
// tag: the content preceding the first comma is used as the key, and the
// following comma-separated options are used to tweak the marshalling process.
// Conflicting names result in a runtime error.
//
// The field tag format accepted is:
//
//	`(...) yaml:"[<key>][,<flag1>[,<flag2>]]" (...)`
//
// The following flags are currently supported:
//
//	omitempty    Only include the field if it's not set to the zero
//	             value for the type or to empty slices or maps.
//	             Zero valued structs will be omitted if all their public
//	             fields are zero, unless they implement an IsZero
//	             method (see the IsZeroer interface type), in which
//	             case the field will be included if that method returns true.
//
//	flow         Marshal using a flow style (useful for structs,
//	             sequences and maps).
//
//	inline       Inline the field, which must be a struct or a map,
//	             causing all of its fields or keys to be processed as if
//	             they were part of the outer struct. For maps, keys must
//	             not conflict with the yaml keys of other struct fields.
//
//	anchor       Marshal with anchor. If want to define anchor name explicitly, use anchor=name style.
//	             Otherwise, if used 'anchor' name only, used the field name lowercased as the anchor name
//
//	alias        Marshal with alias. If want to define alias name explicitly, use alias=name style.
//	             Otherwise, If omitted alias name and the field type is pointer type,
//	             assigned anchor name automatically from same pointer address.
//
// In addition, if the key is "-", the field is ignored.
//
// For example:
//
//	type T struct {
//	    F int `yaml:"a,omitempty"`
//	    B int
//	}
//	yaml.Marshal(&T{B: 2}) // Returns "b: 2\n"
//	yaml.Marshal(&T{F: 1}) // Returns "a: 1\nb: 0\n"
func Marshal(v interface{}) ([]byte, error) {
	return MarshalWithOptions(v)
}

// MarshalWithOptions serializes the value provided into a YAML document with EncodeOptions.
func MarshalWithOptions(v interface{}, opts ...EncodeOption) ([]byte, error) {
	return MarshalContext(context.Background(), v, opts...)
}

// MarshalContext serializes the value provided into a YAML document with context.Context and EncodeOptions.
func MarshalContext(ctx context.Context, v interface{}, opts ...EncodeOption) ([]byte, error) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf, opts...).EncodeContext(ctx, v); err != nil {
		return nil, errors.Wrapf(err, "failed to marshal")
	}
	return buf.Bytes(), nil
}

// ValueToNode convert from value to ast.Node.
func ValueToNode(v interface{}, opts ...EncodeOption) (ast.Node, error) {
	var buf bytes.Buffer
	node, err := NewEncoder(&buf, opts...).EncodeToNode(v)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert value to node")
	}
	return node, nil
}

// Unmarshal decodes the first document found within the in byte slice
// and assigns decoded values into the out value.
//
// Struct fields are only unmarshalled if they are exported (have an
// upper case first letter), and are unmarshalled using the field name
// lowercased as the default key. Custom keys may be defined via the
// "yaml" name in the field tag: the content preceding the first comma
// is used as the key, and the following comma-separated options are
// used to tweak the marshalling process (see Marshal).
// Conflicting names result in a runtime error.
//
// For example:
//
//	type T struct {
//	    F int `yaml:"a,omitempty"`
//	    B int
//	}
//	var t T
//	yaml.Unmarshal([]byte("a: 1\nb: 2"), &t)
//
// See the documentation of Marshal for the format of tags and a list of
// supported tag options.
func Unmarshal(data []byte, v interface{}) error {
	return UnmarshalWithOptions(data, v)
}

// UnmarshalWithOptions decodes with DecodeOptions the first document found within the in byte slice
// and assigns decoded values into the out value.
func UnmarshalWithOptions(data []byte, v interface{}, opts ...DecodeOption) error {
	return UnmarshalContext(context.Background(), data, v, opts...)
}

// UnmarshalContext decodes with context.Context and DecodeOptions.
func UnmarshalContext(ctx context.Context, data []byte, v interface{}, opts ...DecodeOption) error {
	dec := NewDecoder(bytes.NewBuffer(data), opts...)
	if err := dec.DecodeContext(ctx, v); err != nil {
		if err == io.EOF {
			return nil
		}
		return errors.Wrapf(err, "failed to unmarshal")
	}
	return nil
}

// NodeToValue converts node to the value pointed to by v.
func NodeToValue(node ast.Node, v interface{}, opts ...DecodeOption) error {
	var buf bytes.Buffer
	if err := NewDecoder(&buf, opts...).DecodeFromNode(node, v); err != nil {
		return errors.Wrapf(err, "failed to convert node to value")
	}
	return nil
}

// FormatError is a utility function that takes advantage of the metadata
// stored in the errors returned by this package's parser.
//
// If the second argument `colored` is true, the error message is colorized.
// If the third argument `inclSource` is true, the error message will
// contain snippets of the YAML source that was used.
func FormatError(e error, colored, inclSource bool) string {
	var pp errors.PrettyPrinter
	if xerrors.As(e, &pp) {
		var buf bytes.Buffer
		pp.PrettyPrint(&errors.Sink{&buf}, colored, inclSource)
		return buf.String()
	}

	return e.Error()
}

// YAMLToJSON convert YAML bytes to JSON.
func YAMLToJSON(bytes []byte) ([]byte, error) {
	var v interface{}
	if err := UnmarshalWithOptions(bytes, &v, UseOrderedMap()); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal")
	}
	out, err := MarshalWithOptions(v, JSON())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal with json option")
	}
	return out, nil
}

// JSONToYAML convert JSON bytes to YAML.
func JSONToYAML(bytes []byte) ([]byte, error) {
	var v interface{}
	if err := UnmarshalWithOptions(bytes, &v, UseOrderedMap()); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal from json bytes")
	}
	out, err := Marshal(v)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal")
	}
	return out, nil
}

var (
	globalCustomMarshalerMu    sync.Mutex
	globalCustomUnmarshalerMu  sync.Mutex
	globalCustomMarshalerMap   = map[reflect.Type]func(interface{}) ([]byte, error){}
	globalCustomUnmarshalerMap = map[reflect.Type]func(interface{}, []byte) error{}
)

// RegisterCustomMarshaler overrides any encoding process for the type specified in generics.
// If you want to switch the behavior for each encoder, use `CustomMarshaler` defined as EncodeOption.
//
// NOTE: If type T implements MarshalYAML for pointer receiver, the type specified in RegisterCustomMarshaler must be *T.
// If RegisterCustomMarshaler and CustomMarshaler of EncodeOption are specified for the same type,
// the CustomMarshaler specified in EncodeOption takes precedence.
func RegisterCustomMarshaler[T any](marshaler func(T) ([]byte, error)) {
	globalCustomMarshalerMu.Lock()
	defer globalCustomMarshalerMu.Unlock()

	var typ T
	globalCustomMarshalerMap[reflect.TypeOf(typ)] = func(v interface{}) ([]byte, error) {
		return marshaler(v.(T))
	}
}

// RegisterCustomUnmarshaler overrides any decoding process for the type specified in generics.
// If you want to switch the behavior for each decoder, use `CustomUnmarshaler` defined as DecodeOption.
//
// NOTE: If RegisterCustomUnmarshaler and CustomUnmarshaler of DecodeOption are specified for the same type,
// the CustomUnmarshaler specified in DecodeOption takes precedence.
func RegisterCustomUnmarshaler[T any](unmarshaler func(*T, []byte) error) {
	globalCustomUnmarshalerMu.Lock()
	defer globalCustomUnmarshalerMu.Unlock()

	var typ *T
	globalCustomUnmarshalerMap[reflect.TypeOf(typ)] = func(v interface{}, b []byte) error {
		return unmarshaler(v.(*T), b)
	}
}
