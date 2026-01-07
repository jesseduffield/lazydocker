package stubs

import (
	"context"
	"errors"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/signature"
)

// NoSignaturesInitialize implements parts of private.ImageDestination
// for transports that don’t support storing signatures.
// See NoSignatures() below.
type NoSignaturesInitialize struct {
	message string
}

// NoSignatures creates a NoSignaturesInitialize, failing with message.
func NoSignatures(message string) NoSignaturesInitialize {
	return NoSignaturesInitialize{
		message: message,
	}
}

// SupportsSignatures returns an error (to be displayed to the user) if the destination certainly can't store signatures.
// Note: It is still possible for PutSignatures to fail if SupportsSignatures returns nil.
func (stub NoSignaturesInitialize) SupportsSignatures(ctx context.Context) error {
	return errors.New(stub.message)
}

// PutSignaturesWithFormat writes a set of signatures to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// MUST be called after PutManifest (signatures may reference manifest contents).
func (stub NoSignaturesInitialize) PutSignaturesWithFormat(ctx context.Context, signatures []signature.Signature, instanceDigest *digest.Digest) error {
	if len(signatures) != 0 {
		return errors.New(stub.message)
	}
	return nil
}

// SupportsSignatures implements SupportsSignatures() that returns nil.
// Note that it might be even more useful to return a value dynamically detected based on
type AlwaysSupportsSignatures struct{}

// SupportsSignatures returns an error (to be displayed to the user) if the destination certainly can't store signatures.
// Note: It is still possible for PutSignatures to fail if SupportsSignatures returns nil.
func (stub AlwaysSupportsSignatures) SupportsSignatures(ctx context.Context) error {
	return nil
}
