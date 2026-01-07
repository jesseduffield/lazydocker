package imagesource

import (
	"context"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/imagesource/stubs"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/types"
)

// wrapped provides the private.ImageSource operations
// for a source that only implements types.ImageSource
type wrapped struct {
	stubs.NoGetBlobAtInitialize

	types.ImageSource
}

// FromPublic(src) returns an object that provides the private.ImageSource API
//
// Eventually, we might want to expose this function, and methods of the returned object,
// as a public API (or rather, a variant that does not include the already-superseded
// methods of types.ImageSource, and has added more future-proofing), and more strongly
// deprecate direct use of types.ImageSource.
//
// NOTE: The returned API MUST NOT be a public interface (it can be either just a struct
// with public methods, or perhaps a private interface), so that we can add methods
// without breaking any external implementers of a public interface.
func FromPublic(src types.ImageSource) private.ImageSource {
	if src2, ok := src.(private.ImageSource); ok {
		return src2
	}
	return &wrapped{
		NoGetBlobAtInitialize: stubs.NoGetBlobAt(src.Reference()),

		ImageSource: src,
	}
}

// GetSignaturesWithFormat returns the image's signatures.  It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
func (w *wrapped) GetSignaturesWithFormat(ctx context.Context, instanceDigest *digest.Digest) ([]signature.Signature, error) {
	sigs, err := w.GetSignatures(ctx, instanceDigest)
	if err != nil {
		return nil, err
	}
	res := []signature.Signature{}
	for _, sig := range sigs {
		res = append(res, signature.SimpleSigningFromBlob(sig))
	}
	return res, nil
}
