package archive

import (
	"context"
	"errors"
	"fmt"
	"io"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/imagesource"
	"go.podman.io/image/v5/internal/imagesource/impl"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	ocilayout "go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/types"
)

// ImageNotFoundError is used when the OCI structure, in principle, exists and seems valid enough,
// but nothing matches the “image” part of the provided reference.
type ImageNotFoundError struct {
	ref ociArchiveReference
	// We may make members public, or add methods, in the future.
}

func (e ImageNotFoundError) Error() string {
	return fmt.Sprintf("no descriptor found for reference %q", e.ref.image)
}

// ArchiveFileNotFoundError occurs when the archive file does not exist.
type ArchiveFileNotFoundError struct {
	// ref is the image reference
	ref ociArchiveReference
	// path is the file path that was not present
	path string
}

func (e ArchiveFileNotFoundError) Error() string {
	return fmt.Sprintf("archive file not found: %q", e.path)
}

type ociArchiveImageSource struct {
	impl.Compat

	ref         ociArchiveReference
	unpackedSrc private.ImageSource
	tempDirRef  tempDirOCIRef
}

// newImageSource returns an ImageSource for reading from an existing directory.
// newImageSource untars the file and saves it in a temp directory
func newImageSource(ctx context.Context, sys *types.SystemContext, ref ociArchiveReference) (private.ImageSource, error) {
	tempDirRef, err := createUntarTempDir(sys, ref)
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}

	unpackedSrc, err := tempDirRef.ociRefExtracted.NewImageSource(ctx, sys)
	if err != nil {
		var notFound ocilayout.ImageNotFoundError
		if errors.As(err, &notFound) {
			err = ImageNotFoundError{ref: ref}
		}
		if err := tempDirRef.deleteTempDir(); err != nil {
			return nil, fmt.Errorf("deleting temp directory %q: %w", tempDirRef.tempDirectory, err)
		}
		return nil, err
	}
	s := &ociArchiveImageSource{
		ref:         ref,
		unpackedSrc: imagesource.FromPublic(unpackedSrc),
		tempDirRef:  tempDirRef,
	}
	s.Compat = impl.AddCompat(s)
	return s, nil
}

// LoadManifestDescriptor loads the manifest
// Deprecated: use LoadManifestDescriptorWithContext instead
func LoadManifestDescriptor(imgRef types.ImageReference) (imgspecv1.Descriptor, error) {
	return LoadManifestDescriptorWithContext(nil, imgRef)
}

// LoadManifestDescriptorWithContext loads the manifest
func LoadManifestDescriptorWithContext(sys *types.SystemContext, imgRef types.ImageReference) (imgspecv1.Descriptor, error) {
	ociArchRef, ok := imgRef.(ociArchiveReference)
	if !ok {
		return imgspecv1.Descriptor{}, errors.New("error typecasting, need type ociArchiveReference")
	}
	tempDirRef, err := createUntarTempDir(sys, ociArchRef)
	if err != nil {
		return imgspecv1.Descriptor{}, fmt.Errorf("creating temp directory: %w", err)
	}
	defer func() {
		err := tempDirRef.deleteTempDir()
		logrus.Debugf("Error deleting temporary directory: %v", err)
	}()

	descriptor, err := ocilayout.LoadManifestDescriptor(tempDirRef.ociRefExtracted)
	if err != nil {
		return imgspecv1.Descriptor{}, fmt.Errorf("loading index: %w", err)
	}
	return descriptor, nil
}

// Reference returns the reference used to set up this source.
func (s *ociArchiveImageSource) Reference() types.ImageReference {
	return s.ref
}

// Close removes resources associated with an initialized ImageSource, if any.
// Close deletes the temporary directory at dst
func (s *ociArchiveImageSource) Close() error {
	defer func() {
		err := s.tempDirRef.deleteTempDir()
		logrus.Debugf("error deleting tmp dir: %v", err)
	}()
	return s.unpackedSrc.Close()
}

// GetManifest returns the image's manifest along with its MIME type (which may be empty when it can't be determined but the manifest is available).
// It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve (when the primary manifest is a manifest list);
// this never happens if the primary manifest is not a manifest list (e.g. if the source never returns manifest lists).
func (s *ociArchiveImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	return s.unpackedSrc.GetManifest(ctx, instanceDigest)
}

// HasThreadSafeGetBlob indicates whether GetBlob can be executed concurrently.
func (s *ociArchiveImageSource) HasThreadSafeGetBlob() bool {
	return false
}

// GetBlob returns a stream for the specified blob, and the blob’s size (or -1 if unknown).
// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
func (s *ociArchiveImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	return s.unpackedSrc.GetBlob(ctx, info, cache)
}

// SupportsGetBlobAt() returns true if GetBlobAt (BlobChunkAccessor) is supported.
func (s *ociArchiveImageSource) SupportsGetBlobAt() bool {
	return s.unpackedSrc.SupportsGetBlobAt()
}

// GetBlobAt returns a sequential channel of readers that contain data for the requested
// blob chunks, and a channel that might get a single error value.
// The specified chunks must be not overlapping and sorted by their offset.
// The readers must be fully consumed, in the order they are returned, before blocking
// to read the next chunk.
// If the Length for the last chunk is set to math.MaxUint64, then it
// fully fetches the remaining data from the offset to the end of the blob.
func (s *ociArchiveImageSource) GetBlobAt(ctx context.Context, info types.BlobInfo, chunks []private.ImageSourceChunk) (chan io.ReadCloser, chan error, error) {
	return s.unpackedSrc.GetBlobAt(ctx, info, chunks)
}

// GetSignaturesWithFormat returns the image's signatures.  It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
func (s *ociArchiveImageSource) GetSignaturesWithFormat(ctx context.Context, instanceDigest *digest.Digest) ([]signature.Signature, error) {
	return s.unpackedSrc.GetSignaturesWithFormat(ctx, instanceDigest)
}

// LayerInfosForCopy returns either nil (meaning the values in the manifest are fine), or updated values for the layer
// blobsums that are listed in the image's manifest.  If values are returned, they should be used when using GetBlob()
// to read the image's layers.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve BlobInfos for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (s *ociArchiveImageSource) LayerInfosForCopy(ctx context.Context, instanceDigest *digest.Digest) ([]types.BlobInfo, error) {
	return s.unpackedSrc.LayerInfosForCopy(ctx, instanceDigest)
}
