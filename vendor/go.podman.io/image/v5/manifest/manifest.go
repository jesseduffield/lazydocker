package manifest

import (
	"fmt"

	"github.com/containers/libtrust"
	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/types"
)

// FIXME: Should we just use docker/distribution and docker/docker implementations directly?

// FIXME(runcom, mitr): should we have a mediatype pkg??
const (
	// DockerV2Schema1MediaType MIME type represents Docker manifest schema 1
	DockerV2Schema1MediaType = manifest.DockerV2Schema1MediaType
	// DockerV2Schema1SignedMediaType MIME type represents Docker manifest schema 1 with a JWS signature
	DockerV2Schema1SignedMediaType = manifest.DockerV2Schema1SignedMediaType
	// DockerV2Schema2MediaType MIME type represents Docker manifest schema 2
	DockerV2Schema2MediaType = manifest.DockerV2Schema2MediaType
	// DockerV2Schema2ConfigMediaType is the MIME type used for schema 2 config blobs.
	DockerV2Schema2ConfigMediaType = manifest.DockerV2Schema2ConfigMediaType
	// DockerV2Schema2LayerMediaType is the MIME type used for schema 2 layers.
	DockerV2Schema2LayerMediaType = manifest.DockerV2Schema2LayerMediaType
	// DockerV2SchemaLayerMediaTypeUncompressed is the mediaType used for uncompressed layers.
	DockerV2SchemaLayerMediaTypeUncompressed = manifest.DockerV2SchemaLayerMediaTypeUncompressed
	// DockerV2SchemaLayerMediaTypeZstd is the mediaType used for zstd layers.
	// Warning: This mediaType is not officially supported in https://github.com/distribution/distribution/blob/main/docs/content/spec/manifest-v2-2.md but some images may exhibit it. Support is partial.
	DockerV2SchemaLayerMediaTypeZstd = manifest.DockerV2SchemaLayerMediaTypeZstd
	// DockerV2ListMediaType MIME type represents Docker manifest schema 2 list
	DockerV2ListMediaType = manifest.DockerV2ListMediaType
	// DockerV2Schema2ForeignLayerMediaType is the MIME type used for schema 2 foreign layers.
	DockerV2Schema2ForeignLayerMediaType = manifest.DockerV2Schema2ForeignLayerMediaType
	// DockerV2Schema2ForeignLayerMediaType is the MIME type used for gzipped schema 2 foreign layers.
	DockerV2Schema2ForeignLayerMediaTypeGzip = manifest.DockerV2Schema2ForeignLayerMediaTypeGzip
)

// NonImageArtifactError (detected via errors.As) is used when asking for an image-specific operation
// on an object which is not a “container image” in the standard sense (e.g. an OCI artifact)
type NonImageArtifactError = manifest.NonImageArtifactError

// SupportedSchema2MediaType checks if the specified string is a supported Docker v2s2 media type.
func SupportedSchema2MediaType(m string) error {
	switch m {
	case DockerV2ListMediaType, DockerV2Schema1MediaType, DockerV2Schema1SignedMediaType, DockerV2Schema2ConfigMediaType, DockerV2Schema2ForeignLayerMediaType, DockerV2Schema2ForeignLayerMediaTypeGzip, DockerV2Schema2LayerMediaType, DockerV2Schema2MediaType, DockerV2SchemaLayerMediaTypeUncompressed, DockerV2SchemaLayerMediaTypeZstd:
		return nil
	default:
		return fmt.Errorf("unsupported docker v2s2 media type: %q", m)
	}
}

// DefaultRequestedManifestMIMETypes is a list of MIME types a types.ImageSource
// should request from the backend unless directed otherwise.
var DefaultRequestedManifestMIMETypes = []string{
	imgspecv1.MediaTypeImageManifest,
	DockerV2Schema2MediaType,
	DockerV2Schema1SignedMediaType,
	DockerV2Schema1MediaType,
	DockerV2ListMediaType,
	imgspecv1.MediaTypeImageIndex,
}

// Manifest is an interface for parsing, modifying image manifests in isolation.
// Callers can either use this abstract interface without understanding the details of the formats,
// or instantiate a specific implementation (e.g. manifest.OCI1) and access the public members
// directly.
//
// See types.Image for functionality not limited to manifests, including format conversions and config parsing.
// This interface is similar to, but not strictly equivalent to, the equivalent methods in types.Image.
type Manifest interface {
	// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
	ConfigInfo() types.BlobInfo
	// LayerInfos returns a list of LayerInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
	// The Digest field is guaranteed to be provided; Size may be -1.
	// WARNING: The list may contain duplicates, and they are semantically relevant.
	LayerInfos() []LayerInfo
	// UpdateLayerInfos replaces the original layers with the specified BlobInfos (size+digest+urls), in order (the root layer first, and then successive layered layers)
	UpdateLayerInfos(layerInfos []types.BlobInfo) error

	// ImageID computes an ID which can uniquely identify this image by its contents, irrespective
	// of which (of possibly more than one simultaneously valid) reference was used to locate the
	// image, and unchanged by whether or how the layers are compressed.  The result takes the form
	// of the hexadecimal portion of a digest.Digest.
	ImageID(diffIDs []digest.Digest) (string, error)

	// Inspect returns various information for (skopeo inspect) parsed from the manifest,
	// incorporating information from a configuration blob returned by configGetter, if
	// the underlying image format is expected to include a configuration blob.
	Inspect(configGetter func(types.BlobInfo) ([]byte, error)) (*types.ImageInspectInfo, error)

	// Serialize returns the manifest in a blob format.
	// NOTE: Serialize() does not in general reproduce the original blob if this object was loaded from one, even if no modifications were made!
	Serialize() ([]byte, error)
}

// LayerInfo is an extended version of types.BlobInfo for low-level users of Manifest.LayerInfos.
type LayerInfo struct {
	types.BlobInfo
	EmptyLayer bool // The layer is an “empty”/“throwaway” one, and may or may not be physically represented in various transport / storage systems.  false if the manifest type does not have the concept.
}

// GuessMIMEType guesses MIME type of a manifest and returns it _if it is recognized_, or "" if unknown or unrecognized.
// FIXME? We should, in general, prefer out-of-band MIME type instead of blindly parsing the manifest,
// but we may not have such metadata available (e.g. when the manifest is a local file).
func GuessMIMEType(manifestBlob []byte) string {
	return manifest.GuessMIMEType(manifestBlob)
}

// Digest returns the a digest of a docker manifest, with any necessary implied transformations like stripping v1s1 signatures.
func Digest(manifestBlob []byte) (digest.Digest, error) {
	return manifest.Digest(manifestBlob)
}

// MatchesDigest returns true iff the manifest matches expectedDigest.
// Error may be set if this returns false.
// Note that this is not doing ConstantTimeCompare; by the time we get here, the cryptographic signature must already have been verified,
// or we are not using a cryptographic channel and the attacker can modify the digest along with the manifest blob.
func MatchesDigest(manifestBlob []byte, expectedDigest digest.Digest) (bool, error) {
	return manifest.MatchesDigest(manifestBlob, expectedDigest)
}

// AddDummyV2S1Signature adds an JWS signature with a temporary key (i.e. useless) to a v2s1 manifest.
// This is useful to make the manifest acceptable to a docker/distribution registry (even though nothing needs or wants the JWS signature).
func AddDummyV2S1Signature(manifest []byte) ([]byte, error) {
	key, err := libtrust.GenerateECP256PrivateKey()
	if err != nil {
		return nil, err // Coverage: This can fail only if rand.Reader fails.
	}

	js, err := libtrust.NewJSONSignature(manifest)
	if err != nil {
		return nil, err
	}
	if err := js.Sign(key); err != nil { // Coverage: This can fail basically only if rand.Reader fails.
		return nil, err
	}
	return js.PrettySignature("signatures")
}

// MIMETypeIsMultiImage returns true if mimeType is a list of images
func MIMETypeIsMultiImage(mimeType string) bool {
	return mimeType == DockerV2ListMediaType || mimeType == imgspecv1.MediaTypeImageIndex
}

// MIMETypeSupportsEncryption returns true if the mimeType supports encryption
func MIMETypeSupportsEncryption(mimeType string) bool {
	return mimeType == imgspecv1.MediaTypeImageManifest
}

// NormalizedMIMEType returns the effective MIME type of a manifest MIME type returned by a server,
// centralizing various workarounds.
func NormalizedMIMEType(input string) string {
	return manifest.NormalizedMIMEType(input)
}

// FromBlob returns a Manifest instance for the specified manifest blob and the corresponding MIME type
func FromBlob(manblob []byte, mt string) (Manifest, error) {
	nmt := NormalizedMIMEType(mt)
	switch nmt {
	case DockerV2Schema1MediaType, DockerV2Schema1SignedMediaType:
		return Schema1FromManifest(manblob)
	case imgspecv1.MediaTypeImageManifest:
		return OCI1FromManifest(manblob)
	case DockerV2Schema2MediaType:
		return Schema2FromManifest(manblob)
	case DockerV2ListMediaType, imgspecv1.MediaTypeImageIndex:
		return nil, fmt.Errorf("Treating manifest lists as individual manifests is not implemented")
	}
	// Note that this may not be reachable, NormalizedMIMEType has a default for unknown values.
	return nil, fmt.Errorf("Unimplemented manifest MIME type %q (normalized as %q)", mt, nmt)
}
