// Package none implements a dummy BlobInfoCache which records no data.
package none

import (
	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/blobinfocache"
	"go.podman.io/image/v5/types"
)

// noCache implements a dummy BlobInfoCache which records no data.
type noCache struct {
}

// NoCache implements BlobInfoCache by not recording any data.
//
// This exists primarily for implementations of configGetter for
// Manifest.Inspect, because configs only have one representation.
// Any use of BlobInfoCache with blobs should usually use at least a
// short-lived cache, ideally blobinfocache.DefaultCache.
var NoCache blobinfocache.BlobInfoCache2 = blobinfocache.FromBlobInfoCache(&noCache{})

// UncompressedDigest returns an uncompressed digest corresponding to anyDigest.
// May return anyDigest if it is known to be uncompressed.
// Returns "" if nothing is known about the digest (it may be compressed or uncompressed).
func (noCache) UncompressedDigest(anyDigest digest.Digest) digest.Digest {
	return ""
}

// RecordDigestUncompressedPair records that the uncompressed version of anyDigest is uncompressed.
// It’s allowed for anyDigest == uncompressed.
// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
func (noCache) RecordDigestUncompressedPair(anyDigest digest.Digest, uncompressed digest.Digest) {
}

// UncompressedDigestForTOC returns an uncompressed digest corresponding to anyDigest.
// Returns "" if the uncompressed digest is unknown.
func (noCache) UncompressedDigestForTOC(tocDigest digest.Digest) digest.Digest {
	return ""
}

// RecordTOCUncompressedPair records that the tocDigest corresponds to uncompressed.
// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
func (noCache) RecordTOCUncompressedPair(tocDigest digest.Digest, uncompressed digest.Digest) {
}

// RecordKnownLocation records that a blob with the specified digest exists within the specified (transport, scope) scope,
// and can be reused given the opaque location data.
func (noCache) RecordKnownLocation(transport types.ImageTransport, scope types.BICTransportScope, blobDigest digest.Digest, location types.BICLocationReference) {
}

// CandidateLocations returns a prioritized, limited, number of blobs and their locations that could possibly be reused
// within the specified (transport scope) (if they still exist, which is not guaranteed).
//
// If !canSubstitute, the returned candidates will match the submitted digest exactly; if canSubstitute,
// data from previous RecordDigestUncompressedPair calls is used to also look up variants of the blob which have the same
// uncompressed digest.
func (noCache) CandidateLocations(transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest, canSubstitute bool) []types.BICReplacementCandidate {
	return nil
}
