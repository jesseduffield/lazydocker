package imagedestination

import (
	"context"
	"io"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/imagedestination/stubs"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/types"
)

// wrapped provides the private.ImageDestination operations
// for a destination that only implements types.ImageDestination
type wrapped struct {
	stubs.IgnoresOriginalOCIConfig
	stubs.NoPutBlobPartialInitialize

	types.ImageDestination
}

// FromPublic(dest) returns an object that provides the private.ImageDestination API
//
// Eventually, we might want to expose this function, and methods of the returned object,
// as a public API (or rather, a variant that does not include the already-superseded
// methods of types.ImageDestination, and has added more future-proofing), and more strongly
// deprecate direct use of types.ImageDestination.
//
// NOTE: The returned API MUST NOT be a public interface (it can be either just a struct
// with public methods, or perhaps a private interface), so that we can add methods
// without breaking any external implementers of a public interface.
func FromPublic(dest types.ImageDestination) private.ImageDestination {
	if dest2, ok := dest.(private.ImageDestination); ok {
		return dest2
	}
	return &wrapped{
		NoPutBlobPartialInitialize: stubs.NoPutBlobPartial(dest.Reference()),

		ImageDestination: dest,
	}
}

// PutBlobWithOptions writes contents of stream and returns data representing the result.
// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
// inputInfo.Size is the expected length of stream, if known.
// inputInfo.MediaType describes the blob format, if known.
// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
// to any other readers for download using the supplied digest.
// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlobWithOptions MUST 1) fail, and 2) delete any data stored so far.
func (w *wrapped) PutBlobWithOptions(ctx context.Context, stream io.Reader, inputInfo types.BlobInfo, options private.PutBlobOptions) (private.UploadedBlob, error) {
	res, err := w.PutBlob(ctx, stream, inputInfo, options.Cache, options.IsConfig)
	if err != nil {
		return private.UploadedBlob{}, err
	}
	return private.UploadedBlob{
		Digest: res.Digest,
		Size:   res.Size,
	}, nil
}

// TryReusingBlobWithOptions checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
// info.Digest must not be empty.
// If the blob has been successfully reused, returns (true, info, nil).
// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
func (w *wrapped) TryReusingBlobWithOptions(ctx context.Context, info types.BlobInfo, options private.TryReusingBlobOptions) (bool, private.ReusedBlob, error) {
	if options.RequiredCompression != nil {
		return false, private.ReusedBlob{}, nil
	}
	reused, blob, err := w.TryReusingBlob(ctx, info, options.Cache, options.CanSubstitute)
	if !reused || err != nil {
		return reused, private.ReusedBlob{}, err
	}
	return true, private.ReusedBlob{
		Digest:               blob.Digest,
		Size:                 blob.Size,
		CompressionOperation: blob.CompressionOperation,
		CompressionAlgorithm: blob.CompressionAlgorithm,
		// CompressionAnnotations could be set to blob.Annotations, but that may contain unrelated
		// annotations, and we didn’t use the blob.Annotations field previously, so we’ll
		// continue not using it.
	}, nil
}

// PutSignaturesWithFormat writes a set of signatures to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// MUST be called after PutManifest (signatures may reference manifest contents).
func (w *wrapped) PutSignaturesWithFormat(ctx context.Context, signatures []signature.Signature, instanceDigest *digest.Digest) error {
	simpleSigs := [][]byte{}
	for _, sig := range signatures {
		simpleSig, ok := sig.(signature.SimpleSigning)
		if !ok {
			return signature.UnsupportedFormatError(sig)
		}
		simpleSigs = append(simpleSigs, simpleSig.UntrustedSignature())
	}
	return w.PutSignatures(ctx, simpleSigs, instanceDigest)
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (w *wrapped) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	return w.Commit(ctx, options.UnparsedToplevel)
}
