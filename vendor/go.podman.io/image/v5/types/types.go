package types

import (
	"context"
	"io"
	"net/url"
	"time"

	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
	compression "go.podman.io/image/v5/pkg/compression/types"
)

// ImageTransport is a top-level namespace for ways to store/load an image.
// It should generally correspond to ImageSource/ImageDestination implementations.
//
// Note that ImageTransport is based on "ways the users refer to image storage", not necessarily on the underlying physical transport.
// For example, all Docker References would be used within a single "docker" transport, regardless of whether the images are pulled over HTTP or HTTPS
// (or, even, IPv4 or IPv6).
//
// OTOH all images using the same transport should (apart from versions of the image format), be interoperable.
// For example, several different ImageTransport implementations may be based on local filesystem paths,
// but using completely different formats for the contents of that path (a single tar file, a directory containing tarballs, a fully expanded container filesystem, ...)
//
// See also transports.KnownTransports.
type ImageTransport interface {
	// Name returns the name of the transport, which must be unique among other transports.
	Name() string
	// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
	ParseReference(reference string) (ImageReference, error)
	// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
	// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
	// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
	// scope passed to this function will not be "", that value is always allowed.
	ValidatePolicyConfigurationScope(scope string) error
}

// ImageReference is an abstracted way to refer to an image location, namespaced within an ImageTransport.
//
// The object should preferably be immutable after creation, with any parsing/state-dependent resolving happening
// within an ImageTransport.ParseReference() or equivalent API creating the reference object.
// That's also why the various identification/formatting methods of this type do not support returning errors.
//
// WARNING: While this design freezes the content of the reference within this process, it can not freeze the outside
// world: paths may be replaced by symlinks elsewhere, HTTP APIs may start returning different results, and so on.
type ImageReference interface {
	Transport() ImageTransport
	// StringWithinTransport returns a string representation of the reference, which MUST be such that
	// reference.Transport().ParseReference(reference.StringWithinTransport()) returns an equivalent reference.
	// NOTE: The returned string is not promised to be equal to the original input to ParseReference;
	// e.g. default attribute values omitted by the user may be filled in the return value, or vice versa.
	// WARNING: Do not use the return value in the UI to describe an image, it does not contain the Transport().Name() prefix;
	// instead, see transports.ImageName().
	StringWithinTransport() string

	// DockerReference returns a Docker reference associated with this reference
	// (fully explicit, i.e. !reference.IsNameOnly, but reflecting user intent,
	// not e.g. after redirect or alias processing), or nil if unknown/not applicable.
	DockerReference() reference.Named

	// PolicyConfigurationIdentity returns a string representation of the reference, suitable for policy lookup.
	// This MUST reflect user intent, not e.g. after processing of third-party redirects or aliases;
	// The value SHOULD be fully explicit about its semantics, with no hidden defaults, AND canonical
	// (i.e. various references with exactly the same semantics should return the same configuration identity)
	// It is fine for the return value to be equal to StringWithinTransport(), and it is desirable but
	// not required/guaranteed that it will be a valid input to Transport().ParseReference().
	// Returns "" if configuration identities for these references are not supported.
	PolicyConfigurationIdentity() string

	// PolicyConfigurationNamespaces returns a list of other policy configuration namespaces to search
	// for if explicit configuration for PolicyConfigurationIdentity() is not set.  The list will be processed
	// in order, terminating on first match, and an implicit "" is always checked at the end.
	// It is STRONGLY recommended for the first element, if any, to be a prefix of PolicyConfigurationIdentity(),
	// and each following element to be a prefix of the element preceding it.
	PolicyConfigurationNamespaces() []string

	// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
	// The caller must call .Close() on the returned ImageCloser.
	// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
	// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
	// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
	NewImage(ctx context.Context, sys *SystemContext) (ImageCloser, error)
	// NewImageSource returns a types.ImageSource for this reference.
	// The caller must call .Close() on the returned ImageSource.
	NewImageSource(ctx context.Context, sys *SystemContext) (ImageSource, error)
	// NewImageDestination returns a types.ImageDestination for this reference.
	// The caller must call .Close() on the returned ImageDestination.
	NewImageDestination(ctx context.Context, sys *SystemContext) (ImageDestination, error)

	// DeleteImage deletes the named image from the registry, if supported.
	DeleteImage(ctx context.Context, sys *SystemContext) error
}

// LayerCompression indicates if layers must be compressed, decompressed or preserved
type LayerCompression int

const (
	// PreserveOriginal indicates the layer must be preserved, ie
	// no compression or decompression.
	PreserveOriginal LayerCompression = iota
	// Decompress indicates the layer must be decompressed
	Decompress
	// Compress indicates the layer must be compressed
	Compress
)

// LayerCrypto indicates if layers have been encrypted or decrypted or none
type LayerCrypto int

const (
	// PreserveOriginalCrypto indicates the layer must be preserved, ie
	// no encryption/decryption
	PreserveOriginalCrypto LayerCrypto = iota
	// Encrypt indicates the layer is encrypted
	Encrypt
	// Decrypt indicates the layer is decrypted
	Decrypt
)

// BlobInfo collects known information about a blob (layer/config).
// In some situations, some fields may be unknown, in others they may be mandatory; documenting an “unknown” value here does not override that.
type BlobInfo struct {
	Digest      digest.Digest // "" if unknown.
	Size        int64         // -1 if unknown
	URLs        []string
	Annotations map[string]string
	MediaType   string

	// NOTE: The following fields contain desired _edits_ to blob infos.
	// Conceptually then don't belong in the BlobInfo object at all;
	// the edits should be provided specifically as parameters to the edit implementation.
	// We can’t remove the fields without breaking compatibility, but don’t
	// add any more.

	// CompressionOperation is used in Image.UpdateLayerInfos to instruct
	// whether the original layer's "compressed or not" should be preserved,
	// possibly while changing the compression algorithm from one to another,
	// or if it should be changed to compressed or decompressed.
	// The field defaults to preserve the original layer's compressedness.
	// TODO: To remove together with CryptoOperation in re-design to remove
	// field out of BlobInfo.
	CompressionOperation LayerCompression
	// CompressionAlgorithm is used in Image.UpdateLayerInfos to set the correct
	// MIME type for compressed layers (e.g., gzip or zstd). This field MUST be
	// set when `CompressionOperation == Compress` and MAY be set when
	// `CompressionOperation == PreserveOriginal` and the compression type is
	// being changed for an already-compressed layer.
	CompressionAlgorithm *compression.Algorithm
	// CryptoOperation is used in Image.UpdateLayerInfos to instruct
	// whether the original layer was encrypted/decrypted
	// TODO: To remove together with CompressionOperation in re-design to
	// remove field out of BlobInfo.
	CryptoOperation LayerCrypto
	// Before adding any fields to this struct, read the NOTE above.
}

// BICTransportScope encapsulates transport-dependent representation of a “scope” where blobs are or are not present.
// BlobInfocache.RecordKnownLocations / BlobInfocache.CandidateLocations record data about blobs keyed by (scope, digest).
// The scope will typically be similar to an ImageReference, or a superset of it within which blobs are reusable.
//
// NOTE: The contents of this structure may be recorded in a persistent file, possibly shared across different
// tools which use different versions of the transport.  Allow for reasonable backward/forward compatibility,
// at least by not failing hard when encountering unknown data.
type BICTransportScope struct {
	Opaque string
}

// BICLocationReference encapsulates transport-dependent representation of a blob location within a BICTransportScope.
// Each transport can store arbitrary data using BlobInfoCache.RecordKnownLocation, and ImageDestination.TryReusingBlob
// can look it up using BlobInfoCache.CandidateLocations.
//
// NOTE: The contents of this structure may be recorded in a persistent file, possibly shared across different
// tools which use different versions of the transport.  Allow for reasonable backward/forward compatibility,
// at least by not failing hard when encountering unknown data.
type BICLocationReference struct {
	Opaque string
}

// BICReplacementCandidate is an item returned by BlobInfoCache.CandidateLocations.
type BICReplacementCandidate struct {
	Digest   digest.Digest
	Location BICLocationReference
}

// BlobInfoCache records data useful for reusing blobs, or substituting equivalent ones, to avoid unnecessary blob copies.
//
// It records two kinds of data:
//
//   - Sets of corresponding digest vs. uncompressed digest ("DiffID") pairs:
//     One of the two digests is known to be uncompressed, and a single uncompressed digest may correspond to more than one compressed digest.
//     This allows matching compressed layer blobs to existing local uncompressed layers (to avoid unnecessary download and decompression),
//     or uncompressed layer blobs to existing remote compressed layers (to avoid unnecessary compression and upload)/
//
//     It is allowed to record an (uncompressed digest, the same uncompressed digest) correspondence, to express that the digest is known
//     to be uncompressed (i.e. that a conversion from schema1 does not have to decompress the blob to compute a DiffID value).
//
//     This mapping is primarily maintained in generic copy.Image code, but transports may want to contribute more data points if they independently
//     compress/decompress blobs for their own purposes.
//
//   - Known blob locations, managed by individual transports:
//     The transports call RecordKnownLocation when encountering a blob that could possibly be reused (typically in GetBlob/PutBlob/TryReusingBlob),
//     recording transport-specific information that allows the transport to reuse the blob in the future;
//     then, TryReusingBlob implementations can call CandidateLocations to look up previously recorded blob locations that could be reused.
//
//     Each transport defines its own “scopes” within which blob reuse is possible (e.g. in, the docker/distribution case, blobs
//     can be directly reused within a registry, or mounted across registries within a registry server.)
//
// None of the methods return an error indication: errors when neither reading from, nor writing to, the cache, should be fatal;
// users of the cache should just fall back to copying the blobs the usual way.
//
// The BlobInfoCache interface is deprecated.  Consumers of this library should use one of the implementations provided by
// subpackages of the library's "pkg/blobinfocache" package in preference to implementing the interface on their own.
type BlobInfoCache interface {
	// UncompressedDigest returns an uncompressed digest corresponding to anyDigest.
	// May return anyDigest if it is known to be uncompressed.
	// Returns "" if nothing is known about the digest (it may be compressed or uncompressed).
	UncompressedDigest(anyDigest digest.Digest) digest.Digest
	// RecordDigestUncompressedPair records that the uncompressed version of anyDigest is uncompressed.
	// It’s allowed for anyDigest == uncompressed.
	// WARNING: Only call this for LOCALLY VERIFIED data; don’t record a digest pair just because some remote author claims so (e.g.
	// because a manifest/config pair exists); otherwise the cache could be poisoned and allow substituting unexpected blobs.
	// (Eventually, the DiffIDs in image config could detect the substitution, but that may be too late, and not all image formats contain that data.)
	RecordDigestUncompressedPair(anyDigest digest.Digest, uncompressed digest.Digest)

	// RecordKnownLocation records that a blob with the specified digest exists within the specified (transport, scope) scope,
	// and can be reused given the opaque location data.
	RecordKnownLocation(transport ImageTransport, scope BICTransportScope, digest digest.Digest, location BICLocationReference)
	// CandidateLocations returns a prioritized, limited, number of blobs and their locations that could possibly be reused
	// within the specified (transport scope) (if they still exist, which is not guaranteed).
	//
	// If !canSubstitute, the returned candidates will match the submitted digest exactly; if canSubstitute,
	// data from previous RecordDigestUncompressedPair calls is used to also look up variants of the blob which have the same
	// uncompressed digest.
	CandidateLocations(transport ImageTransport, scope BICTransportScope, digest digest.Digest, canSubstitute bool) []BICReplacementCandidate
}

// ImageSource is a service, possibly remote (= slow), to download components of a single image or a named image set (manifest list).
// This is primarily useful for copying images around; for examining their properties, Image (below)
// is usually more useful.
// Each ImageSource should eventually be closed by calling Close().
//
// WARNING: Various methods which return an object identified by digest generally do not
// validate that the returned data actually matches that digest; this is the caller’s responsibility.
// See the individual methods’ documentation for potentially more details.
type ImageSource interface {
	// Reference returns the reference used to set up this source, _as specified by the user_
	// (not as the image itself, or its underlying storage, claims).  This can be used e.g. to determine which public keys are trusted for this image.
	Reference() ImageReference
	// Close removes resources associated with an initialized ImageSource, if any.
	Close() error
	// GetManifest returns the image's manifest along with its MIME type (which may be empty when it can't be determined but the manifest is available).
	// It may use a remote (= slow) service.
	// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve (when the primary manifest is a manifest list);
	// this never happens if the primary manifest is not a manifest list (e.g. if the source never returns manifest lists).
	//
	// WARNING: This is a raw access to the data as provided by the source; if the reference contains a digest, or instanceDigest is set,
	// callers must enforce the digest match themselves, typically by using image.UnparsedInstance to access the manifest instead
	// of calling this directly. (Compare the generic warning applicable to all of the [ImageSource] interface.)
	GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error)
	// GetBlob returns a stream for the specified blob, and the blob’s size (or -1 if unknown).
	// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
	// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
	//
	// WARNING: This is a raw access to the data as provided by the source; callers must validate the contents
	// against the blob’s digest themselves. (Compare the generic warning applicable to all of the [ImageSource] interface.)
	GetBlob(context.Context, BlobInfo, BlobInfoCache) (io.ReadCloser, int64, error)
	// HasThreadSafeGetBlob indicates whether GetBlob can be executed concurrently.
	HasThreadSafeGetBlob() bool
	// GetSignatures returns the image's signatures.  It may use a remote (= slow) service.
	// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
	// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
	// (e.g. if the source never returns manifest lists).
	GetSignatures(ctx context.Context, instanceDigest *digest.Digest) ([][]byte, error)
	// LayerInfosForCopy returns either nil (meaning the values in the manifest are fine), or updated values for the layer
	// blobsums that are listed in the image's manifest.  If values are returned, they should be used when using GetBlob()
	// to read the image's layers.
	// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve BlobInfos for
	// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
	// (e.g. if the source never returns manifest lists).
	// The Digest field is guaranteed to be provided; Size may be -1.
	// WARNING: The list may contain duplicates, and they are semantically relevant.
	LayerInfosForCopy(ctx context.Context, instanceDigest *digest.Digest) ([]BlobInfo, error)
}

// ImageDestination is a service, possibly remote (= slow), to store components of a single image.
//
// There is a specific required order for some of the calls:
// TryReusingBlob/PutBlob on the various blobs, if any, MUST be called before PutManifest (manifest references blobs, which may be created or compressed only at push time)
// PutSignatures, if called, MUST be called after PutManifest (signatures reference manifest contents)
// Finally, Commit MUST be called if the caller wants the image, as formed by the components saved above, to persist.
//
// Each ImageDestination should eventually be closed by calling Close().
type ImageDestination interface {
	// Reference returns the reference used to set up this destination.  Note that this should directly correspond to user's intent,
	// e.g. it should use the public hostname instead of the result of resolving CNAMEs or following redirects.
	Reference() ImageReference
	// Close removes resources associated with an initialized ImageDestination, if any.
	Close() error

	// SupportedManifestMIMETypes tells which manifest mime types the destination supports
	// If an empty slice or nil it's returned, then any mime type can be tried to upload
	SupportedManifestMIMETypes() []string
	// SupportsSignatures returns an error (to be displayed to the user) if the destination certainly can't store signatures.
	// Note: It is still possible for PutSignatures to fail if SupportsSignatures returns nil.
	SupportsSignatures(ctx context.Context) error
	// DesiredLayerCompression indicates the kind of compression to apply on layers
	DesiredLayerCompression() LayerCompression
	// AcceptsForeignLayerURLs returns false iff foreign layers in manifest should be actually
	// uploaded to the image destination, true otherwise.
	AcceptsForeignLayerURLs() bool
	// MustMatchRuntimeOS returns true iff the destination can store only images targeted for the current runtime architecture and OS. False otherwise.
	MustMatchRuntimeOS() bool
	// IgnoresEmbeddedDockerReference() returns true iff the destination does not care about Image.EmbeddedDockerReferenceConflicts(),
	// and would prefer to receive an unmodified manifest instead of one modified for the destination.
	// Does not make a difference if Reference().DockerReference() is nil.
	IgnoresEmbeddedDockerReference() bool

	// PutBlob writes contents of stream and returns data representing the result.
	// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
	// inputInfo.Size is the expected length of stream, if known.
	// inputInfo.MediaType describes the blob format, if known.
	// May update cache.
	// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
	// to any other readers for download using the supplied digest.
	// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlob MUST 1) fail, and 2) delete any data stored so far.
	PutBlob(ctx context.Context, stream io.Reader, inputInfo BlobInfo, cache BlobInfoCache, isConfig bool) (BlobInfo, error)
	// HasThreadSafePutBlob indicates whether PutBlob can be executed concurrently.
	HasThreadSafePutBlob() bool
	// TryReusingBlob checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
	// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
	// info.Digest must not be empty.
	// If canSubstitute, TryReusingBlob can use an equivalent equivalent of the desired blob; in that case the returned info may not match the input.
	// If the blob has been successfully reused, returns (true, info, nil); info must contain at least a digest and size, and may
	// include CompressionOperation and CompressionAlgorithm fields to indicate that a change to the compression type should be
	// reflected in the manifest that will be written.
	// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
	// May use and/or update cache.
	TryReusingBlob(ctx context.Context, info BlobInfo, cache BlobInfoCache, canSubstitute bool) (bool, BlobInfo, error)
	// PutManifest writes manifest to the destination.
	// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write the manifest for
	// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
	// It is expected but not enforced that the instanceDigest, when specified, matches the digest of `manifest` as generated
	// by `manifest.Digest()`.
	// FIXME? This should also receive a MIME type if known, to differentiate between schema versions.
	// If the destination is in principle available, refuses this manifest type (e.g. it does not recognize the schema),
	// but may accept a different manifest type, the returned error must be an ManifestTypeRejectedError.
	PutManifest(ctx context.Context, manifest []byte, instanceDigest *digest.Digest) error
	// PutSignatures writes a set of signatures to the destination.
	// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
	// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
	// MUST be called after PutManifest (signatures may reference manifest contents).
	PutSignatures(ctx context.Context, signatures [][]byte, instanceDigest *digest.Digest) error
	// Commit marks the process of storing the image as successful and asks for the image to be persisted.
	// unparsedToplevel contains data about the top-level manifest of the source (which may be a single-arch image or a manifest list
	// if PutManifest was only called for the single-arch image with instanceDigest == nil), primarily to allow lookups by the
	// original manifest list digest, if desired.
	// WARNING: This does not have any transactional semantics:
	// - Uploaded data MAY be visible to others before Commit() is called
	// - Uploaded data MAY be removed or MAY remain around if Close() is called without Commit() (i.e. rollback is allowed but not guaranteed)
	Commit(ctx context.Context, unparsedToplevel UnparsedImage) error
}

// ManifestTypeRejectedError is returned by ImageDestination.PutManifest if the destination is in principle available,
// refuses specifically this manifest type, but may accept a different manifest type.
type ManifestTypeRejectedError struct { // We only use a struct to allow a type assertion, without limiting the contents of the error otherwise.
	Err error
}

func (e ManifestTypeRejectedError) Error() string {
	return e.Err.Error()
}

// UnparsedImage is an Image-to-be; until it is verified and accepted, it only caries its identity and caches manifest and signature blobs.
// Thus, an UnparsedImage can be created from an ImageSource simply by fetching blobs without interpreting them,
// allowing cryptographic signature verification to happen first, before even fetching the manifest, or parsing anything else.
// This also makes the UnparsedImage→Image conversion an explicitly visible step.
//
// An UnparsedImage is a pair of (ImageSource, instance digest); it can represent either a manifest list or a single image instance.
//
// The UnparsedImage must not be used after the underlying ImageSource is Close()d.
type UnparsedImage interface {
	// Reference returns the reference used to set up this source, _as specified by the user_
	// (not as the image itself, or its underlying storage, claims).  This can be used e.g. to determine which public keys are trusted for this image.
	Reference() ImageReference
	// Manifest is like ImageSource.GetManifest, but the result is cached; it is OK to call this however often you need.
	Manifest(ctx context.Context) ([]byte, string, error)
	// Signatures is like ImageSource.GetSignatures, but the result is cached; it is OK to call this however often you need.
	Signatures(ctx context.Context) ([][]byte, error)
}

// Image is the primary API for inspecting properties of images.
// An Image is based on a pair of (ImageSource, instance digest); it can represent either a manifest list or a single image instance.
//
// The Image must not be used after the underlying ImageSource is Close()d.
type Image interface {
	// Note that Reference may return nil in the return value of UpdatedImage!
	UnparsedImage
	// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
	// Note that the config object may not exist in the underlying storage in the return value of UpdatedImage! Use ConfigBlob() below.
	ConfigInfo() BlobInfo
	// ConfigBlob returns the blob described by ConfigInfo, if ConfigInfo().Digest != ""; nil otherwise.
	// The result is cached; it is OK to call this however often you need.
	ConfigBlob(context.Context) ([]byte, error)
	// OCIConfig returns the image configuration as per OCI v1 image-spec. Information about
	// layers in the resulting configuration isn't guaranteed to be returned to due how
	// old image manifests work (docker v2s1 especially).
	OCIConfig(context.Context) (*v1.Image, error)
	// LayerInfos returns a list of BlobInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
	// The Digest field is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
	// WARNING: The list may contain duplicates, and they are semantically relevant.
	LayerInfos() []BlobInfo
	// LayerInfosForCopy returns either nil (meaning the values in the manifest are fine), or updated values for the layer blobsums that are listed in the image's manifest.
	// The Digest field is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
	// WARNING: The list may contain duplicates, and they are semantically relevant.
	LayerInfosForCopy(context.Context) ([]BlobInfo, error)
	// EmbeddedDockerReferenceConflicts whether a Docker reference embedded in the manifest, if any, conflicts with destination ref.
	// It returns false if the manifest does not embed a Docker reference.
	// (This embedding unfortunately happens for Docker schema1, please do not add support for this in any new formats.)
	EmbeddedDockerReferenceConflicts(ref reference.Named) bool
	// Inspect returns various information for (skopeo inspect) parsed from the manifest and configuration.
	Inspect(context.Context) (*ImageInspectInfo, error)
	// UpdatedImageNeedsLayerDiffIDs returns true iff UpdatedImage(options) needs InformationOnly.LayerDiffIDs.
	// This is a horribly specific interface, but computing InformationOnly.LayerDiffIDs can be very expensive to compute
	// (most importantly it forces us to download the full layers even if they are already present at the destination).
	UpdatedImageNeedsLayerDiffIDs(options ManifestUpdateOptions) bool
	// UpdatedImage returns a types.Image modified according to options.
	// Everything in options.InformationOnly should be provided, other fields should be set only if a modification is desired.
	// This does not change the state of the original Image object.
	// The returned error will be a manifest.ManifestLayerCompressionIncompatibilityError if
	// manifests of type options.ManifestMIMEType can not include layers that are compressed
	// in accordance with the CompressionOperation and CompressionAlgorithm specified in one
	// or more options.LayerInfos items, though retrying with a different
	// options.ManifestMIMEType or with different CompressionOperation+CompressionAlgorithm
	// values might succeed.
	UpdatedImage(ctx context.Context, options ManifestUpdateOptions) (Image, error)
	// SupportsEncryption returns an indicator that the image supports encryption
	//
	// Deprecated: Initially used to determine if a manifest can be copied from a source manifest type since
	// the process of updating a manifest between different manifest types was to update then convert.
	// This resulted in some fields in the update being lost. This has been fixed by: https://github.com/containers/image/pull/836
	SupportsEncryption(ctx context.Context) bool
	// Size returns an approximation of the amount of disk space which is consumed by the image in its current
	// location.  If the size is not known, -1 will be returned.
	Size() (int64, error)
}

// ImageCloser is an Image with a Close() method which must be called by the user.
// This is returned by ImageReference.NewImage, which transparently instantiates a types.ImageSource,
// to ensure that the ImageSource is closed.
type ImageCloser interface {
	Image
	// Close removes resources associated with an initialized ImageCloser.
	Close() error
}

// ManifestUpdateOptions is a way to pass named optional arguments to Image.UpdatedImage
type ManifestUpdateOptions struct {
	LayerInfos              []BlobInfo // Complete BlobInfos (size+digest+urls+annotations) which should replace the originals, in order (the root layer first, and then successive layered layers). BlobInfos' MediaType fields are ignored.
	EmbeddedDockerReference reference.Named
	ManifestMIMEType        string
	// The values below are NOT requests to modify the image; they provide optional context which may or may not be used.
	InformationOnly ManifestUpdateInformation
}

// ManifestUpdateInformation is a component of ManifestUpdateOptions, named here
// only to make writing struct literals possible.
type ManifestUpdateInformation struct {
	Destination  ImageDestination // and yes, UpdatedImage may write to Destination (see the schema2 → schema1 conversion logic in image/docker_schema2.go)
	LayerInfos   []BlobInfo       // Complete BlobInfos (size+digest) which have been uploaded, in order (the root layer first, and then successive layered layers)
	LayerDiffIDs []digest.Digest  // Digest values for the _uncompressed_ contents of the blobs which have been uploaded, in the same order.
}

// ImageInspectInfo is a set of metadata describing Docker images, primarily their manifest and configuration.
// The Tag field is a legacy field which is here just for the Docker v2s1 manifest. It won't be supported
// for other manifest types.
type ImageInspectInfo struct {
	Tag           string
	Created       *time.Time
	DockerVersion string
	Labels        map[string]string
	Architecture  string
	Variant       string
	Os            string
	Layers        []string
	LayersData    []ImageInspectLayer
	Env           []string
	Author        string
}

// ImageInspectLayer is a set of metadata describing an image layers' detail
type ImageInspectLayer struct {
	MIMEType    string // "" if unknown.
	Digest      digest.Digest
	Size        int64 // -1 if unknown.
	Annotations map[string]string
}

// DockerAuthConfig contains authorization information for connecting to a registry.
// the value of Username and Password can be empty for accessing the registry anonymously
type DockerAuthConfig struct {
	Username string
	Password string
	// IdentityToken can be used as an refresh_token in place of username and
	// password to obtain the bearer/access token in oauth2 flow. If identity
	// token is set, password should not be set.
	// Ref: https://docs.docker.com/registry/spec/auth/oauth/
	IdentityToken string
}

// OptionalBool is a boolean with an additional undefined value, which is meant
// to be used in the context of user input to distinguish between a
// user-specified value and a default value.
type OptionalBool byte

const (
	// OptionalBoolUndefined indicates that the OptionalBoolean hasn't been written.
	OptionalBoolUndefined OptionalBool = iota
	// OptionalBoolTrue represents the boolean true.
	OptionalBoolTrue
	// OptionalBoolFalse represents the boolean false.
	OptionalBoolFalse
)

// NewOptionalBool converts the input bool into either OptionalBoolTrue or
// OptionalBoolFalse.  The function is meant to avoid boilerplate code of users.
func NewOptionalBool(b bool) OptionalBool {
	o := OptionalBoolFalse
	if b {
		o = OptionalBoolTrue
	}
	return o
}

// ShortNameMode defines the mode of short-name resolution.
//
// The use of unqualified-search registries entails an ambiguity as it's
// unclear from which registry a given image, referenced by a short name, may
// be pulled from.
//
// The ShortNameMode type defines how short names should resolve.
type ShortNameMode int

const (
	ShortNameModeInvalid ShortNameMode = iota
	// Use all configured unqualified-search registries without prompting
	// the user.
	ShortNameModeDisabled
	// If stdout and stdin are a TTY, prompt the user to select a configured
	// unqualified-search registry. Otherwise, use all configured
	// unqualified-search registries.
	//
	// Note that if only one unqualified-search registry is set, it will be
	// used without prompting.
	ShortNameModePermissive
	// Always prompt the user to select a configured unqualified-search
	// registry.  Throw an error if stdout or stdin is not a TTY as
	// prompting isn't possible.
	//
	// Note that if only one unqualified-search registry is set, it will be
	// used without prompting.
	ShortNameModeEnforcing
)

// SystemContext allows parameterizing access to implicitly-accessed resources,
// like configuration files in /etc and users' login state in their home directory.
// Various components can share the same field only if their semantics is exactly
// the same; if in doubt, add a new field.
// It is always OK to pass nil instead of a SystemContext.
type SystemContext struct {
	// If not "", prefixed to any absolute paths used by default by the library (e.g. in /etc/).
	// Not used for any of the more specific path overrides available in this struct.
	// Not used for any paths specified by users in config files (even if the location of the config file _was_ affected by it).
	// NOTE: If this is set, environment-variable overrides of paths are ignored (to keep the semantics simple: to create an /etc replacement, just set RootForImplicitAbsolutePaths .
	// and there is no need to worry about the environment.)
	// NOTE: This does NOT affect paths starting by $HOME.
	RootForImplicitAbsolutePaths string

	// === Global configuration overrides ===
	// If not "", overrides the system's default path for signature.Policy configuration.
	SignaturePolicyPath string
	// If not "", overrides the system's default path for registries.d (Docker signature storage configuration)
	RegistriesDirPath string
	// Path to the system-wide registries configuration file
	SystemRegistriesConfPath string
	// Path to the system-wide registries configuration directory
	SystemRegistriesConfDirPath string
	// Path to the user-specific short-names configuration file
	UserShortNameAliasConfPath string
	// If set, short-name resolution in pkg/shortnames must follow the specified mode
	ShortNameMode *ShortNameMode
	// If set, short names will resolve in pkg/shortnames to docker.io only, and unqualified-search registries and
	// short-name aliases in registries.conf are ignored.  Note that this field is only intended to help enforce
	// resolving to Docker Hub in the Docker-compatible REST API of Podman; it should never be used outside this
	// specific context.
	PodmanOnlyShortNamesIgnoreRegistriesConfAndForceDockerHub bool
	// If not "", overrides the default path for the registry authentication file, but only new format files
	AuthFilePath string
	// if not "", overrides the default path for the registry authentication file, but with the legacy format;
	// the code currently will by default look for legacy format files like .dockercfg in the $HOME dir;
	// but in addition to the home dir, openshift may mount .dockercfg files (via secret mount)
	// in locations other than the home dir; openshift components should then set this field in those cases;
	// this field is ignored if `AuthFilePath` is set (we favor the newer format);
	// only reading of this data is supported;
	LegacyFormatAuthFilePath string
	// If set, a path to a Docker-compatible "config.json" file containing credentials; and no other files are processed.
	// This must not be set if AuthFilePath is set.
	// Only credentials and credential helpers in this file apre processed, not any other configuration in this file.
	DockerCompatAuthFilePath string
	// If not "", overrides the use of platform.GOARCH when choosing an image or verifying architecture match.
	ArchitectureChoice string
	// If not "", overrides the use of platform.GOOS when choosing an image or verifying OS match.
	OSChoice string
	// If not "", overrides the use of detected ARM platform variant when choosing an image or verifying variant match.
	VariantChoice string
	// If not "", overrides the system's default directory containing a blob info cache.
	BlobInfoCacheDir string
	// Additional tags when creating or copying a docker-archive.
	DockerArchiveAdditionalTags []reference.NamedTagged
	// If not "", overrides the temporary directory to use for storing big files
	BigFilesTemporaryDir string

	// === OCI.Transport overrides ===
	// If not "", a directory containing a CA certificate (ending with ".crt"),
	// a client certificate (ending with ".cert") and a client certificate key
	// (ending with ".key") used when downloading OCI image layers.
	OCICertPath string
	// Allow downloading OCI image layers over HTTP, or HTTPS with failed TLS verification. Note that this does not affect other TLS connections.
	OCIInsecureSkipTLSVerify bool
	// If not "", use a shared directory for storing blobs rather than within OCI layouts
	OCISharedBlobDirPath string
	// Allow UnCompress image layer for OCI image layer
	OCIAcceptUncompressedLayers bool

	// === docker.Transport overrides ===
	// If not "", a directory containing a CA certificate (ending with ".crt"),
	// a client certificate (ending with ".cert") and a client certificate key
	// (ending with ".key") used when talking to a container registry.
	DockerCertPath string
	// If not "", overrides the system’s default path for a directory containing host[:port] subdirectories with the same structure as DockerCertPath above.
	// Ignored if DockerCertPath is non-empty.
	DockerPerHostCertDirPath string
	// Allow contacting container registries over HTTP, or HTTPS with failed TLS verification. Note that this does not affect other TLS connections.
	DockerInsecureSkipTLSVerify OptionalBool
	// if nil, the library tries to parse ~/.docker/config.json to retrieve credentials
	// Ignored if DockerBearerRegistryToken is non-empty.
	DockerAuthConfig *DockerAuthConfig
	// if not "", the library uses this registry token to authenticate to the registry
	DockerBearerRegistryToken string
	// if not "", an User-Agent header is added to each request when contacting a registry.
	DockerRegistryUserAgent string
	// if true, a V1 ping attempt isn't done to give users a better error. Default is false.
	// Note that this field is used mainly to integrate containers/image into projectatomic/docker
	// in order to not break any existing docker's integration tests.
	// Deprecated: The V1 container registry detection is no longer performed, so setting this flag has no effect.
	DockerDisableV1Ping bool
	// If true, dockerImageDestination.SupportedManifestMIMETypes will omit the Schema1 media types from the supported list
	DockerDisableDestSchema1MIMETypes bool
	// If true, the physical pull source of docker transport images logged as info level
	DockerLogMirrorChoice bool
	// Directory to use for OSTree temporary files
	//
	// Deprecated: The OSTree transport has been removed.
	OSTreeTmpDirPath string
	// If true, all blobs will have precomputed digests to ensure layers are not uploaded that already exist on the registry.
	// Note that this requires writing blobs to temporary files, and takes more time than the default behavior,
	// when the digest for a blob is unknown.
	DockerRegistryPushPrecomputeDigests bool
	// DockerProxyURL specifies proxy configuration schema (like socks5://username:password@ip:port)
	DockerProxyURL *url.URL

	// === docker/daemon.Transport overrides ===
	// A directory containing a CA certificate (ending with ".crt"),
	// a client certificate (ending with ".cert") and a client certificate key
	// (ending with ".key") used when talking to a Docker daemon.
	DockerDaemonCertPath string
	// The hostname or IP to the Docker daemon. If not set (aka ""), client.DefaultDockerHost is assumed.
	DockerDaemonHost string
	// Used to skip TLS verification, off by default. To take effect DockerDaemonCertPath needs to be specified as well.
	DockerDaemonInsecureSkipTLSVerify bool

	// === dir.Transport overrides ===
	// DirForceCompress compresses the image layers if set to true
	DirForceCompress bool
	// DirForceDecompress decompresses the image layers if set to true
	DirForceDecompress bool

	// CompressionFormat is the format to use for the compression of the blobs
	CompressionFormat *compression.Algorithm
	// CompressionLevel specifies what compression level is used
	CompressionLevel *int
}

// ProgressEvent is the type of events a progress reader can produce
// Warning: new event types may be added any time.
type ProgressEvent uint

const (
	// ProgressEventNewArtifact will be fired on progress reader setup
	ProgressEventNewArtifact ProgressEvent = iota

	// ProgressEventRead indicates that the artifact download is currently in
	// progress
	ProgressEventRead

	// ProgressEventDone is fired when the data transfer has been finished for
	// the specific artifact
	ProgressEventDone

	// ProgressEventSkipped is fired when the artifact has been skipped because
	// its already available at the destination
	ProgressEventSkipped
)

// ProgressProperties is used to pass information from the copy code to a monitor which
// can use the real-time information to produce output or react to changes.
type ProgressProperties struct {
	// The event indicating what
	Event ProgressEvent

	// The artifact which has been updated in this interval
	Artifact BlobInfo

	// The currently downloaded size in bytes
	// Increases from 0 to the final Artifact size
	Offset uint64

	// The additional offset which has been downloaded inside the last update
	// interval. Will be reset after each ProgressEventRead event.
	OffsetUpdate uint64
}
