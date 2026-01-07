// Package prioritize provides utilities for filtering and prioritizing locations in
// types.BlobInfoCache.CandidateLocations.
package prioritize

import (
	"cmp"
	"slices"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/blobinfocache"
	"go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/types"
)

// replacementAttempts is the number of blob replacement candidates with known location returned by destructivelyPrioritizeReplacementCandidates,
// and therefore ultimately by types.BlobInfoCache.CandidateLocations.
// This is a heuristic/guess, and could well use a different value.
const replacementAttempts = 5

// replacementUnknownLocationAttempts is the number of blob replacement candidates with unknown Location returned by destructivelyPrioritizeReplacementCandidates,
// and therefore ultimately by blobinfocache.BlobInfoCache2.CandidateLocations2.
// This is a heuristic/guess, and could well use a different value.
const replacementUnknownLocationAttempts = 2

// CandidateTemplate is a subset of BICReplacementCandidate2 with data related to a specific digest,
// which can be later combined with information about a location.
type CandidateTemplate struct {
	digest                 digest.Digest
	compressionOperation   types.LayerCompression // Either types.Decompress for uncompressed, or types.Compress for compressed
	compressionAlgorithm   *compression.Algorithm // An algorithm when the candidate is compressed, or nil when it is uncompressed
	compressionAnnotations map[string]string      // If necessary, annotations necessary to use compressionAlgorithm
}

// CandidateTemplateWithCompression returns a CandidateTemplate if a blob with data is acceptable
// for a CandidateLocations* call with v2Options.
//
// v2Options can be set to nil if the call is CandidateLocations (i.e. compression is not required to be known);
// if not nil, the call is assumed to be CandidateLocations2.
func CandidateTemplateWithCompression(v2Options *blobinfocache.CandidateLocations2Options, digest digest.Digest, data blobinfocache.DigestCompressorData) *CandidateTemplate {
	if v2Options == nil {
		return &CandidateTemplate{ // Anything goes. The compressionOperation, compressionAlgorithm and compressionAnnotations values are not used.
			digest: digest,
		}
	}

	requiredCompression := "nil"
	if v2Options.RequiredCompression != nil {
		requiredCompression = v2Options.RequiredCompression.Name()
	}
	switch data.BaseVariantCompressor {
	case blobinfocache.Uncompressed:
		if !manifest.CandidateCompressionMatchesReuseConditions(manifest.ReuseConditions{
			PossibleManifestFormats: v2Options.PossibleManifestFormats,
			RequiredCompression:     v2Options.RequiredCompression,
		}, nil) {
			logrus.Debugf("Ignoring BlobInfoCache record of digest %q, uncompressed format does not match required %s or MIME types %#v",
				digest.String(), requiredCompression, v2Options.PossibleManifestFormats)
			return nil
		}
		return &CandidateTemplate{
			digest:                 digest,
			compressionOperation:   types.Decompress,
			compressionAlgorithm:   nil,
			compressionAnnotations: nil,
		}
	case blobinfocache.UnknownCompression:
		logrus.Debugf("Ignoring BlobInfoCache record of digest %q with unknown compression", digest.String())
		return nil // Not allowed with CandidateLocations2
	default:
		// See if we can use the specific variant, first.
		if data.SpecificVariantCompressor != blobinfocache.UnknownCompression {
			algo, err := compression.AlgorithmByName(data.SpecificVariantCompressor)
			if err != nil {
				logrus.Debugf("Not considering unrecognized specific compression variant %q for BlobInfoCache record of digest %q: %v",
					data.SpecificVariantCompressor, digest.String(), err)
			} else {
				if !manifest.CandidateCompressionMatchesReuseConditions(manifest.ReuseConditions{
					PossibleManifestFormats: v2Options.PossibleManifestFormats,
					RequiredCompression:     v2Options.RequiredCompression,
				}, &algo) {
					logrus.Debugf("Ignoring specific compression variant %q for BlobInfoCache record of digest %q, it does not match required %s or MIME types %#v",
						data.SpecificVariantCompressor, digest.String(), requiredCompression, v2Options.PossibleManifestFormats)
				} else {
					return &CandidateTemplate{
						digest:                 digest,
						compressionOperation:   types.Compress,
						compressionAlgorithm:   &algo,
						compressionAnnotations: data.SpecificVariantAnnotations,
					}
				}
			}
		}

		// Try the base variant.
		algo, err := compression.AlgorithmByName(data.BaseVariantCompressor)
		if err != nil {
			logrus.Debugf("Ignoring BlobInfoCache record of digest %q with unrecognized compression %q: %v",
				digest.String(), data.BaseVariantCompressor, err)
			return nil // The BICReplacementCandidate2.CompressionAlgorithm field is required
		}
		if !manifest.CandidateCompressionMatchesReuseConditions(manifest.ReuseConditions{
			PossibleManifestFormats: v2Options.PossibleManifestFormats,
			RequiredCompression:     v2Options.RequiredCompression,
		}, &algo) {
			logrus.Debugf("Ignoring BlobInfoCache record of digest %q, compression %q does not match required %s or MIME types %#v",
				digest.String(), data.BaseVariantCompressor, requiredCompression, v2Options.PossibleManifestFormats)
			return nil
		}
		return &CandidateTemplate{
			digest:                 digest,
			compressionOperation:   types.Compress,
			compressionAlgorithm:   &algo,
			compressionAnnotations: nil,
		}
	}
}

// CandidateWithTime is the input to types.BICReplacementCandidate prioritization.
type CandidateWithTime struct {
	candidate blobinfocache.BICReplacementCandidate2 // The replacement candidate
	lastSeen  time.Time                              // Time the candidate was last known to exist (either read or written) (not set for Candidate.UnknownLocation)
}

// CandidateWithLocation returns a complete CandidateWithTime combining (template from CandidateTemplateWithCompression, location, lastSeen)
func (template CandidateTemplate) CandidateWithLocation(location types.BICLocationReference, lastSeen time.Time) CandidateWithTime {
	return CandidateWithTime{
		candidate: blobinfocache.BICReplacementCandidate2{
			Digest:                 template.digest,
			CompressionOperation:   template.compressionOperation,
			CompressionAlgorithm:   template.compressionAlgorithm,
			CompressionAnnotations: template.compressionAnnotations,
			UnknownLocation:        false,
			Location:               location,
		},
		lastSeen: lastSeen,
	}
}

// CandidateWithUnknownLocation returns a complete CandidateWithTime for a template from CandidateTemplateWithCompression and an unknown location.
func (template CandidateTemplate) CandidateWithUnknownLocation() CandidateWithTime {
	return CandidateWithTime{
		candidate: blobinfocache.BICReplacementCandidate2{
			Digest:                 template.digest,
			CompressionOperation:   template.compressionOperation,
			CompressionAlgorithm:   template.compressionAlgorithm,
			CompressionAnnotations: template.compressionAnnotations,
			UnknownLocation:        true,
			Location:               types.BICLocationReference{Opaque: ""},
		},
		lastSeen: time.Time{},
	}
}

// candidateSortState is a closure for a comparison used by slices.SortFunc on candidates to prioritize,
// along with the specially-treated digest values relevant to the ordering.
type candidateSortState struct {
	primaryDigest      digest.Digest // The digest the user actually asked for
	uncompressedDigest digest.Digest // The uncompressed digest corresponding to primaryDigest. May be "", or even equal to primaryDigest
}

func (css *candidateSortState) compare(xi, xj CandidateWithTime) int {
	// primaryDigest entries come first, more recent first.
	// uncompressedDigest entries, if uncompressedDigest is set and != primaryDigest, come last, more recent entry first.
	// Other digest values are primarily sorted by time (more recent first), secondarily by digest (to provide a deterministic order)

	// First, deal with the primaryDigest/uncompressedDigest cases:
	if xi.candidate.Digest != xj.candidate.Digest {
		// - The two digests are different, and one (or both) of the digests is primaryDigest or uncompressedDigest: time does not matter
		if xi.candidate.Digest == css.primaryDigest {
			return -1
		}
		if xj.candidate.Digest == css.primaryDigest {
			return 1
		}
		if css.uncompressedDigest != "" {
			if xi.candidate.Digest == css.uncompressedDigest {
				return 1
			}
			if xj.candidate.Digest == css.uncompressedDigest {
				return -1
			}
		}
	} else { // xi.Candidate.Digest == xj.Candidate.Digest
		// The two digests are the same, and are either primaryDigest or uncompressedDigest: order by time
		if xi.candidate.Digest == css.primaryDigest || (css.uncompressedDigest != "" && xi.candidate.Digest == css.uncompressedDigest) {
			return -xi.lastSeen.Compare(xj.lastSeen)
		}
	}

	// Neither of the digests are primaryDigest/uncompressedDigest:
	if cmp := xi.lastSeen.Compare(xj.lastSeen); cmp != 0 { // Order primarily by time
		return -cmp
	}
	// Fall back to digest, if timestamps end up _exactly_ the same (how?!)
	return cmp.Compare(xi.candidate.Digest, xj.candidate.Digest)
}

// destructivelyPrioritizeReplacementCandidatesWithMax is destructivelyPrioritizeReplacementCandidates with parameters for the
// number of entries to limit for known and unknown location separately, only to make testing simpler.
func destructivelyPrioritizeReplacementCandidatesWithMax(cs []CandidateWithTime, primaryDigest, uncompressedDigest digest.Digest, totalLimit int, noLocationLimit int) []blobinfocache.BICReplacementCandidate2 {
	// split unknown candidates and known candidates
	// and limit them separately.
	var knownLocationCandidates []CandidateWithTime
	var unknownLocationCandidates []CandidateWithTime
	// We don't need to use sort.Stable() because nanosecond timestamps are (presumably?) unique, so no two elements should
	// compare equal.
	slices.SortFunc(cs, (&candidateSortState{
		primaryDigest:      primaryDigest,
		uncompressedDigest: uncompressedDigest,
	}).compare)
	for _, candidate := range cs {
		if candidate.candidate.UnknownLocation {
			unknownLocationCandidates = append(unknownLocationCandidates, candidate)
		} else {
			knownLocationCandidates = append(knownLocationCandidates, candidate)
		}
	}

	knownLocationCandidatesUsed := min(len(knownLocationCandidates), totalLimit)
	remainingCapacity := totalLimit - knownLocationCandidatesUsed
	unknownLocationCandidatesUsed := min(noLocationLimit, remainingCapacity, len(unknownLocationCandidates))
	res := make([]blobinfocache.BICReplacementCandidate2, knownLocationCandidatesUsed)
	for i := 0; i < knownLocationCandidatesUsed; i++ {
		res[i] = knownLocationCandidates[i].candidate
	}
	// If candidates with unknown location are found, lets add them to final list
	for i := 0; i < unknownLocationCandidatesUsed; i++ {
		res = append(res, unknownLocationCandidates[i].candidate)
	}
	return res
}

// DestructivelyPrioritizeReplacementCandidates consumes AND DESTROYS an array of possible replacement candidates with their last known existence times,
// the primary digest the user actually asked for, the corresponding uncompressed digest (if known, possibly equal to the primary digest) returns an
// appropriately prioritized and/or trimmed result suitable for a return value from types.BlobInfoCache.CandidateLocations.
//
// WARNING: The array of candidates is destructively modified. (The implementation of this function could of course
// make a copy, but all CandidateLocations implementations build the slice of candidates only for the single purpose of calling this function anyway.)
func DestructivelyPrioritizeReplacementCandidates(cs []CandidateWithTime, primaryDigest, uncompressedDigest digest.Digest) []blobinfocache.BICReplacementCandidate2 {
	return destructivelyPrioritizeReplacementCandidatesWithMax(cs, primaryDigest, uncompressedDigest, replacementAttempts, replacementUnknownLocationAttempts)
}
