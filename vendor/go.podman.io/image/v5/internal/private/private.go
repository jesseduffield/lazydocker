package private

import (
	"context"
	"io"
	"time"

	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/blobinfocache"
	"go.podman.io/image/v5/internal/signature"
	compression "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

// ImageSourceInternalOnly is the part of private.ImageSource that is not
// a part of types.ImageSource.
type ImageSourceInternalOnly interface {
	// SupportsGetBlobAt() returns true if GetBlobAt (BlobChunkAccessor) is supported.
	SupportsGetBlobAt() bool
	// BlobChunkAccessor.GetBlobAt is available only if SupportsGetBlobAt().
	BlobChunkAccessor

	// GetSignaturesWithFormat returns the image's signatures.  It may use a remote (= slow) service.
	// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
	// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
	// (e.g. if the source never returns manifest lists).
	GetSignaturesWithFormat(ctx context.Context, instanceDigest *digest.Digest) ([]signature.Signature, error)
}

// ImageSource is an internal extension to the types.ImageSource interface.
type ImageSource interface {
	types.ImageSource
	ImageSourceInternalOnly
}

// ImageDestinationInternalOnly is the part of private.ImageDestination that is not
// a part of types.ImageDestination.
type ImageDestinationInternalOnly interface {
	// SupportsPutBlobPartial returns true if PutBlobPartial is supported.
	SupportsPutBlobPartial() bool
	// FIXME: Add SupportsSignaturesWithFormat or something like that, to allow early failures
	// on unsupported formats.

	// NoteOriginalOCIConfig provides the config of the image, as it exists on the source, BUT converted to OCI format,
	// or an error obtaining that value (e.g. if the image is an artifact and not a container image).
	// The destination can use it in its TryReusingBlob/PutBlob implementations
	// (otherwise it only obtains the final config after all layers are written).
	NoteOriginalOCIConfig(ociConfig *imgspecv1.Image, configErr error) error

	// PutBlobWithOptions writes contents of stream and returns data representing the result.
	// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
	// inputInfo.Size is the expected length of stream, if known.
	// inputInfo.MediaType describes the blob format, if known.
	// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
	// to any other readers for download using the supplied digest.
	// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlobWithOptions MUST 1) fail, and 2) delete any data stored so far.
	PutBlobWithOptions(ctx context.Context, stream io.Reader, inputInfo types.BlobInfo, options PutBlobOptions) (UploadedBlob, error)

	// PutBlobPartial attempts to create a blob using the data that is already present
	// at the destination. chunkAccessor is accessed in a non-sequential way to retrieve the missing chunks.
	// It is available only if SupportsPutBlobPartial().
	// Even if SupportsPutBlobPartial() returns true, the call can fail.
	// If the call fails with ErrFallbackToOrdinaryLayerDownload, the caller can fall back to PutBlobWithOptions.
	// The fallback _must not_ be done otherwise.
	PutBlobPartial(ctx context.Context, chunkAccessor BlobChunkAccessor, srcInfo types.BlobInfo, options PutBlobPartialOptions) (UploadedBlob, error)

	// TryReusingBlobWithOptions checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
	// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
	// info.Digest must not be empty.
	// If the blob has been successfully reused, returns (true, info, nil).
	// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
	TryReusingBlobWithOptions(ctx context.Context, info types.BlobInfo, options TryReusingBlobOptions) (bool, ReusedBlob, error)

	// PutSignaturesWithFormat writes a set of signatures to the destination.
	// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
	// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
	// MUST be called after PutManifest (signatures may reference manifest contents).
	PutSignaturesWithFormat(ctx context.Context, signatures []signature.Signature, instanceDigest *digest.Digest) error

	// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
	// WARNING: This does not have any transactional semantics:
	// - Uploaded data MAY be visible to others before CommitWithOptions() is called
	// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
	CommitWithOptions(ctx context.Context, options CommitOptions) error
}

// ImageDestination is an internal extension to the types.ImageDestination
// interface.
type ImageDestination interface {
	types.ImageDestination
	ImageDestinationInternalOnly
}

// UploadedBlob is information about a blob written to a destination.
// It is the subset of types.BlobInfo fields the transport is responsible for setting; all fields must be provided.
type UploadedBlob struct {
	Digest digest.Digest
	Size   int64
}

// PutBlobOptions are used in PutBlobWithOptions.
type PutBlobOptions struct {
	Cache    blobinfocache.BlobInfoCache2 // Cache to optionally update with the uploaded bloblook up blob infos.
	IsConfig bool                         // True if the blob is a config

	// The following fields are new to internal/private.  Users of internal/private MUST fill them in,
	// but they also must expect that they will be ignored by types.ImageDestination transports.
	// Transports, OTOH, MUST support these fields being zero-valued for types.ImageDestination callers
	// if they use internal/imagedestination/impl.Compat;
	// in that case, they will all be consistently zero-valued.

	EmptyLayer bool // True if the blob is an "empty"/"throwaway" layer, and may not necessarily be physically represented.
	LayerIndex *int // If the blob is a layer, a zero-based index of the layer within the image; nil otherwise.
}

// PutBlobPartialOptions are used in PutBlobPartial.
type PutBlobPartialOptions struct {
	Cache      blobinfocache.BlobInfoCache2 // Cache to use and/or update.
	EmptyLayer bool                         // True if the blob is an "empty"/"throwaway" layer, and may not necessarily be physically represented.
	LayerIndex int                          // A zero-based index of the layer within the image (PutBlobPartial is only called with layer-like blobs, not configs)
}

// TryReusingBlobOptions are used in TryReusingBlobWithOptions.
type TryReusingBlobOptions struct {
	Cache blobinfocache.BlobInfoCache2 // Cache to use and/or update.
	// If true, it is allowed to use an equivalent of the desired blob;
	// in that case the returned info may not match the input.
	CanSubstitute bool

	// The following fields are new to internal/private.  Users of internal/private MUST fill them in,
	// but they also must expect that they will be ignored by types.ImageDestination transports.
	// Transports, OTOH, MUST support these fields being zero-valued for types.ImageDestination callers
	// if they use internal/imagedestination/impl.Compat;
	// in that case, they will all be consistently zero-valued.
	EmptyLayer              bool                   // True if the blob is an "empty"/"throwaway" layer, and may not necessarily be physically represented.
	LayerIndex              *int                   // If the blob is a layer, a zero-based index of the layer within the image; nil otherwise.
	SrcRef                  reference.Named        // A reference to the source image that contains the input blob.
	PossibleManifestFormats []string               // A set of possible manifest formats; at least one should support the reused layer blob.
	RequiredCompression     *compression.Algorithm // If set, reuse blobs with a matching algorithm as per implementations in internal/imagedestination/impl.helpers.go
	OriginalCompression     *compression.Algorithm // May be nil to indicate “uncompressed” or “unknown”.
	TOCDigest               digest.Digest          // If specified, the blob can be looked up in the destination also by its TOC digest.
}

// ReusedBlob is information about a blob reused in a destination.
// It is the subset of types.BlobInfo fields the transport is responsible for setting.
type ReusedBlob struct {
	Digest digest.Digest // Must be provided
	Size   int64         // Must be provided
	// The following compression fields should be set when the reuse substitutes
	// a differently-compressed blob.
	// They may be set also to change from a base variant to a specific variant of an algorithm.
	CompressionOperation types.LayerCompression // Compress/Decompress, matching the reused blob; PreserveOriginal if N/A
	CompressionAlgorithm *compression.Algorithm // Algorithm if compressed, nil if decompressed or N/A

	// Annotations that should be added, for CompressionAlgorithm. Note that they might need to be
	// added even if the digest doesn’t change (if we found the annotations in a cache).
	CompressionAnnotations map[string]string

	MatchedByTOCDigest bool // Whether the layer was reused/matched by TOC digest. Used only for UI purposes.
}

// CommitOptions are used in CommitWithOptions
type CommitOptions struct {
	// UnparsedToplevel contains data about the top-level manifest of the source (which may be a single-arch image or a manifest list
	// if PutManifest was only called for the single-arch image with instanceDigest == nil), primarily to allow lookups by the
	// original manifest list digest, if desired.
	UnparsedToplevel types.UnparsedImage
	// ReportResolvedReference, if set, asks the transport to store a “resolved” (more detailed) reference to the created image
	// into the value this option points to.
	// What “resolved” means is transport-specific.
	// Transports which don’t support reporting resolved references can ignore the field; the generic copy code writes "nil" into the value.
	ReportResolvedReference *types.ImageReference
	// Timestamp, if set, will force timestamps of content created in the destination to this value.
	// Most transports don't support this.
	//
	// In oci-archive: destinations, this will set the create/mod/access timestamps in each tar entry
	// (but not a timestamp of the created archive file).
	Timestamp *time.Time
}

// ImageSourceChunk is a portion of a blob.
// This API is experimental and can be changed without bumping the major version number.
type ImageSourceChunk struct {
	// Offset specifies the starting position of the chunk within the source blob.
	Offset uint64

	// Length specifies the size of the chunk.  If it is set to math.MaxUint64,
	// then it refers to all the data from Offset to the end of the blob.
	Length uint64
}

// BlobChunkAccessor allows fetching discontiguous chunks of a blob.
type BlobChunkAccessor interface {
	// GetBlobAt returns a sequential channel of readers that contain data for the requested
	// blob chunks, and a channel that might get a single error value.
	// The specified chunks must be not overlapping and sorted by their offset.
	// The readers must be fully consumed, in the order they are returned, before blocking
	// to read the next chunk.
	// If the Length for the last chunk is set to math.MaxUint64, then it
	// fully fetches the remaining data from the offset to the end of the blob.
	GetBlobAt(ctx context.Context, info types.BlobInfo, chunks []ImageSourceChunk) (chan io.ReadCloser, chan error, error)
}

// BadPartialRequestError is returned by BlobChunkAccessor.GetBlobAt on an invalid request.
type BadPartialRequestError struct {
	Status string
}

func (e BadPartialRequestError) Error() string {
	return e.Status
}

// UnparsedImage is an internal extension to the types.UnparsedImage interface.
type UnparsedImage interface {
	types.UnparsedImage
	// UntrustedSignatures is like ImageSource.GetSignaturesWithFormat, but the result is cached; it is OK to call this however often you need.
	UntrustedSignatures(ctx context.Context) ([]signature.Signature, error)
}

// ErrFallbackToOrdinaryLayerDownload is a custom error type returned by PutBlobPartial.
// It suggests to the caller that a fallback mechanism can be used instead of a hard failure;
// otherwise the caller of PutBlobPartial _must not_ fall back to PutBlob.
type ErrFallbackToOrdinaryLayerDownload struct {
	err error
}

func (c ErrFallbackToOrdinaryLayerDownload) Error() string {
	return c.err.Error()
}

func (c ErrFallbackToOrdinaryLayerDownload) Unwrap() error {
	return c.err
}

func NewErrFallbackToOrdinaryLayerDownload(err error) error {
	return ErrFallbackToOrdinaryLayerDownload{err: err}
}
