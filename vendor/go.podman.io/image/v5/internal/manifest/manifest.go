package manifest

import (
	"encoding/json"
	"slices"

	"github.com/containers/libtrust"
	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
)

// FIXME: Should we just use docker/distribution and docker/docker implementations directly?

// FIXME(runcom, mitr): should we have a mediatype pkg??
const (
	// DockerV2Schema1MediaType MIME type represents Docker manifest schema 1
	DockerV2Schema1MediaType = "application/vnd.docker.distribution.manifest.v1+json"
	// DockerV2Schema1SignedMediaType MIME type represents Docker manifest schema 1 with a JWS signature
	DockerV2Schema1SignedMediaType = "application/vnd.docker.distribution.manifest.v1+prettyjws"
	// DockerV2Schema2MediaType MIME type represents Docker manifest schema 2
	DockerV2Schema2MediaType = "application/vnd.docker.distribution.manifest.v2+json"
	// DockerV2Schema2ConfigMediaType is the MIME type used for schema 2 config blobs.
	DockerV2Schema2ConfigMediaType = "application/vnd.docker.container.image.v1+json"
	// DockerV2Schema2LayerMediaType is the MIME type used for schema 2 layers.
	DockerV2Schema2LayerMediaType = "application/vnd.docker.image.rootfs.diff.tar.gzip"
	// DockerV2SchemaLayerMediaTypeUncompressed is the mediaType used for uncompressed layers.
	DockerV2SchemaLayerMediaTypeUncompressed = "application/vnd.docker.image.rootfs.diff.tar"
	// DockerV2Schema2LayerMediaType is the MIME type used for schema 2 layers.
	DockerV2SchemaLayerMediaTypeZstd = "application/vnd.docker.image.rootfs.diff.tar.zstd"
	// DockerV2ListMediaType MIME type represents Docker manifest schema 2 list
	DockerV2ListMediaType = "application/vnd.docker.distribution.manifest.list.v2+json"
	// DockerV2Schema2ForeignLayerMediaType is the MIME type used for schema 2 foreign layers.
	DockerV2Schema2ForeignLayerMediaType = "application/vnd.docker.image.rootfs.foreign.diff.tar"
	// DockerV2Schema2ForeignLayerMediaType is the MIME type used for gzipped schema 2 foreign layers.
	DockerV2Schema2ForeignLayerMediaTypeGzip = "application/vnd.docker.image.rootfs.foreign.diff.tar.gzip"
)

// GuessMIMEType guesses MIME type of a manifest and returns it _if it is recognized_, or "" if unknown or unrecognized.
// FIXME? We should, in general, prefer out-of-band MIME type instead of blindly parsing the manifest,
// but we may not have such metadata available (e.g. when the manifest is a local file).
// This is publicly visible as c/image/manifest.GuessMIMEType.
func GuessMIMEType(manifest []byte) string {
	// A subset of manifest fields; the rest is silently ignored by json.Unmarshal.
	// Also docker/distribution/manifest.Versioned.
	meta := struct {
		MediaType     string `json:"mediaType"`
		SchemaVersion int    `json:"schemaVersion"`
		Signatures    any    `json:"signatures"`
	}{}
	if err := json.Unmarshal(manifest, &meta); err != nil {
		return ""
	}

	switch meta.MediaType {
	case DockerV2Schema2MediaType, DockerV2ListMediaType,
		imgspecv1.MediaTypeImageManifest, imgspecv1.MediaTypeImageIndex: // A recognized type.
		return meta.MediaType
	}
	// this is the only way the function can return DockerV2Schema1MediaType, and recognizing that is essential for stripping the JWS signatures = computing the correct manifest digest.
	switch meta.SchemaVersion {
	case 1:
		if meta.Signatures != nil {
			return DockerV2Schema1SignedMediaType
		}
		return DockerV2Schema1MediaType
	case 2:
		// Best effort to understand if this is an OCI image since mediaType
		// wasn't in the manifest for OCI image-spec < 1.0.2.
		// For docker v2s2 meta.MediaType should have been set. But given the data, this is our best guess.
		ociMan := struct {
			Config struct {
				MediaType string `json:"mediaType"`
			} `json:"config"`
		}{}
		if err := json.Unmarshal(manifest, &ociMan); err != nil {
			return ""
		}
		switch ociMan.Config.MediaType {
		case imgspecv1.MediaTypeImageConfig:
			return imgspecv1.MediaTypeImageManifest
		case DockerV2Schema2ConfigMediaType:
			// This case should not happen since a Docker image
			// must declare a top-level media type and
			// `meta.MediaType` has already been checked.
			return DockerV2Schema2MediaType
		}
		// Maybe an image index or an OCI artifact.
		ociIndex := struct {
			Manifests []imgspecv1.Descriptor `json:"manifests"`
		}{}
		if err := json.Unmarshal(manifest, &ociIndex); err != nil {
			return ""
		}
		if len(ociIndex.Manifests) != 0 {
			if ociMan.Config.MediaType == "" {
				return imgspecv1.MediaTypeImageIndex
			}
			// FIXME: this is mixing media types of manifests and configs.
			return ociMan.Config.MediaType
		}
		// It's most likely an OCI artifact with a custom config media
		// type which is not (and cannot) be covered by the media-type
		// checks cabove.
		return imgspecv1.MediaTypeImageManifest
	}
	return ""
}

// Digest returns the a digest of a docker manifest, with any necessary implied transformations like stripping v1s1 signatures.
// This is publicly visible as c/image/manifest.Digest.
func Digest(manifest []byte) (digest.Digest, error) {
	if GuessMIMEType(manifest) == DockerV2Schema1SignedMediaType {
		sig, err := libtrust.ParsePrettySignature(manifest, "signatures")
		if err != nil {
			return "", err
		}
		manifest, err = sig.Payload()
		if err != nil {
			// Coverage: This should never happen, libtrust's Payload() can fail only if joseBase64UrlDecode() fails, on a string
			// that libtrust itself has josebase64UrlEncode()d
			return "", err
		}
	}

	return digest.FromBytes(manifest), nil
}

// MatchesDigest returns true iff the manifest matches expectedDigest.
// Error may be set if this returns false.
// Note that this is not doing ConstantTimeCompare; by the time we get here, the cryptographic signature must already have been verified,
// or we are not using a cryptographic channel and the attacker can modify the digest along with the manifest blob.
// This is publicly visible as c/image/manifest.MatchesDigest.
func MatchesDigest(manifest []byte, expectedDigest digest.Digest) (bool, error) {
	// This should eventually support various digest types.
	actualDigest, err := Digest(manifest)
	if err != nil {
		return false, err
	}
	return expectedDigest == actualDigest, nil
}

// NormalizedMIMEType returns the effective MIME type of a manifest MIME type returned by a server,
// centralizing various workarounds.
// This is publicly visible as c/image/manifest.NormalizedMIMEType.
func NormalizedMIMEType(input string) string {
	switch input {
	// "application/json" is a valid v2s1 value per https://github.com/docker/distribution/blob/master/docs/spec/manifest-v2-1.md .
	// This works for now, when nothing else seems to return "application/json"; if that were not true, the mapping/detection might
	// need to happen within the ImageSource.
	case "application/json":
		return DockerV2Schema1SignedMediaType
	case DockerV2Schema1MediaType, DockerV2Schema1SignedMediaType,
		imgspecv1.MediaTypeImageManifest,
		imgspecv1.MediaTypeImageIndex,
		DockerV2Schema2MediaType,
		DockerV2ListMediaType:
		return input
	default:
		// If it's not a recognized manifest media type, or we have failed determining the type, we'll try one last time
		// to deserialize using v2s1 as per https://github.com/docker/distribution/blob/master/manifests.go#L108
		// and https://github.com/docker/distribution/blob/master/manifest/schema1/manifest.go#L50
		//
		// Crane registries can also return "text/plain", or pretty much anything else depending on a file extension “recognized” in the tag.
		// This makes no real sense, but it happens
		// because requests for manifests are
		// redirected to a content distribution
		// network which is configured that way. See https://bugzilla.redhat.com/show_bug.cgi?id=1389442
		return DockerV2Schema1SignedMediaType
	}
}

// CompressionAlgorithmIsUniversallySupported returns true if MIMETypeSupportsCompressionAlgorithm(mimeType, algo) returns true for all mimeType values.
func CompressionAlgorithmIsUniversallySupported(algo compressiontypes.Algorithm) bool {
	// Compare the discussion about BaseVariantName in MIMETypeSupportsCompressionAlgorithm().
	switch algo.Name() {
	case compressiontypes.GzipAlgorithmName:
		return true
	default:
		return false
	}
}

// MIMETypeSupportsCompressionAlgorithm returns true if mimeType can represent algo.
func MIMETypeSupportsCompressionAlgorithm(mimeType string, algo compressiontypes.Algorithm) bool {
	if CompressionAlgorithmIsUniversallySupported(algo) {
		return true
	}
	// This does not use BaseVariantName: Plausibly a manifest format might support zstd but not have annotation fields.
	// The logic might have to be more complex (and more ad-hoc) if more manifest formats, with more capabilities, emerge.
	switch algo.Name() {
	case compressiontypes.ZstdAlgorithmName, compressiontypes.ZstdChunkedAlgorithmName:
		return mimeType == imgspecv1.MediaTypeImageManifest
	default: // Includes Bzip2AlgorithmName and XzAlgorithmName, which are defined names but are not supported anywhere
		return false
	}
}

// ReuseConditions are an input to CandidateCompressionMatchesReuseConditions;
// it is a struct to allow longer and better-documented field names.
type ReuseConditions struct {
	PossibleManifestFormats []string                    // If set, a set of possible manifest formats; at least one should support the reused layer
	RequiredCompression     *compressiontypes.Algorithm // If set, only reuse layers with a matching algorithm
}

// CandidateCompressionMatchesReuseConditions returns true if a layer with candidateCompression
// (which can be nil to represent uncompressed or unknown) matches reuseConditions.
func CandidateCompressionMatchesReuseConditions(c ReuseConditions, candidateCompression *compressiontypes.Algorithm) bool {
	if c.RequiredCompression != nil {
		if candidateCompression == nil ||
			(c.RequiredCompression.Name() != candidateCompression.Name() && c.RequiredCompression.Name() != candidateCompression.BaseVariantName()) {
			return false
		}
	}

	// For candidateCompression == nil, we can’t tell the difference between “uncompressed” and “unknown”;
	// and “uncompressed” is acceptable in all known formats (well, it seems to work in practice for schema1),
	// so don’t impose any restrictions if candidateCompression == nil
	if c.PossibleManifestFormats != nil && candidateCompression != nil {
		if !slices.ContainsFunc(c.PossibleManifestFormats, func(mt string) bool {
			return MIMETypeSupportsCompressionAlgorithm(mt, *candidateCompression)
		}) {
			return false
		}
	}

	return true
}
