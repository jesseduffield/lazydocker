// Package memory implements an in-memory BlobInfoCache.
package memory

import (
	"sync"
	"time"

	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/blobinfocache"
	"go.podman.io/image/v5/internal/set"
	"go.podman.io/image/v5/pkg/blobinfocache/internal/prioritize"
	"go.podman.io/image/v5/types"
)

// locationKey only exists to make lookup in knownLocations easier.
type locationKey struct {
	transport  string
	scope      types.BICTransportScope
	blobDigest digest.Digest
}

// cache implements an in-memory-only BlobInfoCache.
type cache struct {
	mutex sync.Mutex
	// The following fields can only be accessed with mutex held.
	uncompressedDigests      map[digest.Digest]digest.Digest
	uncompressedDigestsByTOC map[digest.Digest]digest.Digest
	digestsByUncompressed    map[digest.Digest]*set.Set[digest.Digest]                // stores a set of digests for each uncompressed digest
	knownLocations           map[locationKey]map[types.BICLocationReference]time.Time // stores last known existence time for each location reference
	compressors              map[digest.Digest]blobinfocache.DigestCompressorData     // stores compression data for each digest; BaseVariantCompressor != UnknownCompression
}

// New returns a BlobInfoCache implementation which is in-memory only.
//
// This is primarily intended for tests, but also used as a fallback
// if blobinfocache.DefaultCache can’t determine, or set up, the
// location for a persistent cache.  Most users should use
// blobinfocache.DefaultCache. instead of calling this directly.
// Manual users of types.{ImageSource,ImageDestination} might also use
// this instead of a persistent cache.
func New() types.BlobInfoCache {
	return new2()
}

func new2() *cache {
	return &cache{
		uncompressedDigests:      map[digest.Digest]digest.Digest{},
		uncompressedDigestsByTOC: map[digest.Digest]digest.Digest{},
		digestsByUncompressed:    map[digest.Digest]*set.Set[digest.Digest]{},
		knownLocations:           map[locationKey]map[types.BICLocationReference]time.Time{},
		compressors:              map[digest.Digest]blobinfocache.DigestCompressorData{},
	}
}

// Open() sets up the cache for future accesses, potentially acquiring costly state. Each Open() must be paired with a Close().
// Note that public callers may call the types.BlobInfoCache operations without Open()/Close().
func (mem *cache) Open() {
}

// Close destroys state created by Open().
func (mem *cache) Close() {
}

// UncompressedDigest returns an uncompressed digest corresponding to anyDigest.
// May return anyDigest if it is known to be uncompressed.
// Returns "" if nothing is known about the digest (it may be compressed or uncompressed).
func (mem *cache) UncompressedDigest(anyDigest digest.Digest) digest.Digest {
	mem.mutex.Lock()
	defer mem.mutex.Unlock()
	return mem.uncompressedDigestLocked(anyDigest)
}

// uncompressedDigestLocked implements types.BlobInfoCache.UncompressedDigest, but must be called only with mem.mutex held.
func (mem *cache) uncompressedDigestLocked(anyDigest digest.Digest) digest.Digest {
	if d, ok := mem.uncompressedDigests[anyDigest]; ok {
		return d
	}
	// Presence in digestsByUncompressed implies that anyDigest must already refer to an uncompressed digest.
	// This way we don't have to waste storage space with trivial (uncompressed, uncompressed) mappings
	// when we already record a (compressed, uncompressed) pair.
	if s, ok := mem.digestsByUncompressed[anyDigest]; ok && !s.Empty() {
		return anyDigest
	}
	return ""
}

// RecordDigestUncompressedPair records that the uncompressed version of anyDigest is uncompressed.
// It’s allowed for anyDigest == uncompressed.
// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
func (mem *cache) RecordDigestUncompressedPair(anyDigest digest.Digest, uncompressed digest.Digest) {
	mem.mutex.Lock()
	defer mem.mutex.Unlock()
	if previous, ok := mem.uncompressedDigests[anyDigest]; ok && previous != uncompressed {
		logrus.Warnf("Uncompressed digest for blob %s previously recorded as %s, now %s", anyDigest, previous, uncompressed)
	}
	mem.uncompressedDigests[anyDigest] = uncompressed

	anyDigestSet, ok := mem.digestsByUncompressed[uncompressed]
	if !ok {
		anyDigestSet = set.New[digest.Digest]()
		mem.digestsByUncompressed[uncompressed] = anyDigestSet
	}
	anyDigestSet.Add(anyDigest)
}

// UncompressedDigestForTOC returns an uncompressed digest corresponding to anyDigest.
// Returns "" if the uncompressed digest is unknown.
func (mem *cache) UncompressedDigestForTOC(tocDigest digest.Digest) digest.Digest {
	mem.mutex.Lock()
	defer mem.mutex.Unlock()
	if d, ok := mem.uncompressedDigestsByTOC[tocDigest]; ok {
		return d
	}
	return ""
}

// RecordTOCUncompressedPair records that the tocDigest corresponds to uncompressed.
// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
func (mem *cache) RecordTOCUncompressedPair(tocDigest digest.Digest, uncompressed digest.Digest) {
	mem.mutex.Lock()
	defer mem.mutex.Unlock()
	if previous, ok := mem.uncompressedDigestsByTOC[tocDigest]; ok && previous != uncompressed {
		logrus.Warnf("Uncompressed digest for blob with TOC %q previously recorded as %q, now %q", tocDigest, previous, uncompressed)
	}
	mem.uncompressedDigestsByTOC[tocDigest] = uncompressed
}

// RecordKnownLocation records that a blob with the specified digest exists within the specified (transport, scope) scope,
// and can be reused given the opaque location data.
func (mem *cache) RecordKnownLocation(transport types.ImageTransport, scope types.BICTransportScope, blobDigest digest.Digest, location types.BICLocationReference) {
	mem.mutex.Lock()
	defer mem.mutex.Unlock()
	key := locationKey{transport: transport.Name(), scope: scope, blobDigest: blobDigest}
	locationScope, ok := mem.knownLocations[key]
	if !ok {
		locationScope = map[types.BICLocationReference]time.Time{}
		mem.knownLocations[key] = locationScope
	}
	locationScope[location] = time.Now() // Possibly overwriting an older entry.
}

// RecordDigestCompressorData records data for the blob with the specified digest.
// WARNING: Only call this with LOCALLY VERIFIED data:
//   - don’t record a compressor for a digest just because some remote author claims so
//     (e.g. because a manifest says so);
//   - don’t record the non-base variant or annotations if we are not _sure_ that the base variant
//     and the blob’s digest match the non-base variant’s annotations (e.g. because we saw them
//     in a manifest)
//
// otherwise the cache could be poisoned and cause us to make incorrect edits to type
// information in a manifest.
func (mem *cache) RecordDigestCompressorData(anyDigest digest.Digest, data blobinfocache.DigestCompressorData) {
	mem.mutex.Lock()
	defer mem.mutex.Unlock()
	if previous, ok := mem.compressors[anyDigest]; ok {
		if previous.BaseVariantCompressor != data.BaseVariantCompressor {
			logrus.Warnf("Base compressor for blob with digest %s previously recorded as %s, now %s", anyDigest, previous.BaseVariantCompressor, data.BaseVariantCompressor)
		} else if previous.SpecificVariantCompressor != blobinfocache.UnknownCompression && data.SpecificVariantCompressor != blobinfocache.UnknownCompression &&
			previous.SpecificVariantCompressor != data.SpecificVariantCompressor {
			logrus.Warnf("Specific compressor for blob with digest %s previously recorded as %s, now %s", anyDigest, previous.SpecificVariantCompressor, data.SpecificVariantCompressor)
		}
		// We don’t check SpecificVariantAnnotations for equality, it’s possible that their generation is not deterministic.

		// Preserve specific variant information if the incoming data does not have it.
		if data.BaseVariantCompressor != blobinfocache.UnknownCompression && data.SpecificVariantCompressor == blobinfocache.UnknownCompression &&
			previous.SpecificVariantCompressor != blobinfocache.UnknownCompression {
			data.SpecificVariantCompressor = previous.SpecificVariantCompressor
			data.SpecificVariantAnnotations = previous.SpecificVariantAnnotations
		}
	}
	if data.BaseVariantCompressor == blobinfocache.UnknownCompression {
		delete(mem.compressors, anyDigest)
		return
	}
	mem.compressors[anyDigest] = data
}

// appendReplacementCandidates creates prioritize.CandidateWithTime values for digest in memory
// with corresponding compression info from mem.compressors, and returns the result of appending
// them to candidates.
// v2Options is not nil if the caller is CandidateLocations2: this allows including candidates with unknown location, and filters out candidates
// with unknown compression.
func (mem *cache) appendReplacementCandidates(candidates []prioritize.CandidateWithTime, transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest,
	v2Options *blobinfocache.CandidateLocations2Options) []prioritize.CandidateWithTime {
	compressionData := blobinfocache.DigestCompressorData{
		BaseVariantCompressor:      blobinfocache.UnknownCompression,
		SpecificVariantCompressor:  blobinfocache.UnknownCompression,
		SpecificVariantAnnotations: nil,
	}
	if v, ok := mem.compressors[digest]; ok {
		compressionData = v
	}
	template := prioritize.CandidateTemplateWithCompression(v2Options, digest, compressionData)
	if template == nil {
		return candidates
	}
	locations := mem.knownLocations[locationKey{transport: transport.Name(), scope: scope, blobDigest: digest}] // nil if not present
	if len(locations) > 0 {
		for l, t := range locations {
			candidates = append(candidates, template.CandidateWithLocation(l, t))
		}
	} else if v2Options != nil {
		candidates = append(candidates, template.CandidateWithUnknownLocation())
	}
	return candidates
}

// CandidateLocations returns a prioritized, limited, number of blobs and their locations that could possibly be reused
// within the specified (transport scope) (if they still exist, which is not guaranteed).
//
// If !canSubstitute, the returned candidates will match the submitted digest exactly; if canSubstitute,
// data from previous RecordDigestUncompressedPair calls is used to also look up variants of the blob which have the same
// uncompressed digest.
func (mem *cache) CandidateLocations(transport types.ImageTransport, scope types.BICTransportScope, primaryDigest digest.Digest, canSubstitute bool) []types.BICReplacementCandidate {
	return blobinfocache.CandidateLocationsFromV2(mem.candidateLocations(transport, scope, primaryDigest, canSubstitute, nil))
}

// CandidateLocations2 returns a prioritized, limited, number of blobs and their locations (if known)
// that could possibly be reused within the specified (transport scope) (if they still
// exist, which is not guaranteed).
func (mem *cache) CandidateLocations2(transport types.ImageTransport, scope types.BICTransportScope, primaryDigest digest.Digest, options blobinfocache.CandidateLocations2Options) []blobinfocache.BICReplacementCandidate2 {
	return mem.candidateLocations(transport, scope, primaryDigest, options.CanSubstitute, &options)
}

// candidateLocations implements CandidateLocations / CandidateLocations2.
// v2Options is not nil if the caller is CandidateLocations2.
func (mem *cache) candidateLocations(transport types.ImageTransport, scope types.BICTransportScope, primaryDigest digest.Digest, canSubstitute bool,
	v2Options *blobinfocache.CandidateLocations2Options) []blobinfocache.BICReplacementCandidate2 {
	mem.mutex.Lock()
	defer mem.mutex.Unlock()
	res := []prioritize.CandidateWithTime{}
	res = mem.appendReplacementCandidates(res, transport, scope, primaryDigest, v2Options)
	var uncompressedDigest digest.Digest // = ""
	if canSubstitute {
		if uncompressedDigest = mem.uncompressedDigestLocked(primaryDigest); uncompressedDigest != "" {
			otherDigests := mem.digestsByUncompressed[uncompressedDigest] // nil if not present in the map
			if otherDigests != nil {
				for d := range otherDigests.All() {
					if d != primaryDigest && d != uncompressedDigest {
						res = mem.appendReplacementCandidates(res, transport, scope, d, v2Options)
					}
				}
			}
			if uncompressedDigest != primaryDigest {
				res = mem.appendReplacementCandidates(res, transport, scope, uncompressedDigest, v2Options)
			}
		}
	}
	return prioritize.DestructivelyPrioritizeReplacementCandidates(res, primaryDigest, uncompressedDigest)
}
