package blobinfocache

import (
	digest "github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/types"
)

// FromBlobInfoCache returns a BlobInfoCache2 based on a BlobInfoCache, returning the original
// object if it implements BlobInfoCache2, or a wrapper which discards compression information
// if it only implements BlobInfoCache.
func FromBlobInfoCache(bic types.BlobInfoCache) BlobInfoCache2 {
	if bic2, ok := bic.(BlobInfoCache2); ok {
		return bic2
	}
	return &v1OnlyBlobInfoCache{
		BlobInfoCache: bic,
	}
}

type v1OnlyBlobInfoCache struct {
	types.BlobInfoCache
}

func (bic *v1OnlyBlobInfoCache) Open() {
}

func (bic *v1OnlyBlobInfoCache) Close() {
}

func (bic *v1OnlyBlobInfoCache) UncompressedDigestForTOC(tocDigest digest.Digest) digest.Digest {
	return ""
}

func (bic *v1OnlyBlobInfoCache) RecordTOCUncompressedPair(tocDigest digest.Digest, uncompressed digest.Digest) {
}

func (bic *v1OnlyBlobInfoCache) RecordDigestCompressorData(anyDigest digest.Digest, data DigestCompressorData) {
}

func (bic *v1OnlyBlobInfoCache) CandidateLocations2(transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest, options CandidateLocations2Options) []BICReplacementCandidate2 {
	return nil
}

// CandidateLocationsFromV2 converts a slice of BICReplacementCandidate2 to a slice of
// types.BICReplacementCandidate, dropping compression information.
func CandidateLocationsFromV2(v2candidates []BICReplacementCandidate2) []types.BICReplacementCandidate {
	candidates := make([]types.BICReplacementCandidate, 0, len(v2candidates))
	for _, c := range v2candidates {
		candidates = append(candidates, types.BICReplacementCandidate{
			Digest:   c.Digest,
			Location: c.Location,
		})
	}
	return candidates
}
