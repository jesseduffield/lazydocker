package archive

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/imagedestination"
	"go.podman.io/image/v5/internal/imagedestination/impl"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/idtools"
)

type ociArchiveImageDestination struct {
	impl.Compat

	ref          ociArchiveReference
	unpackedDest private.ImageDestination
	tempDirRef   tempDirOCIRef
}

// newImageDestination returns an ImageDestination for writing to an existing directory.
func newImageDestination(ctx context.Context, sys *types.SystemContext, ref ociArchiveReference) (private.ImageDestination, error) {
	tempDirRef, err := createOCIRef(sys, ref.image)
	if err != nil {
		return nil, fmt.Errorf("creating oci reference: %w", err)
	}
	unpackedDest, err := tempDirRef.ociRefExtracted.NewImageDestination(ctx, sys)
	if err != nil {
		if err := tempDirRef.deleteTempDir(); err != nil {
			return nil, fmt.Errorf("deleting temp directory %q: %w", tempDirRef.tempDirectory, err)
		}
		return nil, err
	}
	d := &ociArchiveImageDestination{
		ref:          ref,
		unpackedDest: imagedestination.FromPublic(unpackedDest),
		tempDirRef:   tempDirRef,
	}
	d.Compat = impl.AddCompat(d)
	return d, nil
}

// Reference returns the reference used to set up this destination.
func (d *ociArchiveImageDestination) Reference() types.ImageReference {
	return d.ref
}

// Close removes resources associated with an initialized ImageDestination, if any
// Close deletes the temp directory of the oci-archive image
func (d *ociArchiveImageDestination) Close() error {
	defer func() {
		err := d.tempDirRef.deleteTempDir()
		logrus.Debugf("Error deleting temporary directory: %v", err)
	}()
	return d.unpackedDest.Close()
}

func (d *ociArchiveImageDestination) SupportedManifestMIMETypes() []string {
	return d.unpackedDest.SupportedManifestMIMETypes()
}

// SupportsSignatures returns an error (to be displayed to the user) if the destination certainly can't store signatures
func (d *ociArchiveImageDestination) SupportsSignatures(ctx context.Context) error {
	return d.unpackedDest.SupportsSignatures(ctx)
}

func (d *ociArchiveImageDestination) DesiredLayerCompression() types.LayerCompression {
	return d.unpackedDest.DesiredLayerCompression()
}

// AcceptsForeignLayerURLs returns false iff foreign layers in manifest should be actually
// uploaded to the image destination, true otherwise.
func (d *ociArchiveImageDestination) AcceptsForeignLayerURLs() bool {
	return d.unpackedDest.AcceptsForeignLayerURLs()
}

// MustMatchRuntimeOS returns true iff the destination can store only images targeted for the current runtime architecture and OS. False otherwise
func (d *ociArchiveImageDestination) MustMatchRuntimeOS() bool {
	return d.unpackedDest.MustMatchRuntimeOS()
}

// IgnoresEmbeddedDockerReference returns true iff the destination does not care about Image.EmbeddedDockerReferenceConflicts(),
// and would prefer to receive an unmodified manifest instead of one modified for the destination.
// Does not make a difference if Reference().DockerReference() is nil.
func (d *ociArchiveImageDestination) IgnoresEmbeddedDockerReference() bool {
	return d.unpackedDest.IgnoresEmbeddedDockerReference()
}

// HasThreadSafePutBlob indicates whether PutBlob can be executed concurrently.
func (d *ociArchiveImageDestination) HasThreadSafePutBlob() bool {
	return false
}

// SupportsPutBlobPartial returns true if PutBlobPartial is supported.
func (d *ociArchiveImageDestination) SupportsPutBlobPartial() bool {
	return d.unpackedDest.SupportsPutBlobPartial()
}

// NoteOriginalOCIConfig provides the config of the image, as it exists on the source, BUT converted to OCI format,
// or an error obtaining that value (e.g. if the image is an artifact and not a container image).
// The destination can use it in its TryReusingBlob/PutBlob implementations
// (otherwise it only obtains the final config after all layers are written).
func (d *ociArchiveImageDestination) NoteOriginalOCIConfig(ociConfig *imgspecv1.Image, configErr error) error {
	return d.unpackedDest.NoteOriginalOCIConfig(ociConfig, configErr)
}

// PutBlobWithOptions writes contents of stream and returns data representing the result.
// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
// inputInfo.Size is the expected length of stream, if known.
// inputInfo.MediaType describes the blob format, if known.
// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
// to any other readers for download using the supplied digest.
// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlobWithOptions MUST 1) fail, and 2) delete any data stored so far.
func (d *ociArchiveImageDestination) PutBlobWithOptions(ctx context.Context, stream io.Reader, inputInfo types.BlobInfo, options private.PutBlobOptions) (private.UploadedBlob, error) {
	return d.unpackedDest.PutBlobWithOptions(ctx, stream, inputInfo, options)
}

// PutBlobPartial attempts to create a blob using the data that is already present
// at the destination. chunkAccessor is accessed in a non-sequential way to retrieve the missing chunks.
// It is available only if SupportsPutBlobPartial().
// Even if SupportsPutBlobPartial() returns true, the call can fail.
// If the call fails with ErrFallbackToOrdinaryLayerDownload, the caller can fall back to PutBlobWithOptions.
// The fallback _must not_ be done otherwise.
func (d *ociArchiveImageDestination) PutBlobPartial(ctx context.Context, chunkAccessor private.BlobChunkAccessor, srcInfo types.BlobInfo, options private.PutBlobPartialOptions) (private.UploadedBlob, error) {
	return d.unpackedDest.PutBlobPartial(ctx, chunkAccessor, srcInfo, options)
}

// TryReusingBlobWithOptions checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
// info.Digest must not be empty.
// If the blob has been successfully reused, returns (true, info, nil).
// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
func (d *ociArchiveImageDestination) TryReusingBlobWithOptions(ctx context.Context, info types.BlobInfo, options private.TryReusingBlobOptions) (bool, private.ReusedBlob, error) {
	return d.unpackedDest.TryReusingBlobWithOptions(ctx, info, options)
}

// PutManifest writes the manifest to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to overwrite the manifest for (when
// the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// It is expected but not enforced that the instanceDigest, when specified, matches the digest of `manifest` as generated
// by `manifest.Digest()`.
func (d *ociArchiveImageDestination) PutManifest(ctx context.Context, m []byte, instanceDigest *digest.Digest) error {
	return d.unpackedDest.PutManifest(ctx, m, instanceDigest)
}

// PutSignaturesWithFormat writes a set of signatures to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// MUST be called after PutManifest (signatures may reference manifest contents).
func (d *ociArchiveImageDestination) PutSignaturesWithFormat(ctx context.Context, signatures []signature.Signature, instanceDigest *digest.Digest) error {
	return d.unpackedDest.PutSignaturesWithFormat(ctx, signatures, instanceDigest)
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (d *ociArchiveImageDestination) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	if err := d.unpackedDest.CommitWithOptions(ctx, options); err != nil {
		return fmt.Errorf("storing image %q: %w", d.ref.image, err)
	}

	// path of directory to tar up
	src := d.tempDirRef.tempDirectory
	// path to save tarred up file
	dst := d.ref.resolvedFile
	return tarDirectory(src, dst, options.Timestamp)
}

// tar converts the directory at src and saves it to dst
// if contentModTimes is non-nil, tar header entries times are set to this
func tarDirectory(src, dst string, contentModTimes *time.Time) (retErr error) {
	// input is a stream of bytes from the archive of the directory at path
	input, err := archive.TarWithOptions(src, &archive.TarOptions{
		Compression: archive.Uncompressed,
		// Don’t include the data about the user account this code is running under.
		ChownOpts: &idtools.IDPair{UID: 0, GID: 0},
		// override tar header timestamps
		Timestamp: contentModTimes,
	})
	if err != nil {
		return fmt.Errorf("retrieving stream of bytes from %q: %w", src, err)
	}
	defer input.Close()

	// creates the tar file
	outFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating tar file %q: %w", dst, err)
	}

	// since we are writing to this file, make sure we handle errors
	defer func() {
		closeErr := outFile.Close()
		if retErr == nil {
			retErr = closeErr
		}
	}()

	// copies the contents of the directory to the tar file
	// TODO: This can take quite some time, and should ideally be cancellable using a context.Context.
	_, err = io.Copy(outFile, input)

	return err
}
