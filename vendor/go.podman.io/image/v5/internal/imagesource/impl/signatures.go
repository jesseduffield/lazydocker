package impl

import (
	"context"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/signature"
)

// NoSignatures implements GetSignatures() that returns nothing.
type NoSignatures struct{}

// GetSignaturesWithFormat returns the image's signatures.  It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
func (stub NoSignatures) GetSignaturesWithFormat(ctx context.Context, instanceDigest *digest.Digest) ([]signature.Signature, error) {
	return nil, nil
}
