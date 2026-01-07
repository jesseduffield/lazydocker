package impl

import (
	"context"
	"io"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/blobinfocache"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/types"
)

// Compat implements the obsolete parts of types.ImageDestination
// for implementations of private.ImageDestination.
// See AddCompat below.
type Compat struct {
	dest private.ImageDestinationInternalOnly
}

// AddCompat initializes Compat to implement the obsolete parts of types.ImageDestination
// for implementations of private.ImageDestination.
//
// Use it like this:
//
//	type yourDestination struct {
//		impl.Compat
//		…
//	}
//
//	dest := &yourDestination{…}
//	dest.Compat = impl.AddCompat(dest)
func AddCompat(dest private.ImageDestinationInternalOnly) Compat {
	return Compat{dest}
}

// PutBlob writes contents of stream and returns data representing the result.
// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
// inputInfo.Size is the expected length of stream, if known.
// inputInfo.MediaType describes the blob format, if known.
// May update cache.
// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
// to any other readers for download using the supplied digest.
// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlob MUST 1) fail, and 2) delete any data stored so far.
func (c *Compat) PutBlob(ctx context.Context, stream io.Reader, inputInfo types.BlobInfo, cache types.BlobInfoCache, isConfig bool) (types.BlobInfo, error) {
	res, err := c.dest.PutBlobWithOptions(ctx, stream, inputInfo, private.PutBlobOptions{
		Cache:    blobinfocache.FromBlobInfoCache(cache),
		IsConfig: isConfig,
	})
	if err != nil {
		return types.BlobInfo{}, err
	}
	return types.BlobInfo{
		Digest: res.Digest,
		Size:   res.Size,
	}, nil
}

// TryReusingBlob checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
// info.Digest must not be empty.
// If canSubstitute, TryReusingBlob can use an equivalent equivalent of the desired blob; in that case the returned info may not match the input.
// If the blob has been successfully reused, returns (true, info, nil); info must contain at least a digest and size, and may
// include CompressionOperation and CompressionAlgorithm fields to indicate that a change to the compression type should be
// reflected in the manifest that will be written.
// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
// May use and/or update cache.
func (c *Compat) TryReusingBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache, canSubstitute bool) (bool, types.BlobInfo, error) {
	reused, blob, err := c.dest.TryReusingBlobWithOptions(ctx, info, private.TryReusingBlobOptions{
		Cache:         blobinfocache.FromBlobInfoCache(cache),
		CanSubstitute: canSubstitute,
	})
	if !reused || err != nil {
		return reused, types.BlobInfo{}, err
	}
	res := types.BlobInfo{
		Digest:               blob.Digest,
		Size:                 blob.Size,
		CompressionOperation: blob.CompressionOperation,
		CompressionAlgorithm: blob.CompressionAlgorithm,
	}
	// This is probably not necessary; we preserve MediaType to decrease risks of breaking for external callers.
	// Some transports were not setting the MediaType field anyway, and others were setting the old value on substitution;
	// provide the value in cases where it is likely to be correct.
	if blob.Digest == info.Digest {
		res.MediaType = info.MediaType
	}
	return true, res, nil
}

// PutSignatures writes a set of signatures to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// MUST be called after PutManifest (signatures may reference manifest contents).
func (c *Compat) PutSignatures(ctx context.Context, signatures [][]byte, instanceDigest *digest.Digest) error {
	withFormat := []signature.Signature{}
	for _, sig := range signatures {
		withFormat = append(withFormat, signature.SimpleSigningFromBlob(sig))
	}
	return c.dest.PutSignaturesWithFormat(ctx, withFormat, instanceDigest)
}

// Commit marks the process of storing the image as successful and asks for the image to be persisted.
// unparsedToplevel contains data about the top-level manifest of the source (which may be a single-arch image or a manifest list
// if PutManifest was only called for the single-arch image with instanceDigest == nil), primarily to allow lookups by the
// original manifest list digest, if desired.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before Commit() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without Commit() (i.e. rollback is allowed but not guaranteed)
func (c *Compat) Commit(ctx context.Context, unparsedToplevel types.UnparsedImage) error {
	return c.dest.CommitWithOptions(ctx, private.CommitOptions{
		UnparsedToplevel: unparsedToplevel,
	})
}
