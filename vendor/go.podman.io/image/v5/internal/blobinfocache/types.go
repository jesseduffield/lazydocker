package blobinfocache

import (
	digest "github.com/opencontainers/go-digest"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

const (
	// Uncompressed is the value we store in a blob info cache to indicate that we know that
	// the blob in the corresponding location is not compressed.
	Uncompressed = "uncompressed"
	// UnknownCompression is the value we store in a blob info cache to indicate that we don't
	// know if the blob in the corresponding location is compressed (and if so, how) or not.
	UnknownCompression = "unknown"
)

// BlobInfoCache2 extends BlobInfoCache by adding the ability to track information about what kind
// of compression was applied to the blobs it keeps information about.
type BlobInfoCache2 interface {
	types.BlobInfoCache

	// Open() sets up the cache for future accesses, potentially acquiring costly state. Each Open() must be paired with a Close().
	// Note that public callers may call the types.BlobInfoCache operations without Open()/Close().
	Open()
	// Close destroys state created by Open().
	Close()

	// UncompressedDigestForTOC returns an uncompressed digest corresponding to anyDigest.
	// Returns "" if the uncompressed digest is unknown.
	UncompressedDigestForTOC(tocDigest digest.Digest) digest.Digest
	// RecordTOCUncompressedPair records that the tocDigest corresponds to uncompressed.
	// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
	// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
	// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
	RecordTOCUncompressedPair(tocDigest digest.Digest, uncompressed digest.Digest)

	// RecordDigestCompressorData records data for the blob with the specified digest.
	// WARNING: Only call this with LOCALLY VERIFIED data:
	//  - don’t record a compressor for a digest just because some remote author claims so
	//    (e.g. because a manifest says so);
	//  - don’t record the non-base variant or annotations if we are not _sure_ that the base variant
	//    and the blob’s digest match the non-base variant’s annotations (e.g. because we saw them
	//    in a manifest)
	// otherwise the cache could be poisoned and cause us to make incorrect edits to type
	// information in a manifest.
	RecordDigestCompressorData(anyDigest digest.Digest, data DigestCompressorData)
	// CandidateLocations2 returns a prioritized, limited, number of blobs and their locations (if known)
	// that could possibly be reused within the specified (transport scope) (if they still
	// exist, which is not guaranteed).
	CandidateLocations2(transport types.ImageTransport, scope types.BICTransportScope, digest digest.Digest, options CandidateLocations2Options) []BICReplacementCandidate2
}

// DigestCompressorData is information known about how a blob is compressed.
// (This is worded generically, but basically targeted at the zstd / zstd:chunked situation.)
type DigestCompressorData struct {
	BaseVariantCompressor string // A compressor’s base variant name, or Uncompressed or UnknownCompression.
	// The following fields are only valid if the base variant is neither Uncompressed nor UnknownCompression:
	SpecificVariantCompressor  string            // A non-base variant compressor (or UnknownCompression if the true format is just the base variant)
	SpecificVariantAnnotations map[string]string // Annotations required to benefit from the base variant.
}

// CandidateLocations2Options are used in CandidateLocations2.
type CandidateLocations2Options struct {
	// If !CanSubstitute, the returned candidates will match the submitted digest exactly; if
	// CanSubstitute, data from previous RecordDigestUncompressedPair calls is used to also look
	// up variants of the blob which have the same uncompressed digest.
	CanSubstitute           bool
	PossibleManifestFormats []string                    // If set, a set of possible manifest formats; at least one should support the reused layer
	RequiredCompression     *compressiontypes.Algorithm // If set, only reuse layers with a matching algorithm
}

// BICReplacementCandidate2 is an item returned by BlobInfoCache2.CandidateLocations2.
type BICReplacementCandidate2 struct {
	Digest                 digest.Digest
	CompressionOperation   types.LayerCompression      // Either types.Decompress for uncompressed, or types.Compress for compressed
	CompressionAlgorithm   *compressiontypes.Algorithm // An algorithm when the candidate is compressed, or nil when it is uncompressed
	CompressionAnnotations map[string]string           // If necessary, annotations necessary to use CompressionAlgorithm
	UnknownLocation        bool                        // is true when `Location` for this blob is not set
	Location               types.BICLocationReference  // not set if UnknownLocation is set to `true`
}
