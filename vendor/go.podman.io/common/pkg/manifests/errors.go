package manifests

import (
	"errors"
)

var (
	// ErrDigestNotFound is returned when we look for an image instance
	// with a particular digest in a list or index, and fail to find it.
	ErrDigestNotFound = errors.New("no image instance matching the specified digest was found in the list or index")
	// ErrManifestTypeNotSupported is returned when we attempt to parse a
	// manifest with a known MIME type as a list or index, or when we attempt
	// to serialize a list or index to a manifest with a MIME type that we
	// don't know how to encode.
	ErrManifestTypeNotSupported = errors.New("manifest type not supported")
)
