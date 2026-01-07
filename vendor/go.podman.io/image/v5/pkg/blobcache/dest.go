package blobcache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/imagedestination"
	"go.podman.io/image/v5/internal/imagedestination/impl"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/ioutils"
)

type blobCacheDestination struct {
	impl.Compat

	reference   *BlobCache
	destination private.ImageDestination
}

func (b *BlobCache) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	dest, err := b.reference.NewImageDestination(ctx, sys)
	if err != nil {
		return nil, fmt.Errorf("error creating new image destination %q: %w", transports.ImageName(b.reference), err)
	}
	logrus.Debugf("starting to write to image %q using blob cache in %q", transports.ImageName(b.reference), b.directory)
	d := &blobCacheDestination{reference: b, destination: imagedestination.FromPublic(dest)}
	d.Compat = impl.AddCompat(d)
	return d, nil
}

func (d *blobCacheDestination) Reference() types.ImageReference {
	return d.reference
}

func (d *blobCacheDestination) Close() error {
	logrus.Debugf("finished writing to image %q using blob cache", transports.ImageName(d.reference))
	return d.destination.Close()
}

func (d *blobCacheDestination) SupportedManifestMIMETypes() []string {
	return d.destination.SupportedManifestMIMETypes()
}

func (d *blobCacheDestination) SupportsSignatures(ctx context.Context) error {
	return d.destination.SupportsSignatures(ctx)
}

func (d *blobCacheDestination) DesiredLayerCompression() types.LayerCompression {
	return d.destination.DesiredLayerCompression()
}

func (d *blobCacheDestination) AcceptsForeignLayerURLs() bool {
	return d.destination.AcceptsForeignLayerURLs()
}

func (d *blobCacheDestination) MustMatchRuntimeOS() bool {
	return d.destination.MustMatchRuntimeOS()
}

func (d *blobCacheDestination) IgnoresEmbeddedDockerReference() bool {
	return d.destination.IgnoresEmbeddedDockerReference()
}

// Decompress and save the contents of the decompressReader stream into the passed-in temporary
// file.  If we successfully save all of the data, rename the file to match the digest of the data,
// and make notes about the relationship between the file that holds a copy of the compressed data
// and this new file.
func (d *blobCacheDestination) saveStream(wg *sync.WaitGroup, decompressReader io.ReadCloser, tempFile *os.File, compressedFilename string, compressedDigest digest.Digest, isConfig bool, alternateDigest *digest.Digest) {
	defer wg.Done()
	defer decompressReader.Close()

	succeeded := false
	defer func() {
		if !succeeded {
			// Remove the temporary file.
			if err := os.Remove(tempFile.Name()); err != nil {
				logrus.Debugf("error cleaning up temporary file %q for decompressed copy of blob %q: %v", tempFile.Name(), compressedDigest.String(), err)
			}
		}
	}()

	digester := digest.Canonical.Digester()
	if err := func() error { // A scope for defer
		defer tempFile.Close()

		// Decompress from and digest the reading end of that pipe.
		decompressed, err := archive.DecompressStream(decompressReader)
		if err != nil {
			// Drain the pipe to keep from stalling the PutBlob() thread.
			if _, err2 := io.Copy(io.Discard, decompressReader); err2 != nil {
				logrus.Debugf("error draining the pipe: %v", err2)
			}
			return err
		}
		defer decompressed.Close()
		// Read the decompressed data through the filter over the pipe, blocking until the
		// writing end is closed.
		_, err = io.Copy(io.MultiWriter(tempFile, digester.Hash()), decompressed)
		return err
	}(); err != nil {
		return
	}

	// Determine the name that we should give to the uncompressed copy of the blob.
	decompressedFilename, err := d.reference.blobPath(digester.Digest(), isConfig)
	if err != nil {
		return
	}
	// Rename the temporary file.
	if err := os.Rename(tempFile.Name(), decompressedFilename); err != nil {
		logrus.Debugf("error renaming new decompressed copy of blob %q into place at %q: %v", digester.Digest().String(), decompressedFilename, err)
		return
	}
	succeeded = true
	*alternateDigest = digester.Digest()
	// Note the relationship between the two files.
	if err := ioutils.AtomicWriteFile(decompressedFilename+compressedNote, []byte(compressedDigest.String()), 0600); err != nil {
		logrus.Debugf("error noting that the compressed version of %q is %q: %v", digester.Digest().String(), compressedDigest.String(), err)
	}
	if err := ioutils.AtomicWriteFile(compressedFilename+decompressedNote, []byte(digester.Digest().String()), 0600); err != nil {
		logrus.Debugf("error noting that the decompressed version of %q is %q: %v", compressedDigest.String(), digester.Digest().String(), err)
	}
}

func (d *blobCacheDestination) HasThreadSafePutBlob() bool {
	return d.destination.HasThreadSafePutBlob()
}

// NoteOriginalOCIConfig provides the config of the image, as it exists on the source, BUT converted to OCI format,
// or an error obtaining that value (e.g. if the image is an artifact and not a container image).
// The destination can use it in its TryReusingBlob/PutBlob implementations
// (otherwise it only obtains the final config after all layers are written).
func (d *blobCacheDestination) NoteOriginalOCIConfig(ociConfig *imgspecv1.Image, configErr error) error {
	return d.destination.NoteOriginalOCIConfig(ociConfig, configErr)
}

// PutBlobWithOptions writes contents of stream and returns data representing the result.
// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
// inputInfo.Size is the expected length of stream, if known.
// inputInfo.MediaType describes the blob format, if known.
// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
// to any other readers for download using the supplied digest.
// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlobWithOptions MUST 1) fail, and 2) delete any data stored so far.
func (d *blobCacheDestination) PutBlobWithOptions(ctx context.Context, stream io.Reader, inputInfo types.BlobInfo, options private.PutBlobOptions) (private.UploadedBlob, error) {
	var tempfile *os.File
	var err error
	var n int
	var alternateDigest digest.Digest
	var closer io.Closer
	wg := new(sync.WaitGroup)
	needToWait := false
	compression := archive.Uncompressed
	if inputInfo.Digest != "" {
		filename, err2 := d.reference.blobPath(inputInfo.Digest, options.IsConfig)
		if err2 != nil {
			return private.UploadedBlob{}, err2
		}
		tempfile, err = os.CreateTemp(filepath.Dir(filename), filepath.Base(filename))
		if err == nil {
			stream = io.TeeReader(stream, tempfile)
			defer func() {
				if err == nil {
					if err = os.Rename(tempfile.Name(), filename); err != nil {
						if err2 := os.Remove(tempfile.Name()); err2 != nil {
							logrus.Debugf("error cleaning up temporary file %q for blob %q: %v", tempfile.Name(), inputInfo.Digest.String(), err2)
						}
						err = fmt.Errorf("error renaming new layer for blob %q into place at %q: %w", inputInfo.Digest.String(), filename, err)
					}
				} else {
					if err2 := os.Remove(tempfile.Name()); err2 != nil {
						logrus.Debugf("error cleaning up temporary file %q for blob %q: %v", tempfile.Name(), inputInfo.Digest.String(), err2)
					}
				}
				tempfile.Close()
			}()
		} else {
			logrus.Debugf("error while creating a temporary file under %q to hold blob %q: %v", filepath.Dir(filename), inputInfo.Digest.String(), err)
		}
		if !options.IsConfig {
			initial := make([]byte, 8)
			n, err = stream.Read(initial)
			if n > 0 {
				// Build a Reader that will still return the bytes that we just
				// read, for PutBlob()'s sake.
				stream = io.MultiReader(bytes.NewReader(initial[:n]), stream)
				if n >= len(initial) {
					compression = archive.DetectCompression(initial[:n])
				}
				if compression == archive.Gzip {
					// The stream is compressed, so create a file which we'll
					// use to store a decompressed copy.
					decompressedTemp, err2 := os.CreateTemp(filepath.Dir(filename), filepath.Base(filename))
					if err2 != nil {
						logrus.Debugf("error while creating a temporary file under %q to hold decompressed blob %q: %v", filepath.Dir(filename), inputInfo.Digest.String(), err2)
					} else {
						// Write a copy of the compressed data to a pipe,
						// closing the writing end of the pipe after
						// PutBlob() returns.
						decompressReader, decompressWriter := io.Pipe()
						closer = decompressWriter
						stream = io.TeeReader(stream, decompressWriter)
						// Let saveStream() close the reading end and handle the temporary file.
						wg.Add(1)
						needToWait = true
						go d.saveStream(wg, decompressReader, decompressedTemp, filename, inputInfo.Digest, options.IsConfig, &alternateDigest)
					}
				}
			}
		}
	}
	newBlobInfo, err := d.destination.PutBlobWithOptions(ctx, stream, inputInfo, options)
	if closer != nil {
		closer.Close()
	}
	if needToWait {
		wg.Wait()
	}
	if err != nil {
		return newBlobInfo, fmt.Errorf("error storing blob to image destination for cache %q: %w", transports.ImageName(d.reference), err)
	}
	if alternateDigest.Validate() == nil {
		logrus.Debugf("added blob %q (also %q) to the cache at %q", inputInfo.Digest.String(), alternateDigest.String(), d.reference.directory)
	} else {
		logrus.Debugf("added blob %q to the cache at %q", inputInfo.Digest.String(), d.reference.directory)
	}
	return newBlobInfo, nil
}

// SupportsPutBlobPartial returns true if PutBlobPartial is supported.
func (d *blobCacheDestination) SupportsPutBlobPartial() bool {
	return d.destination.SupportsPutBlobPartial()
}

// PutBlobPartial attempts to create a blob using the data that is already present
// at the destination. chunkAccessor is accessed in a non-sequential way to retrieve the missing chunks.
// It is available only if SupportsPutBlobPartial().
// Even if SupportsPutBlobPartial() returns true, the call can fail.
// If the call fails with ErrFallbackToOrdinaryLayerDownload, the caller can fall back to PutBlobWithOptions.
// The fallback _must not_ be done otherwise.
func (d *blobCacheDestination) PutBlobPartial(ctx context.Context, chunkAccessor private.BlobChunkAccessor, srcInfo types.BlobInfo, options private.PutBlobPartialOptions) (private.UploadedBlob, error) {
	return d.destination.PutBlobPartial(ctx, chunkAccessor, srcInfo, options)
}

// TryReusingBlobWithOptions checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
// info.Digest must not be empty.
// If the blob has been successfully reused, returns (true, info, nil).
// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
func (d *blobCacheDestination) TryReusingBlobWithOptions(ctx context.Context, info types.BlobInfo, options private.TryReusingBlobOptions) (bool, private.ReusedBlob, error) {
	if !impl.OriginalCandidateMatchesTryReusingBlobOptions(options) {
		return false, private.ReusedBlob{}, nil
	}
	present, reusedInfo, err := d.destination.TryReusingBlobWithOptions(ctx, info, options)
	if err != nil || present {
		return present, reusedInfo, err
	}

	blobPath, _, isConfig, err := d.reference.findBlob(info)
	if err != nil {
		return false, private.ReusedBlob{}, err
	}
	if blobPath != "" {
		f, err := os.Open(blobPath)
		if err == nil {
			defer f.Close()
			uploadedInfo, err := d.destination.PutBlobWithOptions(ctx, f, info, private.PutBlobOptions{
				Cache:      options.Cache,
				IsConfig:   isConfig,
				EmptyLayer: options.EmptyLayer,
				LayerIndex: options.LayerIndex,
			})
			if err != nil {
				return false, private.ReusedBlob{}, err
			}
			return true, private.ReusedBlob{Digest: uploadedInfo.Digest, Size: uploadedInfo.Size}, nil
		}
	}

	return false, private.ReusedBlob{}, nil
}

func (d *blobCacheDestination) PutManifest(ctx context.Context, manifestBytes []byte, instanceDigest *digest.Digest) error {
	manifestDigest, err := manifest.Digest(manifestBytes)
	if err != nil {
		logrus.Warnf("error digesting manifest %q: %v", string(manifestBytes), err)
	} else {
		filename, err := d.reference.blobPath(manifestDigest, false)
		if err != nil {
			return err
		}
		if err = ioutils.AtomicWriteFile(filename, manifestBytes, 0600); err != nil {
			logrus.Warnf("error saving manifest as %q: %v", filename, err)
		}
	}
	return d.destination.PutManifest(ctx, manifestBytes, instanceDigest)
}

// PutSignaturesWithFormat writes a set of signatures to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// MUST be called after PutManifest (signatures may reference manifest contents).
func (d *blobCacheDestination) PutSignaturesWithFormat(ctx context.Context, signatures []signature.Signature, instanceDigest *digest.Digest) error {
	return d.destination.PutSignaturesWithFormat(ctx, signatures, instanceDigest)
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (d *blobCacheDestination) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	return d.destination.CommitWithOptions(ctx, options)
}
