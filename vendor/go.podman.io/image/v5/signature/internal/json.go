package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"go.podman.io/image/v5/internal/set"
)

// JSONFormatError is returned when JSON does not match expected format.
type JSONFormatError string

func (err JSONFormatError) Error() string {
	return string(err)
}

// ParanoidUnmarshalJSONObject unmarshals data as a JSON object, but failing on the slightest unexpected aspect
// (including duplicated keys, unrecognized keys, and non-matching types). Uses fieldResolver to
// determine the destination for a field value, which should return a pointer to the destination if valid, or nil if the key is rejected.
//
// The fieldResolver approach is useful for decoding the Policy.Transports map; using it for structs is a bit lazy,
// we could use reflection to automate this. Later?
func ParanoidUnmarshalJSONObject(data []byte, fieldResolver func(string) any) error {
	seenKeys := set.New[string]()

	dec := json.NewDecoder(bytes.NewReader(data))
	t, err := dec.Token()
	if err != nil {
		return JSONFormatError(err.Error())
	}
	if t != json.Delim('{') {
		return JSONFormatError(fmt.Sprintf("JSON object expected, got %#v", t))
	}
	for {
		t, err := dec.Token()
		if err != nil {
			return JSONFormatError(err.Error())
		}
		if t == json.Delim('}') {
			break
		}

		key, ok := t.(string)
		if !ok {
			// Coverage: This should never happen, dec.Token() rejects non-string-literals in this state.
			return JSONFormatError(fmt.Sprintf("Key string literal expected, got %#v", t))
		}
		if seenKeys.Contains(key) {
			return JSONFormatError(fmt.Sprintf("Duplicate key %q", key))
		}
		seenKeys.Add(key)

		valuePtr := fieldResolver(key)
		if valuePtr == nil {
			return JSONFormatError(fmt.Sprintf("Unknown key %q", key))
		}
		// This works like json.Unmarshal, in particular it allows us to implement UnmarshalJSON to implement strict parsing of the field value.
		if err := dec.Decode(valuePtr); err != nil {
			return JSONFormatError(err.Error())
		}
	}
	if _, err := dec.Token(); err != io.EOF {
		return JSONFormatError("Unexpected data after JSON object")
	}
	return nil
}

// ParanoidUnmarshalJSONObjectExactFields unmarshals data as a JSON object, but failing on the slightest unexpected aspect
// (including duplicated keys, unrecognized keys, and non-matching types). Each of the fields in exactFields
// must be present exactly once, and none other fields are accepted.
func ParanoidUnmarshalJSONObjectExactFields(data []byte, exactFields map[string]any) error {
	seenKeys := set.New[string]()
	if err := ParanoidUnmarshalJSONObject(data, func(key string) any {
		if valuePtr, ok := exactFields[key]; ok {
			seenKeys.Add(key)
			return valuePtr
		}
		return nil
	}); err != nil {
		return err
	}
	for key := range exactFields {
		if !seenKeys.Contains(key) {
			return JSONFormatError(fmt.Sprintf(`Key %q missing in a JSON object`, key))
		}
	}
	return nil
}
