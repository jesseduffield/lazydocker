package toc

import (
	"errors"

	digest "github.com/opencontainers/go-digest"
	"go.podman.io/storage/pkg/chunked/internal/minimal"
)

// ChunkedAnnotations contains various annotations that might be set or used by the pkg/chunked-supported
// compression formats.
//
// This set does not define their semantics in detail as a public API.
// The _only_ intended use of this set is: code that _changes_ layer compression to a format
// which is not chunked can/should remove these annotations.
var ChunkedAnnotations = map[string]struct{}{
	minimal.ManifestChecksumKey: {},
	minimal.ManifestInfoKey:     {},
	minimal.TarSplitInfoKey:     {},
	minimal.TarSplitChecksumKey: {}, //nolint:staticcheck // The field is deprecated, so removing it when changing compressionn is all the more desirable.
	tocJSONDigestAnnotation:     {},
}

// tocJSONDigestAnnotation is the annotation key for the digest of the estargz
// TOC JSON.
// It is defined in github.com/containerd/stargz-snapshotter/estargz as TOCJSONDigestAnnotation
// Duplicate it here to avoid a dependency on the package.
const tocJSONDigestAnnotation = "containerd.io/snapshot/stargz/toc.digest"

// GetTOCDigest returns the digest of the TOC as recorded in the annotations.
// This function retrieves a digest that represents the content of a
// table of contents (TOC) from the image's annotations.
// This is an experimental feature and may be changed/removed in the future.
func GetTOCDigest(annotations map[string]string) (*digest.Digest, error) {
	d1, ok1 := annotations[tocJSONDigestAnnotation]
	d2, ok2 := annotations[minimal.ManifestChecksumKey]
	switch {
	case ok1 && ok2:
		return nil, errors.New("both zstd:chunked and eStargz TOC found")
	case ok1:
		d, err := digest.Parse(d1)
		if err != nil {
			return nil, err
		}
		return &d, nil
	case ok2:
		d, err := digest.Parse(d2)
		if err != nil {
			return nil, err
		}
		return &d, nil
	default:
		return nil, nil
	}
}
