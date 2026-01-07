// Package strictyaml provides a strict YAML unmarshaller based on `go-yaml/yaml`
package strictyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Unmarshal takes a byte array and an interface passed by reference. The
// d.Decode will read the next YAML-encoded value from its input and store it in
// the value pointed to by yamlObj. Any config keys from the incoming YAML
// document which do not correspond to expected keys in the config struct will
// result in errors.
//
// TODO(https://github.com/go-yaml/yaml/issues/639): Replace this function with
// yaml.Unmarshal once a more ergonomic way to set unmarshal options is added
// upstream.
func Unmarshal(b []byte, yamlObj interface{}) error {
	r := bytes.NewReader(b)

	d := yaml.NewDecoder(r)
	d.KnownFields(true)

	// d.Decode will mutate yamlObj
	err := d.Decode(yamlObj)

	if err != nil {
		// io.EOF is returned when the YAML document is empty.
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("unmarshalling YAML, bytes cannot be nil: %w", err)
		}
		return fmt.Errorf("unmarshalling YAML: %w", err)
	}

	// As bytes are read by the decoder, the length of the byte buffer should
	// decrease. If it doesn't, there's a problem.
	if r.Len() != 0 {
		return fmt.Errorf("yaml object of size %d bytes had %d bytes of unexpected unconsumed trailers", r.Size(), r.Len())
	}

	return nil
}
