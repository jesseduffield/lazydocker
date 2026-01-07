package impl

import (
	"context"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
)

// Compat implements the obsolete parts of types.ImageSource
// for implementations of private.ImageSource.
// See AddCompat below.
type Compat struct {
	src private.ImageSourceInternalOnly
}

// AddCompat initializes Compat to implement the obsolete parts of types.ImageSource
// for implementations of private.ImageSource.
//
// Use it like this:
//
//	type yourSource struct {
//		impl.Compat
//		…
//	}
//
//	src := &yourSource{…}
//	src.Compat = impl.AddCompat(src)
func AddCompat(src private.ImageSourceInternalOnly) Compat {
	return Compat{src}
}

// GetSignatures returns the image's signatures.  It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
func (c *Compat) GetSignatures(ctx context.Context, instanceDigest *digest.Digest) ([][]byte, error) {
	// Silently ignore signatures with other formats; the caller can’t handle them.
	// Admittedly callers that want to sync all of the image might want to fail instead; this
	// way an upgrade of c/image neither breaks them nor adds new functionality.
	// Alternatively, we could possibly define the old GetSignatures to use the multi-format
	// signature.Blob representation now, in general, but that could silently break them as well.
	sigs, err := c.src.GetSignaturesWithFormat(ctx, instanceDigest)
	if err != nil {
		return nil, err
	}
	simpleSigs := [][]byte{}
	for _, sig := range sigs {
		if sig, ok := sig.(signature.SimpleSigning); ok {
			simpleSigs = append(simpleSigs, sig.UntrustedSignature())
		}
	}
	return simpleSigs, nil
}
