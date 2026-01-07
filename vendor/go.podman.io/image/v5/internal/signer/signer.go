package signer

import (
	"context"

	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/signature"
)

// Signer is an object, possibly carrying state, that can be used by copy.Image to sign one or more container images.
// This type is visible to external callers, so it has no public fields or methods apart from Close().
//
// The owner of a Signer must call Close() when done.
type Signer struct {
	implementation SignerImplementation
}

// NewSigner creates a public Signer from a SignerImplementation
func NewSigner(impl SignerImplementation) *Signer {
	return &Signer{implementation: impl}
}

func (s *Signer) Close() error {
	return s.implementation.Close()
}

// ProgressMessage returns a human-readable sentence that makes sense to write before starting to create a single signature.
// Alternatively, should SignImageManifest be provided a logging writer of some kind?
func ProgressMessage(signer *Signer) string {
	return signer.implementation.ProgressMessage()
}

// SignImageManifest invokes a SignerImplementation.
// This is a function, not a method, so that it can only be called by code that is allowed to import this internal subpackage.
func SignImageManifest(ctx context.Context, signer *Signer, manifest []byte, dockerReference reference.Named) (signature.Signature, error) {
	return signer.implementation.SignImageManifest(ctx, manifest, dockerReference)
}

// SignerImplementation is an object, possibly carrying state, that can be used by copy.Image to sign one or more container images.
// This interface is distinct from Signer so that implementations can be created outside of this package.
type SignerImplementation interface {
	// ProgressMessage returns a human-readable sentence that makes sense to write before starting to create a single signature.
	ProgressMessage() string
	// SignImageManifest creates a new signature for manifest m as dockerReference.
	SignImageManifest(ctx context.Context, m []byte, dockerReference reference.Named) (signature.Signature, error)
	Close() error
}
