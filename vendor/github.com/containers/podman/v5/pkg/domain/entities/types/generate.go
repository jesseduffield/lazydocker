package types

import (
	"io"
)

type GenerateSystemdReport struct {
	// Units of the generate process. key = unit name -> value = unit content
	Units map[string]string
}

type GenerateKubeReport struct {
	// FIXME: Podman4.0 should change io.Reader to io.ReaderCloser
	// Reader - the io.Reader to reader the generated YAML file.
	Reader io.Reader
}

type GenerateSpecReport struct {
	Data []byte
}
