//go:build !containers_image_storage_stub

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/internal/imagedestination/impl"
	"go.podman.io/image/v5/internal/imagedestination/stubs"
	srcImpl "go.podman.io/image/v5/internal/imagesource/impl"
	srcStubs "go.podman.io/image/v5/internal/imagesource/stubs"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/putblobdigest"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/internal/tmpdir"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/blobinfocache/none"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	graphdriver "go.podman.io/storage/drivers"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chunked"
	"go.podman.io/storage/pkg/chunked/toc"
	"go.podman.io/storage/pkg/ioutils"
)

var (
	// ErrBlobDigestMismatch could potentially be returned when PutBlob() is given a blob
	// with a digest-based name that doesn't match its contents.
	// Deprecated: PutBlob() doesn't do this any more (it just accepts the caller’s value),
	// and there is no known user of this error.
	ErrBlobDigestMismatch = errors.New("blob digest mismatch")
	// ErrBlobSizeMismatch is returned when PutBlob() is given a blob
	// with an expected size that doesn't match the reader.
	ErrBlobSizeMismatch = errors.New("blob size mismatch")
)

type storageImageDestination struct {
	impl.Compat
	impl.PropertyMethodsInitialize
	stubs.ImplementsPutBlobPartial
	stubs.AlwaysSupportsSignatures

	imageRef              storageReference
	directory             string                   // Temporary directory where we store blobs until Commit() time
	nextTempFileID        atomic.Int32             // A counter that we use for computing filenames to assign to blobs
	manifest              []byte                   // (Per-instance) manifest contents, or nil if not yet known.
	manifestMIMEType      string                   // Valid if manifest != nil
	manifestDigest        digest.Digest            // Valid if manifest != nil
	untrustedDiffIDValues []digest.Digest          // From config’s RootFS.DiffIDs (not even validated to be valid digest.Digest!); or nil if not read yet
	signatures            []byte                   // Signature contents, temporary
	signatureses          map[digest.Digest][]byte // Instance signature contents, temporary
	metadata              storageImageMetadata     // Metadata contents being built

	// Mapping from layer (by index) to the associated ID in the storage.
	// It's protected *implicitly* since `commitLayer()`, at any given
	// time, can only be executed by *one* goroutine.  Please refer to
	// `queueOrCommit()` for further details on how the single-caller
	// guarantee is implemented.
	indexToStorageID map[int]string

	// A storage destination may be used concurrently, due to HasThreadSafePutBlob.
	lock          sync.Mutex // Protects lockProtected
	lockProtected storageImageDestinationLockProtected
}

// storageImageDestinationLockProtected contains storageImageDestination data which might be
// accessed concurrently, due to HasThreadSafePutBlob.
// _During the concurrent TryReusingBlob/PutBlob/* calls_ (but not necessarily during the final Commit)
// uses must hold storageImageDestination.lock.
type storageImageDestinationLockProtected struct {
	currentIndex          int                    // The index of the layer to be committed (i.e., lower indices have already been committed)
	indexToAddedLayerInfo map[int]addedLayerInfo // Mapping from layer (by index) to blob to add to the image

	// Externally, a layer is identified either by (compressed) digest, or by TOC digest
	// (and we assume the TOC digest also uniquely identifies the contents, i.e. there aren’t two
	// different formats/ways to parse a single TOC); internally, we use uncompressed digest (“DiffID”) or a TOC digest.
	// We may or may not know the relationships between these three values.
	//
	// When creating a layer, the c/storage layer metadata and image IDs must _only_ be based on trusted values
	// we have computed ourselves. (Layer reuse can then look up against such trusted values, but it might not
	// recompute those values for incoming layers — the point of the reuse is that we don’t need to consume the incoming layer.)
	//
	// Layer identification: For a layer, at least one of (indexToDiffID, indexToTOCDigest, blobDiffIDs) must be available
	// before commitLayer is called.
	// The layer is identified by the first of the three fields which exists, in that order (and the value must be trusted).
	//
	// WARNING: All values in indexToDiffID, indexToTOCDigest, and blobDiffIDs are _individually_ trusted, but blobDiffIDs is more subtle.
	// The values in indexTo* are all consistent, because the code writing them processed them all at once, and consistently.
	// But it is possible for a layer’s indexToDiffID an indexToTOCDigest to be based on a TOC, without setting blobDiffIDs
	// for the compressed digest of that index, and for blobDiffIDs[compressedDigest] to be set _separately_ while processing some
	// other layer entry. In particular it is possible for indexToDiffID[index] and blobDiffIDs[compressedDigestAtIndex]] to refer
	// to mismatching contents.
	// Users of these fields should use trustedLayerIdentityDataLocked, which centralizes the validity logic,
	// instead of interpreting these fields, especially blobDiffIDs, directly.
	//
	// Ideally we wouldn’t have blobDiffIDs, and we would just keep records by index, but the public API does not require the caller
	// to provide layer indices; and configs don’t have layer indices. blobDiffIDs needs to exist for those cases.
	indexToDiffID map[int]digest.Digest // Mapping from layer index to DiffID
	// Mapping from layer index to a TOC Digest.
	// If this is set, then either c/storage/pkg/chunked/toc.GetTOCDigest must have returned a value, or indexToDiffID must be set as well.
	indexToTOCDigest map[int]digest.Digest
	blobDiffIDs      map[digest.Digest]digest.Digest // Mapping from layer blobsums to their corresponding DiffIDs. CAREFUL: See the WARNING above.

	// Layer data: Before commitLayer is called, either at least one of (diffOutputs, indexToAdditionalLayer, filenames)
	// should be available; or indexToDiffID/indexToTOCDigest/blobDiffIDs should be enough to locate an existing c/storage layer.
	// They are looked up in the order they are mentioned above.
	diffOutputs            map[int]*graphdriver.DriverWithDifferOutput // Mapping from layer index to a partially-pulled layer intermediate data
	indexToAdditionalLayer map[int]storage.AdditionalLayer             // Mapping from layer index to their corresponding additional layer
	// Mapping from layer blobsums to names of files we used to hold them. If set, fileSizes and blobDiffIDs must also be set.
	filenames map[digest.Digest]string
	// Mapping from layer blobsums to their sizes. If set, filenames and blobDiffIDs must also be set.
	fileSizes map[digest.Digest]int64

	// Config
	configDigest digest.Digest // "" if N/A or not known yet.
}

// addedLayerInfo records data about a layer to use in this image.
type addedLayerInfo struct {
	digest     digest.Digest // Mandatory, the digest of the layer.
	emptyLayer bool          // The layer is an “empty”/“throwaway” one, and may or may not be physically represented in various transport / storage systems.  false if the manifest type does not have the concept.
}

// newImageDestination sets us up to write a new image, caching blobs in a temporary directory until
// it's time to Commit() the image
func newImageDestination(sys *types.SystemContext, imageRef storageReference) (*storageImageDestination, error) {
	directory, err := tmpdir.MkDirBigFileTemp(sys, "storage")
	if err != nil {
		return nil, fmt.Errorf("creating a temporary directory: %w", err)
	}
	dest := &storageImageDestination{
		PropertyMethodsInitialize: impl.PropertyMethods(impl.Properties{
			SupportedManifestMIMETypes: []string{
				imgspecv1.MediaTypeImageManifest,
				manifest.DockerV2Schema2MediaType,
				manifest.DockerV2Schema1SignedMediaType,
				manifest.DockerV2Schema1MediaType,
			},
			// We ultimately have to decompress layers to populate trees on disk
			// and need to explicitly ask for it here, so that the layers' MIME
			// types can be set accordingly.
			DesiredLayerCompression:        types.PreserveOriginal,
			AcceptsForeignLayerURLs:        false,
			MustMatchRuntimeOS:             true,
			IgnoresEmbeddedDockerReference: true, // Yes, we want the unmodified manifest
			HasThreadSafePutBlob:           true,
		}),

		imageRef:     imageRef,
		directory:    directory,
		signatureses: make(map[digest.Digest][]byte),
		metadata: storageImageMetadata{
			SignatureSizes:  []int{},
			SignaturesSizes: make(map[digest.Digest][]int),
		},
		indexToStorageID: make(map[int]string),
		lockProtected: storageImageDestinationLockProtected{
			indexToAddedLayerInfo: make(map[int]addedLayerInfo),

			indexToDiffID:    make(map[int]digest.Digest),
			indexToTOCDigest: make(map[int]digest.Digest),
			blobDiffIDs:      make(map[digest.Digest]digest.Digest),

			diffOutputs:            make(map[int]*graphdriver.DriverWithDifferOutput),
			indexToAdditionalLayer: make(map[int]storage.AdditionalLayer),
			filenames:              make(map[digest.Digest]string),
			fileSizes:              make(map[digest.Digest]int64),
		},
	}
	dest.Compat = impl.AddCompat(dest)
	return dest, nil
}

// Reference returns the reference used to set up this destination.  Note that this should directly correspond to user's intent,
// e.g. it should use the public hostname instead of the result of resolving CNAMEs or following redirects.
func (s *storageImageDestination) Reference() types.ImageReference {
	return s.imageRef
}

// Close cleans up the temporary directory and additional layer store handlers.
func (s *storageImageDestination) Close() error {
	// This is outside of the scope of HasThreadSafePutBlob, so we don’t need to hold s.lock.
	for _, al := range s.lockProtected.indexToAdditionalLayer {
		al.Release()
	}
	for _, v := range s.lockProtected.diffOutputs {
		_ = s.imageRef.transport.store.CleanupStagedLayer(v)
	}
	return os.RemoveAll(s.directory)
}

func (s *storageImageDestination) computeNextBlobCacheFile() string {
	return filepath.Join(s.directory, fmt.Sprintf("%d", s.nextTempFileID.Add(1)))
}

// NoteOriginalOCIConfig provides the config of the image, as it exists on the source, BUT converted to OCI format,
// or an error obtaining that value (e.g. if the image is an artifact and not a container image).
// The destination can use it in its TryReusingBlob/PutBlob implementations
// (otherwise it only obtains the final config after all layers are written).
func (s *storageImageDestination) NoteOriginalOCIConfig(ociConfig *imgspecv1.Image, configErr error) error {
	if configErr != nil {
		return fmt.Errorf("writing to c/storage without a valid image config: %w", configErr)
	}
	s.setUntrustedDiffIDValuesFromOCIConfig(ociConfig)
	return nil
}

// PutBlobWithOptions writes contents of stream and returns data representing the result.
// inputInfo.Digest can be optionally provided if known; if provided, and stream is read to the end without error, the digest MUST match the stream contents.
// inputInfo.Size is the expected length of stream, if known.
// inputInfo.MediaType describes the blob format, if known.
// WARNING: The contents of stream are being verified on the fly.  Until stream.Read() returns io.EOF, the contents of the data SHOULD NOT be available
// to any other readers for download using the supplied digest.
// If stream.Read() at any time, ESPECIALLY at end of input, returns an error, PutBlob MUST 1) fail, and 2) delete any data stored so far.
func (s *storageImageDestination) PutBlobWithOptions(ctx context.Context, stream io.Reader, blobinfo types.BlobInfo, options private.PutBlobOptions) (private.UploadedBlob, error) {
	info, err := s.putBlobToPendingFile(stream, blobinfo, &options)
	if err != nil {
		return info, err
	}

	if options.IsConfig {
		s.lock.Lock()
		defer s.lock.Unlock()
		if s.lockProtected.configDigest != "" {
			return private.UploadedBlob{}, fmt.Errorf("after config %q, refusing to record another config %q",
				s.lockProtected.configDigest.String(), info.Digest.String())
		}
		s.lockProtected.configDigest = info.Digest
		return info, nil
	}
	if options.LayerIndex == nil {
		return info, nil
	}

	return info, s.queueOrCommit(*options.LayerIndex, addedLayerInfo{
		digest:     info.Digest,
		emptyLayer: options.EmptyLayer,
	})
}

// putBlobToPendingFile implements ImageDestination.PutBlobWithOptions, storing stream into an on-disk file.
// The caller must arrange the blob to be eventually committed using s.commitLayer().
func (s *storageImageDestination) putBlobToPendingFile(stream io.Reader, blobinfo types.BlobInfo, options *private.PutBlobOptions) (private.UploadedBlob, error) {
	// Stores a layer or data blob in our temporary directory, checking that any information
	// in the blobinfo matches the incoming data.
	if blobinfo.Digest != "" {
		if err := blobinfo.Digest.Validate(); err != nil {
			return private.UploadedBlob{}, fmt.Errorf("invalid digest %#v: %w", blobinfo.Digest.String(), err)
		}
	}

	// Set up to digest the blob if necessary, and count its size while saving it to a file.
	filename := s.computeNextBlobCacheFile()
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return private.UploadedBlob{}, fmt.Errorf("creating temporary file %q: %w", filename, err)
	}
	blobDigest, diffID, count, err := func() (_, _ digest.Digest, _ int64, retErr error) { // A scope for defer
		// since we are writing to this file, make sure we handle err on Close()
		defer func() {
			closeErr := file.Close()
			if retErr == nil {
				retErr = closeErr
			}
		}()
		counter := ioutils.NewWriteCounter(file)
		stream = io.TeeReader(stream, counter)
		digester, stream := putblobdigest.DigestIfUnknown(stream, blobinfo)
		decompressed, err := archive.DecompressStream(stream)
		if err != nil {
			return "", "", 0, fmt.Errorf("setting up to decompress blob: %w", err)

		}
		defer decompressed.Close()

		diffID := digest.Canonical.Digester()
		// Copy the data to the file.
		// TODO: This can take quite some time, and should ideally be cancellable using context.Context.
		_, err = io.Copy(diffID.Hash(), decompressed)
		if err != nil {
			return "", "", 0, fmt.Errorf("storing blob to file %q: %w", filename, err)
		}

		return digester.Digest(), diffID.Digest(), counter.Count, nil
	}()
	if err != nil {
		return private.UploadedBlob{}, err
	}

	// Determine blob properties, and fail if information that we were given about the blob
	// is known to be incorrect.
	blobSize := blobinfo.Size
	if blobSize < 0 {
		blobSize = count
	} else if blobinfo.Size != count {
		return private.UploadedBlob{}, ErrBlobSizeMismatch
	}

	// Record information about the blob.
	s.lock.Lock()
	s.lockProtected.blobDiffIDs[blobDigest] = diffID
	s.lockProtected.fileSizes[blobDigest] = count
	s.lockProtected.filenames[blobDigest] = filename
	s.lock.Unlock()
	// This is safe because we have just computed diffID, and blobDigest was either computed
	// by us, or validated by the caller (usually copy.digestingReader).
	options.Cache.RecordDigestUncompressedPair(blobDigest, diffID)
	return private.UploadedBlob{
		Digest: blobDigest,
		Size:   blobSize,
	}, nil
}

type zstdFetcher struct {
	chunkAccessor private.BlobChunkAccessor
	ctx           context.Context
	blobInfo      types.BlobInfo
}

// GetBlobAt converts from chunked.GetBlobAt to BlobChunkAccessor.GetBlobAt.
func (f *zstdFetcher) GetBlobAt(chunks []chunked.ImageSourceChunk) (chan io.ReadCloser, chan error, error) {
	newChunks := make([]private.ImageSourceChunk, 0, len(chunks))
	for _, v := range chunks {
		i := private.ImageSourceChunk{
			Offset: v.Offset,
			Length: v.Length,
		}
		newChunks = append(newChunks, i)
	}
	rc, errs, err := f.chunkAccessor.GetBlobAt(f.ctx, f.blobInfo, newChunks)
	if _, ok := err.(private.BadPartialRequestError); ok {
		err = chunked.ErrBadRequest{}
	}
	return rc, errs, err

}

// PutBlobPartial attempts to create a blob using the data that is already present
// at the destination. chunkAccessor is accessed in a non-sequential way to retrieve the missing chunks.
// It is available only if SupportsPutBlobPartial().
// Even if SupportsPutBlobPartial() returns true, the call can fail.
// If the call fails with ErrFallbackToOrdinaryLayerDownload, the caller can fall back to PutBlobWithOptions.
// The fallback _must not_ be done otherwise.
func (s *storageImageDestination) PutBlobPartial(ctx context.Context, chunkAccessor private.BlobChunkAccessor, srcInfo types.BlobInfo, options private.PutBlobPartialOptions) (_ private.UploadedBlob, retErr error) {
	inputTOCDigest, err := toc.GetTOCDigest(srcInfo.Annotations)
	if err != nil {
		return private.UploadedBlob{}, err
	}

	// The identity of partially-pulled layers is, as long as we keep compatibility with tar-like consumers,
	// unfixably ambiguous: there are two possible “views” of the same file (same compressed digest),
	// the traditional “view” that decompresses the primary stream and consumes a tar file,
	// and the partial-pull “view” that starts with the TOC.
	// The two “views” have two separate metadata sets and may refer to different parts of the blob for file contents;
	// the direct way to ensure they are consistent would be to read the full primary stream (and authenticate it against
	// the compressed digest), and ensure the metadata and layer contents exactly match the partially-pulled contents -
	// making the partial pull completely pointless.
	//
	// Instead, for partial-pull-capable layers (with inputTOCDigest set), we require the image to “commit”
	// to uncompressed layer digest values via the config's RootFS.DiffIDs array:
	// they are already naturally computed for traditionally-pulled layers, and for partially-pulled layers we
	// do the optimal partial pull, and then reconstruct the uncompressed tar stream just to (expensively) compute this digest.
	//
	// Layers which don’t support partial pulls (inputTOCDigest == "", incl. all schema1 layers) can be let through:
	// the partial pull code will either not engage, or consume the full layer; and the rules of indexToTOCDigest / layerIdentifiedByTOC
	// ensure the layer is identified by DiffID, i.e. using the traditional “view”.
	//
	// But if inputTOCDigest is set and the input image doesn't have RootFS.DiffIDs (the config is invalid for schema2/OCI),
	// don't allow a partial pull, and fall back to PutBlobWithOptions.
	//
	// (The user can opt out of the DiffID commitment checking by a c/storage option, giving up security for performance,
	// but we will still trigger the fall back here, and we will still enforce a DiffID match, so that the set of accepted images
	// is the same in both cases, and so that users are not tempted to set the c/storage option to allow accepting some invalid images.)
	var untrustedDiffID digest.Digest // "" if unknown
	udid, err := s.untrustedLayerDiffID(options.LayerIndex)
	if err != nil {
		var diffIDUnknownErr untrustedLayerDiffIDUnknownError
		switch {
		case errors.Is(err, errUntrustedLayerDiffIDNotYetAvailable):
			// PutBlobPartial is a private API, so all callers are within c/image, and should have called
			// NoteOriginalOCIConfig first.
			return private.UploadedBlob{}, fmt.Errorf("internal error: in PutBlobPartial, untrustedLayerDiffID returned errUntrustedLayerDiffIDNotYetAvailable")
		case errors.As(err, &diffIDUnknownErr):
			if inputTOCDigest != nil {
				return private.UploadedBlob{}, private.NewErrFallbackToOrdinaryLayerDownload(err)
			}
			untrustedDiffID = "" // A schema1 image or a non-TOC layer with no ambiguity, let it through
		default:
			return private.UploadedBlob{}, err
		}
	} else {
		untrustedDiffID = udid
	}

	fetcher := zstdFetcher{
		chunkAccessor: chunkAccessor,
		ctx:           ctx,
		blobInfo:      srcInfo,
	}

	defer func() {
		var perr chunked.ErrFallbackToOrdinaryLayerDownload
		if errors.As(retErr, &perr) {
			retErr = private.NewErrFallbackToOrdinaryLayerDownload(retErr)
		}
	}()

	differ, err := chunked.NewDiffer(ctx, s.imageRef.transport.store, srcInfo.Digest, srcInfo.Size, srcInfo.Annotations, &fetcher)
	if err != nil {
		return private.UploadedBlob{}, err
	}
	defer differ.Close()

	out, err := s.imageRef.transport.store.PrepareStagedLayer(nil, differ)
	if err != nil {
		return private.UploadedBlob{}, fmt.Errorf("staging a partially-pulled layer: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = s.imageRef.transport.store.CleanupStagedLayer(out)
		}
	}()

	if out.TOCDigest == "" && out.UncompressedDigest == "" {
		return private.UploadedBlob{}, errors.New("internal error: PrepareStagedLayer succeeded with neither TOCDigest nor UncompressedDigest set")
	}

	blobDigest := srcInfo.Digest

	s.lock.Lock()
	if err := func() error { // A scope for defer
		defer s.lock.Unlock()

		// For true partial pulls, c/storage decides whether to compute the uncompressed digest based on an option in storage.conf
		// (defaults to true, to avoid ambiguity.)
		// c/storage can also be configured, to consume a layer not prepared for partial pulls (primarily to allow composefs conversion),
		// and in that case it always consumes the full blob and always computes the uncompressed digest.
		if out.UncompressedDigest != "" {
			// This is centrally enforced later, in commitLayer, but because we have the value available,
			// we might just as well check immediately.
			if untrustedDiffID != "" && out.UncompressedDigest != untrustedDiffID {
				return fmt.Errorf("uncompressed digest of layer %q is %q, config claims %q", srcInfo.Digest.String(),
					out.UncompressedDigest.String(), untrustedDiffID.String())
			}

			s.lockProtected.indexToDiffID[options.LayerIndex] = out.UncompressedDigest
			if out.TOCDigest != "" {
				s.lockProtected.indexToTOCDigest[options.LayerIndex] = out.TOCDigest
				options.Cache.RecordTOCUncompressedPair(out.TOCDigest, out.UncompressedDigest)
			}

			// If the whole layer has been consumed, chunked.GetDiffer is responsible for ensuring blobDigest has been validated.
			if out.CompressedDigest != "" {
				if out.CompressedDigest != blobDigest {
					return fmt.Errorf("internal error: PrepareStagedLayer returned CompressedDigest %q not matching expected %q",
						out.CompressedDigest, blobDigest)
				}
				// So, record also information about blobDigest, that might benefit reuse.
				// We trust PrepareStagedLayer to validate or create both values correctly.
				s.lockProtected.blobDiffIDs[blobDigest] = out.UncompressedDigest
				options.Cache.RecordDigestUncompressedPair(out.CompressedDigest, out.UncompressedDigest)
			}
		} else {
			// Sanity-check the defined rules for indexToTOCDigest.
			if inputTOCDigest == nil {
				return fmt.Errorf("internal error: PrepareStagedLayer returned a TOC-only identity for layer %q with no TOC digest", srcInfo.Digest.String())
			}

			// Use diffID for layer identity if it is known.
			if uncompressedDigest := options.Cache.UncompressedDigestForTOC(out.TOCDigest); uncompressedDigest != "" {
				s.lockProtected.indexToDiffID[options.LayerIndex] = uncompressedDigest
			}
			s.lockProtected.indexToTOCDigest[options.LayerIndex] = out.TOCDigest
		}
		s.lockProtected.diffOutputs[options.LayerIndex] = out
		return nil
	}(); err != nil {
		return private.UploadedBlob{}, err
	}

	succeeded = true
	return private.UploadedBlob{
			Digest: blobDigest,
			Size:   srcInfo.Size,
		}, s.queueOrCommit(options.LayerIndex, addedLayerInfo{
			digest:     blobDigest,
			emptyLayer: options.EmptyLayer,
		})
}

// TryReusingBlobWithOptions checks whether the transport already contains, or can efficiently reuse, a blob, and if so, applies it to the current destination
// (e.g. if the blob is a filesystem layer, this signifies that the changes it describes need to be applied again when composing a filesystem tree).
// info.Digest must not be empty.
// If the blob has been successfully reused, returns (true, info, nil).
// If the transport can not reuse the requested blob, TryReusingBlob returns (false, {}, nil); it returns a non-nil error only on an unexpected failure.
func (s *storageImageDestination) TryReusingBlobWithOptions(ctx context.Context, blobinfo types.BlobInfo, options private.TryReusingBlobOptions) (bool, private.ReusedBlob, error) {
	if !impl.OriginalCandidateMatchesTryReusingBlobOptions(options) {
		return false, private.ReusedBlob{}, nil
	}
	reused, info, err := s.tryReusingBlobAsPending(blobinfo.Digest, blobinfo.Size, &options)
	if err != nil || !reused || options.LayerIndex == nil {
		return reused, info, err
	}

	return reused, info, s.queueOrCommit(*options.LayerIndex, addedLayerInfo{
		digest:     info.Digest,
		emptyLayer: options.EmptyLayer,
	})
}

// tryReusingBlobAsPending implements TryReusingBlobWithOptions for (blobDigest, size or -1), filling s.blobDiffIDs and other metadata.
// The caller must arrange the blob to be eventually committed using s.commitLayer().
func (s *storageImageDestination) tryReusingBlobAsPending(blobDigest digest.Digest, size int64, options *private.TryReusingBlobOptions) (bool, private.ReusedBlob, error) {
	if blobDigest == "" {
		return false, private.ReusedBlob{}, errors.New(`Can not check for a blob with unknown digest`)
	}
	if err := blobDigest.Validate(); err != nil {
		return false, private.ReusedBlob{}, fmt.Errorf("Can not check for a blob with invalid digest: %w", err)
	}
	useTOCDigest := false // If set, (options.TOCDigest != "" && options.LayerIndex != nil) AND we can use options.TOCDigest safely.
	if options.TOCDigest != "" && options.LayerIndex != nil {
		if err := options.TOCDigest.Validate(); err != nil {
			return false, private.ReusedBlob{}, fmt.Errorf("Can not check for a blob with invalid digest: %w", err)
		}
		// Only consider using TOCDigest if we can avoid ambiguous image “views”, see the detailed comment in PutBlobPartial.
		_, err := s.untrustedLayerDiffID(*options.LayerIndex)
		if err != nil {
			var diffIDUnknownErr untrustedLayerDiffIDUnknownError
			switch {
			case errors.Is(err, errUntrustedLayerDiffIDNotYetAvailable):
				// options.TOCDigest is a private API, so all callers are within c/image, and should have called
				// NoteOriginalOCIConfig first.
				return false, private.ReusedBlob{}, fmt.Errorf("internal error: in TryReusingBlobWithOptions, untrustedLayerDiffID returned errUntrustedLayerDiffIDNotYetAvailable")
			case errors.As(err, &diffIDUnknownErr):
				logrus.Debugf("Not using TOC %q to look for layer reuse: %v", options.TOCDigest, err)
				// But don’t abort entirely, keep useTOCDigest = false, try a blobDigest match.
			default:
				return false, private.ReusedBlob{}, err
			}
		} else {
			useTOCDigest = true
		}
	}

	// lock the entire method as it executes fairly quickly
	s.lock.Lock()
	defer s.lock.Unlock()

	if options.SrcRef != nil && useTOCDigest {
		// Check if we have the layer in the underlying additional layer store.
		aLayer, err := s.imageRef.transport.store.LookupAdditionalLayer(options.TOCDigest, options.SrcRef.String())
		if err != nil && !errors.Is(err, storage.ErrLayerUnknown) {
			return false, private.ReusedBlob{}, fmt.Errorf(`looking for compressed layers with digest %q and labels: %w`, blobDigest, err)
		} else if err == nil {
			// Compare the long comment in PutBlobPartial. We assume that the Additional Layer Store will, somehow,
			// avoid layer “view” ambiguity.
			alsTOCDigest := aLayer.TOCDigest()
			if alsTOCDigest != options.TOCDigest {
				// FIXME: If alsTOCDigest is "", the Additional Layer Store FUSE server is probably just too old, and we could
				// probably go on reading the layer from other sources.
				//
				// Currently it should not be possible for alsTOCDigest to be set and not the expected value, but there’s
				// not that much benefit to checking for equality — we trust the FUSE server to validate the digest either way.
				return false, private.ReusedBlob{}, fmt.Errorf("additional layer for TOCDigest %q reports unexpected TOCDigest %q",
					options.TOCDigest, alsTOCDigest)
			}
			s.lockProtected.indexToTOCDigest[*options.LayerIndex] = options.TOCDigest
			s.lockProtected.indexToAdditionalLayer[*options.LayerIndex] = aLayer
			return true, private.ReusedBlob{
				Digest: blobDigest,
				Size:   aLayer.CompressedSize(),
			}, nil
		}
	}

	// Check if we have a wasn't-compressed layer in storage that's based on that blob.

	// Check if we've already cached it in a file.
	if size, ok := s.lockProtected.fileSizes[blobDigest]; ok {
		// s.lockProtected.blobDiffIDs is set either by putBlobToPendingFile or in createNewLayer when creating the
		// filenames/fileSizes entry.
		return true, private.ReusedBlob{
			Digest: blobDigest,
			Size:   size,
		}, nil
	}

	layers, err := s.imageRef.transport.store.LayersByUncompressedDigest(blobDigest)
	if err != nil && !errors.Is(err, storage.ErrLayerUnknown) {
		return false, private.ReusedBlob{}, fmt.Errorf(`looking for layers with digest %q: %w`, blobDigest, err)
	}
	if len(layers) > 0 {
		s.lockProtected.blobDiffIDs[blobDigest] = blobDigest
		return true, private.ReusedBlob{
			Digest: blobDigest,
			Size:   layers[0].UncompressedSize,
		}, nil
	}

	// Check if we have a was-compressed layer in storage that's based on that blob.
	layers, err = s.imageRef.transport.store.LayersByCompressedDigest(blobDigest)
	if err != nil && !errors.Is(err, storage.ErrLayerUnknown) {
		return false, private.ReusedBlob{}, fmt.Errorf(`looking for compressed layers with digest %q: %w`, blobDigest, err)
	}
	if len(layers) > 0 {
		// LayersByCompressedDigest only finds layers which were created from a full layer blob, and extracting that
		// always sets UncompressedDigest.
		diffID := layers[0].UncompressedDigest
		if diffID == "" {
			return false, private.ReusedBlob{}, fmt.Errorf("internal error: compressed layer %q (for compressed digest %q) does not have an uncompressed digest", layers[0].ID, blobDigest.String())
		}
		s.lockProtected.blobDiffIDs[blobDigest] = diffID
		return true, private.ReusedBlob{
			Digest: blobDigest,
			Size:   layers[0].CompressedSize,
		}, nil
	}

	// Does the blob correspond to a known DiffID which we already have available?
	// Because we must return the size, which is unknown for unavailable compressed blobs, the returned BlobInfo refers to the
	// uncompressed layer, and that can happen only if options.CanSubstitute, or if the incoming manifest already specifies the size.
	if options.CanSubstitute || size != -1 {
		if uncompressedDigest := options.Cache.UncompressedDigest(blobDigest); uncompressedDigest != "" && uncompressedDigest != blobDigest {
			layers, err := s.imageRef.transport.store.LayersByUncompressedDigest(uncompressedDigest)
			if err != nil && !errors.Is(err, storage.ErrLayerUnknown) {
				return false, private.ReusedBlob{}, fmt.Errorf(`looking for layers with digest %q: %w`, uncompressedDigest, err)
			}
			if found, reused := reusedBlobFromLayerLookup(layers, blobDigest, size, options); found {
				s.lockProtected.blobDiffIDs[reused.Digest] = uncompressedDigest
				return true, reused, nil
			}
		}
	}

	if useTOCDigest {
		// Check if we know which which UncompressedDigest the TOC digest resolves to, and we have a match for that.
		// Prefer this over LayersByTOCDigest because we can identify the layer using UncompressedDigest, maximizing reuse.
		uncompressedDigest := options.Cache.UncompressedDigestForTOC(options.TOCDigest)
		if uncompressedDigest != "" {
			layers, err = s.imageRef.transport.store.LayersByUncompressedDigest(uncompressedDigest)
			if err != nil && !errors.Is(err, storage.ErrLayerUnknown) {
				return false, private.ReusedBlob{}, fmt.Errorf(`looking for layers with digest %q: %w`, uncompressedDigest, err)
			}
			if found, reused := reusedBlobFromLayerLookup(layers, blobDigest, size, options); found {
				s.lockProtected.indexToDiffID[*options.LayerIndex] = uncompressedDigest
				reused.MatchedByTOCDigest = true
				return true, reused, nil
			}
		}
		// Check if we have a chunked layer in storage with the same TOC digest.
		layers, err := s.imageRef.transport.store.LayersByTOCDigest(options.TOCDigest)
		if err != nil && !errors.Is(err, storage.ErrLayerUnknown) {
			return false, private.ReusedBlob{}, fmt.Errorf(`looking for layers with TOC digest %q: %w`, options.TOCDigest, err)
		}
		if found, reused := reusedBlobFromLayerLookup(layers, blobDigest, size, options); found {
			if uncompressedDigest == "" && layers[0].UncompressedDigest != "" {
				// Determine an uncompressed digest if at all possible, to use a traditional image ID
				// and to maximize image reuse.
				uncompressedDigest = layers[0].UncompressedDigest
			}
			if uncompressedDigest != "" {
				s.lockProtected.indexToDiffID[*options.LayerIndex] = uncompressedDigest
			}
			s.lockProtected.indexToTOCDigest[*options.LayerIndex] = options.TOCDigest
			reused.MatchedByTOCDigest = true
			return true, reused, nil
		}
	}

	// Nope, we don't have it.
	return false, private.ReusedBlob{}, nil
}

// reusedBlobFromLayerLookup returns (true, ReusedBlob) if layers contain a usable match; or (false, ...) if not.
// The caller is still responsible for setting the layer identification fields, to allow the layer to be found again.
func reusedBlobFromLayerLookup(layers []storage.Layer, blobDigest digest.Digest, blobSize int64, options *private.TryReusingBlobOptions) (bool, private.ReusedBlob) {
	if len(layers) > 0 {
		if blobSize != -1 {
			return true, private.ReusedBlob{
				Digest: blobDigest,
				Size:   blobSize,
			}
		} else if options.CanSubstitute && layers[0].UncompressedDigest != "" {
			return true, private.ReusedBlob{
				Digest:               layers[0].UncompressedDigest,
				Size:                 layers[0].UncompressedSize,
				CompressionOperation: types.Decompress,
				CompressionAlgorithm: nil,
			}
		}
	}
	return false, private.ReusedBlob{}
}

// trustedLayerIdentityData is a _consistent_ set of information known about a single layer.
type trustedLayerIdentityData struct {
	// true if we decided the layer should be identified by tocDigest, false if by diffID
	// This can only be true if c/storage/pkg/chunked/toc.GetTOCDigest returns a value.
	layerIdentifiedByTOC bool

	diffID     digest.Digest // A digest of the uncompressed full contents of the layer, or "" if unknown; must be set if !layerIdentifiedByTOC
	tocDigest  digest.Digest // A digest of the TOC digest, or "" if unknown; must be set if layerIdentifiedByTOC
	blobDigest digest.Digest // A digest of the (possibly-compressed) layer as presented, or "" if unknown/untrusted.
}

// logString() prints a representation of trusted suitable identifying a layer in logs and errors.
// The string is already quoted to expose malicious input and does not need to be quoted again.
// Note that it does not include _all_ of the contents.
func (trusted trustedLayerIdentityData) logString() string {
	return fmt.Sprintf("%q/%q/%q", trusted.blobDigest, trusted.tocDigest, trusted.diffID)
}

// trustedLayerIdentityDataLocked returns a _consistent_ set of information for a layer with (layerIndex, blobDigest).
// blobDigest is the (possibly-compressed) layer digest referenced in the manifest.
// It returns (trusted, true) if the layer was found, or (_, false) if insufficient data is available.
//
// The caller must hold s.lock.
func (s *storageImageDestination) trustedLayerIdentityDataLocked(layerIndex int, blobDigest digest.Digest) (trustedLayerIdentityData, bool) {
	// The decision about layerIdentifiedByTOC must be _stable_ once the data for layerIndex is set,
	// even if s.lockProtected.blobDiffIDs changes later and we can subsequently find an entry that wasn’t originally available.
	//
	// If we previously didn't have a blobDigest match and decided to use the TOC, but _later_ we happen to find
	// a blobDigest match, we might in principle want to reconsider, set layerIdentifiedByTOC to false, and use the file:
	// but the layer in question, and possibly child layers, might already have been committed to storage.
	// A late-arriving addition to s.lockProtected.blobDiffIDs would mean that we would want to set
	// new layer IDs for potentially the whole parent chain = throw away the just-created layers and create them all again.
	//
	// Such a within-image layer reuse is expected to be pretty rare; instead, ignore the unexpected file match
	// and proceed to the originally-planned TOC match.

	res := trustedLayerIdentityData{}
	diffID, layerIdentifiedByDiffID := s.lockProtected.indexToDiffID[layerIndex]
	if layerIdentifiedByDiffID {
		res.layerIdentifiedByTOC = false
		res.diffID = diffID
	}
	if tocDigest, ok := s.lockProtected.indexToTOCDigest[layerIndex]; ok {
		res.tocDigest = tocDigest
		if !layerIdentifiedByDiffID {
			res.layerIdentifiedByTOC = true
		}
	}
	if otherDiffID, ok := s.lockProtected.blobDiffIDs[blobDigest]; ok {
		if !layerIdentifiedByDiffID && !res.layerIdentifiedByTOC {
			// This is the only data we have, so it is clearly self-consistent.
			res.layerIdentifiedByTOC = false
			res.diffID = otherDiffID
			res.blobDigest = blobDigest
			layerIdentifiedByDiffID = true
		} else {
			// We have set up the layer identity without referring to blobDigest:
			// an attacker might have used a manifest with non-matching tocDigest and blobDigest.
			// But, if we know a trusted diffID value from other sources, and it matches the one for blobDigest,
			// we know blobDigest is fine as well.
			if res.diffID != "" && otherDiffID == res.diffID {
				res.blobDigest = blobDigest
			}
		}
	}
	if !layerIdentifiedByDiffID && !res.layerIdentifiedByTOC {
		return trustedLayerIdentityData{}, false // We found nothing at all
	}
	return res, true
}

// computeID computes a recommended image ID based on information we have so far.  If
// the manifest is not of a type that we recognize, we return an empty value, indicating
// that since we don't have a recommendation, a random ID should be used if one needs
// to be allocated.
func (s *storageImageDestination) computeID(m manifest.Manifest) (string, error) {
	// This is outside of the scope of HasThreadSafePutBlob, so we don’t need to hold s.lock.

	layerInfos := m.LayerInfos()

	// Build the diffID list.  We need the decompressed sums that we've been calculating to
	// fill in the DiffIDs.  It's expected (but not enforced by us) that the number of
	// diffIDs corresponds to the number of non-EmptyLayer entries in the history.
	var diffIDs []digest.Digest
	switch m.(type) {
	case *manifest.Schema1:
		// Build a list of the diffIDs we've generated for the non-throwaway FS layers
		for i, li := range layerInfos {
			if li.EmptyLayer {
				continue
			}
			trusted, ok := s.trustedLayerIdentityDataLocked(i, li.Digest)
			if !ok { // We have already committed all layers if we get to this point, so the data must have been available.
				return "", fmt.Errorf("internal inconsistency: layer (%d, %q) not found", i, li.Digest)
			}
			if trusted.diffID == "" {
				if trusted.layerIdentifiedByTOC {
					logrus.Infof("v2s1 image uses a layer identified by TOC with unknown diffID; choosing a random image ID")
					return "", nil
				}
				return "", fmt.Errorf("internal inconsistency: layer (%d, %q) is not identified by TOC and has no diffID", i, li.Digest)
			}
			diffIDs = append(diffIDs, trusted.diffID)
		}
	case *manifest.Schema2, *manifest.OCI1:
		// We know the ID calculation doesn't actually use the diffIDs, so we don't need to populate
		// the diffID list.
	default:
		return "", nil
	}

	// We want to use the same ID for “the same” images, but without risking unwanted sharing / malicious image corruption.
	//
	// Traditionally that means the same ~config digest, as computed by m.ImageID;
	// but if we identify a layer by TOC, we verify the layer against neither the (compressed) blob digest in the manifest,
	// nor against the config’s RootFS.DiffIDs. We don’t really want to do either, to allow partial layer pulls where we never see
	// most of the data.
	//
	// So, if a layer is identified by TOC (and we do validate against the TOC), the fact that we used the TOC, and the value of the TOC,
	// must enter into the image ID computation.
	// But for images where no TOC was used, continue to use IDs computed the traditional way, to maximize image reuse on upgrades,
	// and to introduce the changed behavior only when partial pulls are used.
	//
	// Note that it’s not 100% guaranteed that an image pulled by TOC uses an OCI manifest; consider
	// (skopeo copy --format v2s2 docker://…/zstd-chunked-image containers-storage:… ). So this is not happening only in the OCI case above.
	ordinaryImageID, err := m.ImageID(diffIDs)
	if err != nil {
		return "", err
	}
	tocIDInput := ""
	hasLayerPulledByTOC := false
	for i, li := range layerInfos {
		trusted, ok := s.trustedLayerIdentityDataLocked(i, li.Digest)
		if !ok { // We have already committed all layers if we get to this point, so the data must have been available.
			return "", fmt.Errorf("internal inconsistency: layer (%d, %q) not found", i, li.Digest)
		}
		layerValue := "" // An empty string is not a valid digest, so this is unambiguous with the TOC case.
		if trusted.layerIdentifiedByTOC {
			hasLayerPulledByTOC = true
			layerValue = trusted.tocDigest.String()
		}
		tocIDInput += layerValue + "|" // "|" can not be present in a TOC digest, so this is an unambiguous separator.
	}

	if !hasLayerPulledByTOC {
		return ordinaryImageID, nil
	}
	// ordinaryImageID is a digest of a config, which is a JSON value.
	// To avoid the risk of collisions, start the input with @ so that the input is not a valid JSON.
	tocImageID := digest.FromString("@With TOC:" + tocIDInput).Encoded()
	logrus.Debugf("Ordinary storage image ID %s; a layer was looked up by TOC, so using image ID %s", ordinaryImageID, tocImageID)
	return tocImageID, nil
}

// getConfigBlob exists only to let us retrieve the configuration blob so that the manifest package can dig
// information out of it for Inspect().
func (s *storageImageDestination) getConfigBlob(info types.BlobInfo) ([]byte, error) {
	if info.Digest == "" {
		return nil, errors.New(`no digest supplied when reading blob`)
	}
	if err := info.Digest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid digest supplied when reading blob: %w", err)
	}
	// Assume it's a file, since we're only calling this from a place that expects to read files.
	if filename, ok := s.lockProtected.filenames[info.Digest]; ok {
		contents, err2 := os.ReadFile(filename)
		if err2 != nil {
			return nil, fmt.Errorf(`reading blob from file %q: %w`, filename, err2)
		}
		return contents, nil
	}
	// If it's not a file, it's a bug, because we're not expecting to be asked for a layer.
	return nil, errors.New("blob not found")
}

// queueOrCommit queues the specified layer to be committed to the storage.
// If no other goroutine is already committing layers, the layer and all
// subsequent layers (if already queued) will be committed to the storage.
func (s *storageImageDestination) queueOrCommit(index int, info addedLayerInfo) error {
	// NOTE: whenever the code below is touched, make sure that all code
	// paths unlock the lock and to unlock it exactly once.
	//
	// Conceptually, the code is divided in two stages:
	//
	// 1) Queue in work by marking the layer as ready to be committed.
	//    If at least one previous/parent layer with a lower index has
	//    not yet been committed, return early.
	//
	// 2) Process the queued-in work by committing the "ready" layers
	//    in sequence.  Make sure that more items can be queued-in
	//    during the comparatively I/O expensive task of committing a
	//    layer.
	//
	// The conceptual benefit of this design is that caller can continue
	// pulling layers after an early return.  At any given time, only one
	// caller is the "worker" routine committing layers.  All other routines
	// can continue pulling and queuing in layers.
	s.lock.Lock()
	s.lockProtected.indexToAddedLayerInfo[index] = info

	// We're still waiting for at least one previous/parent layer to be
	// committed, so there's nothing to do.
	if index != s.lockProtected.currentIndex {
		s.lock.Unlock()
		return nil
	}

	for {
		info, ok := s.lockProtected.indexToAddedLayerInfo[index]
		if !ok {
			break
		}
		s.lock.Unlock()
		// Note: commitLayer locks on-demand.
		if stopQueue, err := s.commitLayer(index, info, -1); stopQueue || err != nil {
			return err
		}
		s.lock.Lock()
		index++
	}

	// Set the index at the very end to make sure that only one routine
	// enters stage 2).
	s.lockProtected.currentIndex = index
	s.lock.Unlock()
	return nil
}

// commitLayer commits the specified layer with the given index to the storage.
// size can usually be -1; it can be provided if the layer is not known to be already present in blobDiffIDs.
//
// If the layer cannot be committed yet, the function returns (true, nil).
//
// Note that the previous layer is expected to already be committed.
//
// Caution: this function must be called without holding `s.lock`.  Callers
// must guarantee that, at any given time, at most one goroutine may execute
// `commitLayer()`.
func (s *storageImageDestination) commitLayer(index int, info addedLayerInfo, size int64) (bool, error) {
	if _, alreadyCommitted := s.indexToStorageID[index]; alreadyCommitted {
		return false, nil
	}

	var parentLayer string // "" if no parent
	if index != 0 {
		// s.indexToStorageID can only be written by this function, and our caller
		// is responsible for ensuring it can be only be called by *one* goroutine at any
		// given time. Hence, we don't need to lock accesses.
		prev, ok := s.indexToStorageID[index-1]
		if !ok {
			return false, fmt.Errorf("Internal error: commitLayer called with previous layer %d not committed yet", index-1)
		}
		parentLayer = prev
	}

	if info.emptyLayer {
		s.indexToStorageID[index] = parentLayer
		return false, nil
	}

	// Collect trusted parameters of the layer.
	s.lock.Lock()
	trusted, ok := s.trustedLayerIdentityDataLocked(index, info.digest)
	s.lock.Unlock()
	if !ok {
		// Check if the layer exists already and the caller just (incorrectly) forgot to pass it to us in a PutBlob() / TryReusingBlob() / …
		//
		// Use none.NoCache to avoid a repeated DiffID lookup in the BlobInfoCache: a caller
		// that relies on using a blob digest that has never been seen by the store had better call
		// TryReusingBlob; not calling PutBlob already violates the documented API, so there’s only
		// so far we are going to accommodate that (if we should be doing that at all).
		//
		// We are also ignoring lookups by TOC, and other non-trivial situations.
		// Those can only happen using the c/image/internal/private API,
		// so those internal callers should be fixed to follow the API instead of expanding this fallback.
		logrus.Debugf("looking for diffID for blob=%+v", info.digest)

		// Use tryReusingBlobAsPending, not the top-level TryReusingBlobWithOptions, to prevent recursion via queueOrCommit.
		has, _, err := s.tryReusingBlobAsPending(info.digest, size, &private.TryReusingBlobOptions{
			Cache:         none.NoCache,
			CanSubstitute: false,
		})
		if err != nil {
			return false, fmt.Errorf("checking for a layer based on blob %q: %w", info.digest.String(), err)
		}
		if !has {
			return false, fmt.Errorf("error determining uncompressed digest for blob %q", info.digest.String())
		}

		s.lock.Lock()
		trusted, ok = s.trustedLayerIdentityDataLocked(index, info.digest)
		s.lock.Unlock()
		if !ok {
			return false, fmt.Errorf("we have blob %q, but don't know its layer ID", info.digest.String())
		}
	}

	// Ensure that we always see the same “view” of a layer, as identified by the layer’s uncompressed digest,
	// unless the user has explicitly opted out of this in storage.conf: see the more detailed explanation in PutBlobPartial.
	if trusted.diffID != "" {
		untrustedDiffID, err := s.untrustedLayerDiffID(index)
		if err != nil {
			var diffIDUnknownErr untrustedLayerDiffIDUnknownError
			switch {
			case errors.Is(err, errUntrustedLayerDiffIDNotYetAvailable):
				logrus.Debugf("Skipping commit for layer %q, manifest not yet available for DiffID check", index)
				return true, nil
			case errors.As(err, &diffIDUnknownErr):
				// If untrustedLayerDiffIDUnknownError, the input image is schema1, has no TOC annotations,
				// so we could not have reused a TOC-identified layer nor have done a TOC-identified partial pull,
				// i.e. there is no other “view” to worry about.  Sanity-check that we really see the only expected view.
				//
				// Or, maybe, the input image is OCI, and has invalid/missing DiffID values in config. In that case
				// we _must_ fail if we used a TOC-identified layer - but PutBlobPartial should have already
				// refused to do a partial pull, so we are in an inconsistent state.
				if trusted.layerIdentifiedByTOC {
					return false, fmt.Errorf("internal error: layer %d for blob %s was identified by TOC, but we don't have a DiffID in config",
						index, trusted.logString())
				}
				// else a schema1 image or a non-TOC layer with no ambiguity, let it through
			default:
				return false, err
			}
		} else if trusted.diffID != untrustedDiffID {
			return false, fmt.Errorf("layer %d (blob %s) does not match config's DiffID %q", index, trusted.logString(), untrustedDiffID)
		}
	}

	id := layerID(parentLayer, trusted)

	if layer, err2 := s.imageRef.transport.store.Layer(id); layer != nil && err2 == nil {
		// There's already a layer that should have the right contents, just reuse it.
		s.indexToStorageID[index] = layer.ID
		return false, nil
	}

	layer, err := s.createNewLayer(index, trusted, parentLayer, id)
	if err != nil {
		return false, err
	}
	if layer == nil {
		return true, nil
	}
	s.indexToStorageID[index] = layer.ID
	return false, nil
}

// layerID computes a layer (“chain”) ID for (a possibly-empty parentID, trusted)
func layerID(parentID string, trusted trustedLayerIdentityData) string {
	var component string
	mustHash := false
	if trusted.layerIdentifiedByTOC {
		// "@" is not a valid start of a digest.Digest.Encoded(), so this is unambiguous with the !layerIdentifiedByTOC case.
		// But we _must_ hash this below to get a Digest.Encoded()-formatted value.
		component = "@TOC=" + trusted.tocDigest.Encoded()
		mustHash = true
	} else {
		component = trusted.diffID.Encoded() // This looks like chain IDs, and it uses the traditional value.
	}

	if parentID == "" && !mustHash {
		return component
	}
	return digest.Canonical.FromString(parentID + "+" + component).Encoded()
}

// createNewLayer creates a new layer newLayerID for (index, trusted) on top of parentLayer (which may be "").
// If the layer cannot be committed yet, the function returns (nil, nil).
func (s *storageImageDestination) createNewLayer(index int, trusted trustedLayerIdentityData, parentLayer, newLayerID string) (*storage.Layer, error) {
	s.lock.Lock()
	diffOutput, ok := s.lockProtected.diffOutputs[index]
	s.lock.Unlock()
	if ok {
		// Typically, we compute a trusted DiffID value to authenticate the layer contents, see the detailed explanation
		// in PutBlobPartial.  If the user has opted out of that, but we know a trusted DiffID value
		// (e.g. from a BlobInfoCache), set it in diffOutput.
		// That way it will be persisted in storage even if the cache is deleted; also
		// we can use the value below to avoid the untrustedUncompressedDigest logic.
		if diffOutput.UncompressedDigest == "" && trusted.diffID != "" {
			diffOutput.UncompressedDigest = trusted.diffID
		}

		var untrustedUncompressedDigest digest.Digest
		if diffOutput.UncompressedDigest == "" {
			d, err := s.untrustedLayerDiffID(index)
			if err != nil {
				var diffIDUnknownErr untrustedLayerDiffIDUnknownError
				switch {
				case errors.Is(err, errUntrustedLayerDiffIDNotYetAvailable):
					logrus.Debugf("Skipping commit for layer %q, manifest not yet available", newLayerID)
					return nil, nil
				case errors.As(err, &diffIDUnknownErr):
					// If untrustedLayerDiffIDUnknownError, the input image is schema1, has no TOC annotations,
					// so we should have !trusted.layerIdentifiedByTOC, i.e. we should have set
					// diffOutput.UncompressedDigest above in this function, at the very latest.
					//
					// Or, maybe, the input image is OCI, and has invalid/missing DiffID values in config. In that case
					// commitLayer should have already refused this image when dealing with the “view” ambiguity.
					return nil, fmt.Errorf("internal error: layer %d for blob %s was partially-pulled with unknown UncompressedDigest, but we don't have a DiffID in config",
						index, trusted.logString())
				default:
					return nil, err
				}
			}

			untrustedUncompressedDigest = d
			// While the contents of the digest are untrusted, make sure at least the _format_ is valid,
			// because we are going to write it to durable storage in expectedLayerDiffIDFlag .
			if err := untrustedUncompressedDigest.Validate(); err != nil {
				return nil, err
			}
		}

		flags := make(map[string]any)
		if untrustedUncompressedDigest != "" {
			flags[expectedLayerDiffIDFlag] = untrustedUncompressedDigest.String()
			logrus.Debugf("Setting uncompressed digest to %q for layer %q", untrustedUncompressedDigest, newLayerID)
		}

		args := storage.ApplyStagedLayerOptions{
			ID:          newLayerID,
			ParentLayer: parentLayer,

			DiffOutput: diffOutput,
			DiffOptions: &graphdriver.ApplyDiffWithDifferOpts{
				Flags: flags,
			},
		}
		layer, err := s.imageRef.transport.store.ApplyStagedLayer(args)
		if err != nil && !errors.Is(err, storage.ErrDuplicateID) {
			return nil, fmt.Errorf("failed to put layer using a partial pull: %w", err)
		}
		return layer, nil
	}

	s.lock.Lock()
	al, ok := s.lockProtected.indexToAdditionalLayer[index]
	s.lock.Unlock()
	if ok {
		layer, err := al.PutAs(newLayerID, parentLayer, nil)
		if err != nil && !errors.Is(err, storage.ErrDuplicateID) {
			return nil, fmt.Errorf("failed to put layer from digest and labels: %w", err)
		}
		return layer, nil
	}

	// Check if we previously cached a file with that blob's contents.  If we didn't,
	// then we need to read the desired contents from a layer.
	var filename string
	var gotFilename bool
	if trusted.blobDigest != "" {
		s.lock.Lock()
		filename, gotFilename = s.lockProtected.filenames[trusted.blobDigest]
		s.lock.Unlock()
	}
	var trustedOriginalDigest digest.Digest // For storage.LayerOptions
	var trustedOriginalSize *int64
	if gotFilename {
		// The code setting .filenames[trusted.blobDigest] is responsible for ensuring that the file contents match trusted.blobDigest.
		trustedOriginalDigest = trusted.blobDigest
		trustedOriginalSize = nil // It’s s.lockProtected.fileSizes[trusted.blobDigest], but we don’t hold the lock now, and the consumer can compute it at trivial cost.
	} else {
		// Try to find the layer with contents matching the data we use.
		var layer *storage.Layer // = nil
		if trusted.diffID != "" {
			if layers, err2 := s.imageRef.transport.store.LayersByUncompressedDigest(trusted.diffID); err2 == nil && len(layers) > 0 {
				layer = &layers[0]
			}
		}
		if layer == nil && trusted.tocDigest != "" {
			if layers, err2 := s.imageRef.transport.store.LayersByTOCDigest(trusted.tocDigest); err2 == nil && len(layers) > 0 {
				layer = &layers[0]
			}
		}
		if layer == nil && trusted.blobDigest != "" {
			if layers, err2 := s.imageRef.transport.store.LayersByCompressedDigest(trusted.blobDigest); err2 == nil && len(layers) > 0 {
				layer = &layers[0]
			}
		}
		if layer == nil {
			return nil, fmt.Errorf("layer for blob %s not found", trusted.logString())
		}

		// Read the layer's contents.
		noCompression := archive.Uncompressed
		diffOptions := &storage.DiffOptions{
			Compression: &noCompression,
		}
		diff, err2 := s.imageRef.transport.store.Diff("", layer.ID, diffOptions)
		if err2 != nil {
			return nil, fmt.Errorf("reading layer %q for blob %s: %w", layer.ID, trusted.logString(), err2)
		}
		// Copy the layer diff to a file.  Diff() takes a lock that it holds
		// until the ReadCloser that it returns is closed, and PutLayer() wants
		// the same lock, so the diff can't just be directly streamed from one
		// to the other.
		filename = s.computeNextBlobCacheFile()
		file, err := os.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			diff.Close()
			return nil, fmt.Errorf("creating temporary file %q: %w", filename, err)
		}
		// Copy the data to the file.
		// TODO: This can take quite some time, and should ideally be cancellable using
		// ctx.Done().
		fileSize, err := io.Copy(file, diff)
		diff.Close()
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("storing blob to file %q: %w", filename, err)
		}

		if trusted.diffID == "" && layer.UncompressedDigest != "" {
			trusted.diffID = layer.UncompressedDigest // This data might have been unavailable in tryReusingBlobAsPending, and is only known now.
		}

		// Set the layer’s CompressedDigest/CompressedSize to relevant values if known, to allow more layer reuse.
		// But we don’t want to just use the size from the manifest if we never saw the compressed blob,
		// so that we don’t propagate mistakes / attacks.
		//
		// s.lockProtected.fileSizes[trusted.blobDigest] is not set, otherwise we would have found gotFilename.
		// So, check if the layer we found contains that metadata. (If that layer continues to exist, there’s no benefit
		// to us propagating the metadata; but that layer could be removed, and in that case propagating the metadata to
		// this new layer copy can help.)
		if trusted.blobDigest != "" && layer.CompressedDigest == trusted.blobDigest && layer.CompressedSize > 0 {
			trustedOriginalDigest = trusted.blobDigest
			sizeCopy := layer.CompressedSize
			trustedOriginalSize = &sizeCopy
		} else {
			// The stream we have is uncompressed, and it matches trusted.diffID (if known).
			//
			// We can legitimately set storage.LayerOptions.OriginalDigest to "",
			// but that would just result in PutLayer computing the digest of the input stream == trusted.diffID.
			// So, instead, set .OriginalDigest to the value we know already, to avoid that digest computation.
			trustedOriginalDigest = trusted.diffID
			trustedOriginalSize = nil // Probably layer.UncompressedSize, but the consumer can compute it at trivial cost.
		}

		// Allow using the already-collected layer contents without extracting the layer again.
		//
		// This only matches against the uncompressed digest.
		// If we have trustedOriginalDigest == trusted.blobDigest, we could arrange to reuse the
		// same uncompressed stream for future calls of createNewLayer; but for the non-layer blobs (primarily the config),
		// we assume that the file at filenames[someDigest] matches someDigest _exactly_; we would need to differentiate
		// between “original files” and “possibly uncompressed files”.
		// Within-image layer reuse is probably very rare, for now we prefer to avoid that complexity.
		if trusted.diffID != "" {
			s.lock.Lock()
			s.lockProtected.blobDiffIDs[trusted.diffID] = trusted.diffID
			s.lockProtected.filenames[trusted.diffID] = filename
			s.lockProtected.fileSizes[trusted.diffID] = fileSize
			s.lock.Unlock()
		}
	}
	// Read the cached blob and use it as a diff.
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("opening file %q: %w", filename, err)
	}
	defer file.Close()
	// Build the new layer using the diff, regardless of where it came from.
	// TODO: This can take quite some time, and should ideally be cancellable using ctx.Done().
	layer, _, err := s.imageRef.transport.store.PutLayer(newLayerID, parentLayer, nil, "", false, &storage.LayerOptions{
		OriginalDigest: trustedOriginalDigest,
		OriginalSize:   trustedOriginalSize, // nil in many cases
		// This might be "" if trusted.layerIdentifiedByTOC; in that case PutLayer will compute the value from the stream.
		UncompressedDigest: trusted.diffID,
	}, file)
	if err != nil && !errors.Is(err, storage.ErrDuplicateID) {
		return nil, fmt.Errorf("adding layer with blob %s: %w", trusted.logString(), err)
	}
	return layer, nil
}

// uncommittedImageSource allows accessing an image’s metadata (not layers) before it has been committed,
// to allow using image.FromUnparsedImage.
type uncommittedImageSource struct {
	srcImpl.Compat
	srcImpl.PropertyMethodsInitialize
	srcImpl.NoSignatures
	srcImpl.DoesNotAffectLayerInfosForCopy
	srcStubs.NoGetBlobAtInitialize

	d *storageImageDestination
}

func newUncommittedImageSource(d *storageImageDestination) *uncommittedImageSource {
	s := &uncommittedImageSource{
		PropertyMethodsInitialize: srcImpl.PropertyMethods(srcImpl.Properties{
			HasThreadSafeGetBlob: true,
		}),
		NoGetBlobAtInitialize: srcStubs.NoGetBlobAt(d.Reference()),

		d: d,
	}
	s.Compat = srcImpl.AddCompat(s)
	return s
}

func (u *uncommittedImageSource) Reference() types.ImageReference {
	return u.d.Reference()
}

func (u *uncommittedImageSource) Close() error {
	return nil
}

func (u *uncommittedImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	return u.d.manifest, u.d.manifestMIMEType, nil
}

func (u *uncommittedImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	blob, err := u.d.getConfigBlob(info)
	if err != nil {
		return nil, -1, err
	}
	return io.NopCloser(bytes.NewReader(blob)), int64(len(blob)), nil
}

// errUntrustedLayerDiffIDNotYetAvailable is returned by untrustedLayerDiffID
// if the value is not yet available (but it can be available after s.manifests is set).
// This should only happen for external callers of the transport, not for c/image/copy.
//
// Callers of untrustedLayerDiffID before PutManifest must handle this error specially;
// callers after PutManifest can use the default, reporting an internal error.
var errUntrustedLayerDiffIDNotYetAvailable = errors.New("internal error: untrustedLayerDiffID has no value available and fallback was not implemented")

// untrustedLayerDiffIDUnknownError is returned by untrustedLayerDiffID
// if the image’s format does not provide DiffIDs.
type untrustedLayerDiffIDUnknownError struct {
	layerIndex int
}

func (e untrustedLayerDiffIDUnknownError) Error() string {
	return fmt.Sprintf("DiffID value for layer %d is unknown or explicitly empty", e.layerIndex)
}

// untrustedLayerDiffID returns a DiffID value for layerIndex from the image’s config.
// It may return two special errors, errUntrustedLayerDiffIDNotYetAvailable or untrustedLayerDiffIDUnknownError.
//
// WARNING: This function does not even validate that the returned digest has a valid format.
// WARNING: We don’t _always_ validate this DiffID value against the layer contents; it must not be used for any deduplication.
func (s *storageImageDestination) untrustedLayerDiffID(layerIndex int) (digest.Digest, error) {
	// At this point, we are either inside the multi-threaded scope of HasThreadSafePutBlob,
	// nothing is writing to s.manifest yet, and s.untrustedDiffIDValues might have been set
	// by NoteOriginalOCIConfig and are not being updated any more;
	// or PutManifest has been called and s.manifest != nil.
	// Either way this function does not need the protection of s.lock.

	if s.untrustedDiffIDValues == nil {
		// Typically, we expect untrustedDiffIDValues to be set by the generic copy code
		// via NoteOriginalOCIConfig; this is a compatibility fallback for external callers
		// of the public types.ImageDestination.
		if s.manifest == nil {
			return "", errUntrustedLayerDiffIDNotYetAvailable
		}

		ctx := context.Background() // This is all happening in memory, no need to worry about cancellation.
		unparsed := image.UnparsedInstance(newUncommittedImageSource(s), nil)
		sourced, err := image.FromUnparsedImage(ctx, nil, unparsed)
		if err != nil {
			return "", fmt.Errorf("parsing image to be committed: %w", err)
		}
		configOCI, err := sourced.OCIConfig(ctx)
		if err != nil {
			return "", fmt.Errorf("obtaining config of image to be committed: %w", err)
		}

		s.setUntrustedDiffIDValuesFromOCIConfig(configOCI)
	}

	// Let entirely empty / missing diffIDs through; but if the array does exist, expect it to contain an entry for every layer,
	// and fail hard on missing entries. This tries to account for completely naive image producers who just don’t fill DiffID,
	// while still detecting incorrectly-built / confused images.
	//
	// schema1 images don’t have DiffID values in the config.
	// Our schema1.OCIConfig code produces non-empty DiffID arrays of empty values, so treat arrays of all-empty
	// values as “DiffID unknown”.
	// For schema 1, it is important to exit here, before the layerIndex >= len(s.untrustedDiffIDValues)
	// check, because the format conversion from schema1 to OCI used to compute untrustedDiffIDValues
	// changes the number of layres (drops items with Schema1V1Compatibility.ThrowAway).
	if !slices.ContainsFunc(s.untrustedDiffIDValues, func(d digest.Digest) bool {
		return d != ""
	}) {
		return "", untrustedLayerDiffIDUnknownError{
			layerIndex: layerIndex,
		}
	}
	if layerIndex >= len(s.untrustedDiffIDValues) {
		return "", fmt.Errorf("image config has only %d DiffID values, but a layer with index %d exists", len(s.untrustedDiffIDValues), layerIndex)
	}
	return s.untrustedDiffIDValues[layerIndex], nil
}

// setUntrustedDiffIDValuesFromOCIConfig updates s.untrustedDiffIDvalues from config.
// The caller must ensure s.lock does not need to be held.
func (s *storageImageDestination) setUntrustedDiffIDValuesFromOCIConfig(config *imgspecv1.Image) {
	s.untrustedDiffIDValues = slices.Clone(config.RootFS.DiffIDs)
	if s.untrustedDiffIDValues == nil { // Unlikely but possible in theory…
		s.untrustedDiffIDValues = []digest.Digest{}
	}
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (s *storageImageDestination) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	// This function is outside of the scope of HasThreadSafePutBlob, so we don’t need to hold s.lock.

	if s.manifest == nil {
		return errors.New("Internal error: storageImageDestination.CommitWithOptions() called without PutManifest()")
	}
	toplevelManifest, _, err := options.UnparsedToplevel.Manifest(ctx)
	if err != nil {
		return fmt.Errorf("retrieving top-level manifest: %w", err)
	}
	// If the name we're saving to includes a digest, then check that the
	// manifests that we're about to save all either match the one from the
	// options.UnparsedToplevel, or match the digest in the name that we're using.
	if s.imageRef.named != nil {
		if digested, ok := s.imageRef.named.(reference.Digested); ok {
			matches, err := manifest.MatchesDigest(s.manifest, digested.Digest())
			if err != nil {
				return err
			}
			if !matches {
				matches, err = manifest.MatchesDigest(toplevelManifest, digested.Digest())
				if err != nil {
					return err
				}
			}
			if !matches {
				return fmt.Errorf("Manifest to be saved does not match expected digest %s", digested.Digest())
			}
		}
	}
	// Find the list of layer blobs.
	man, err := manifest.FromBlob(s.manifest, s.manifestMIMEType)
	if err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}
	layerBlobs := man.LayerInfos()

	// Extract, commit, or find the layers.
	for i, blob := range layerBlobs {
		if stopQueue, err := s.commitLayer(i, addedLayerInfo{
			digest:     blob.Digest,
			emptyLayer: blob.EmptyLayer,
		}, blob.Size); err != nil {
			return err
		} else if stopQueue {
			return fmt.Errorf("Internal error: storageImageDestination.CommitWithOptions(): commitLayer() not ready to commit for layer %q", blob.Digest)
		}
	}
	var lastLayer string
	if len(layerBlobs) > 0 { // Zero-layer images rarely make sense, but it is technically possible, and may happen for non-image artifacts.
		prev, ok := s.indexToStorageID[len(layerBlobs)-1]
		if !ok {
			return fmt.Errorf("Internal error: storageImageDestination.CommitWithOptions(): previous layer %d hasn't been committed (lastLayer == nil)", len(layerBlobs)-1)
		}
		lastLayer = prev
	}

	// If one of those blobs was a configuration blob, then we can try to dig out the date when the image
	// was originally created, in case we're just copying it.  If not, no harm done.
	imgOptions := &storage.ImageOptions{}
	if inspect, err := man.Inspect(s.getConfigBlob); err == nil && inspect.Created != nil {
		logrus.Debugf("setting image creation date to %s", inspect.Created)
		imgOptions.CreationDate = *inspect.Created
	}

	// Set up to save the config as a data item.  Since we only share layers, the config should be in a file.
	if s.lockProtected.configDigest != "" {
		v, err := os.ReadFile(s.lockProtected.filenames[s.lockProtected.configDigest])
		if err != nil {
			return fmt.Errorf("copying config blob %q to image: %w", s.lockProtected.configDigest, err)
		}
		imgOptions.BigData = append(imgOptions.BigData, storage.ImageBigDataOption{
			Key:    s.lockProtected.configDigest.String(),
			Data:   v,
			Digest: digest.Canonical.FromBytes(v),
		})
	}
	// Set up to save the options.UnparsedToplevel's manifest if it differs from
	// the per-platform one, which is saved below.
	if !bytes.Equal(toplevelManifest, s.manifest) {
		manifestDigest, err := manifest.Digest(toplevelManifest)
		if err != nil {
			return fmt.Errorf("digesting top-level manifest: %w", err)
		}
		key, err := manifestBigDataKey(manifestDigest)
		if err != nil {
			return err
		}
		imgOptions.BigData = append(imgOptions.BigData, storage.ImageBigDataOption{
			Key:    key,
			Data:   toplevelManifest,
			Digest: manifestDigest,
		})
	}
	// Set up to save the image's manifest.  Allow looking it up by digest by using the key convention defined by the Store.
	// Record the manifest twice: using a digest-specific key to allow references to that specific digest instance,
	// and using storage.ImageDigestBigDataKey for future users that don’t specify any digest and for compatibility with older readers.
	key, err := manifestBigDataKey(s.manifestDigest)
	if err != nil {
		return err
	}
	imgOptions.BigData = append(imgOptions.BigData, storage.ImageBigDataOption{
		Key:    key,
		Data:   s.manifest,
		Digest: s.manifestDigest,
	})
	imgOptions.BigData = append(imgOptions.BigData, storage.ImageBigDataOption{
		Key:    storage.ImageDigestBigDataKey,
		Data:   s.manifest,
		Digest: s.manifestDigest,
	})
	// Set up to save the signatures, if we have any.
	if len(s.signatures) > 0 {
		imgOptions.BigData = append(imgOptions.BigData, storage.ImageBigDataOption{
			Key:    "signatures",
			Data:   s.signatures,
			Digest: digest.Canonical.FromBytes(s.signatures),
		})
	}
	for instanceDigest, signatures := range s.signatureses {
		key, err := signatureBigDataKey(instanceDigest)
		if err != nil {
			return err
		}
		imgOptions.BigData = append(imgOptions.BigData, storage.ImageBigDataOption{
			Key:    key,
			Data:   signatures,
			Digest: digest.Canonical.FromBytes(signatures),
		})
	}

	// Set up to save our metadata.
	metadata, err := json.Marshal(s.metadata)
	if err != nil {
		return fmt.Errorf("encoding metadata for image: %w", err)
	}
	if len(metadata) != 0 {
		imgOptions.Metadata = string(metadata)
	}

	// Create the image record, pointing to the most-recently added layer.
	intendedID := s.imageRef.id
	if intendedID == "" {
		intendedID, err = s.computeID(man)
		if err != nil {
			return err
		}
	}
	oldNames := []string{}
	img, err := s.imageRef.transport.store.CreateImage(intendedID, nil, lastLayer, "", imgOptions)
	if err != nil {
		if !errors.Is(err, storage.ErrDuplicateID) {
			logrus.Debugf("error creating image: %q", err)
			return fmt.Errorf("creating image %q: %w", intendedID, err)
		}
		img, err = s.imageRef.transport.store.Image(intendedID)
		if err != nil {
			return fmt.Errorf("reading image %q: %w", intendedID, err)
		}
		if img.TopLayer != lastLayer {
			logrus.Debugf("error creating image: image with ID %q exists, but uses different layers", intendedID)
			return fmt.Errorf("image with ID %q already exists, but uses a different top layer: %w", intendedID, storage.ErrDuplicateID)
		}
		logrus.Debugf("reusing image ID %q", img.ID)
		oldNames = append(oldNames, img.Names...)
		// set the data items and metadata on the already-present image
		// FIXME: this _replaces_ any "signatures" blobs and their
		// sizes (tracked in the metadata) which might have already
		// been present with new values, when ideally we'd find a way
		// to merge them since they all apply to the same image
		for _, data := range imgOptions.BigData {
			if err := s.imageRef.transport.store.SetImageBigData(img.ID, data.Key, data.Data, manifest.Digest); err != nil {
				logrus.Debugf("error saving big data %q for image %q: %v", data.Key, img.ID, err)
				return fmt.Errorf("saving big data %q for image %q: %w", data.Key, img.ID, err)
			}
		}
		if imgOptions.Metadata != "" {
			if err := s.imageRef.transport.store.SetMetadata(img.ID, imgOptions.Metadata); err != nil {
				logrus.Debugf("error saving metadata for image %q: %v", img.ID, err)
				return fmt.Errorf("saving metadata for image %q: %w", img.ID, err)
			}
			logrus.Debugf("saved image metadata %q", imgOptions.Metadata)
		}
	} else {
		logrus.Debugf("created new image ID %q with metadata %q", img.ID, imgOptions.Metadata)
	}

	// Clean up the unfinished image on any error.
	// (Is this the right thing to do if the image has existed before?)
	commitSucceeded := false
	defer func() {
		if !commitSucceeded {
			logrus.Errorf("Updating image %q (old names %v) failed, deleting it", img.ID, oldNames)
			if _, err := s.imageRef.transport.store.DeleteImage(img.ID, true); err != nil {
				logrus.Errorf("Error deleting incomplete image %q: %v", img.ID, err)
			}
		}
	}()

	// Add the reference's name on the image.  We don't need to worry about avoiding duplicate
	// values because AddNames() will deduplicate the list that we pass to it.
	if name := s.imageRef.DockerReference(); name != nil {
		if err := s.imageRef.transport.store.AddNames(img.ID, []string{name.String()}); err != nil {
			return fmt.Errorf("adding names %v to image %q: %w", name, img.ID, err)
		}
		logrus.Debugf("added name %q to image %q", name, img.ID)
	}
	if options.ReportResolvedReference != nil {
		// FIXME? This is using nil for the named reference.
		// It would be better to also  use s.imageRef.named, because that allows us to resolve to the right
		// digest / manifest (and corresponding signatures).
		// The problem with that is that resolving such a reference fails if the s.imageRef.named name is moved to a different image
		// (because it is a tag that moved, or because we have pulled “the same” image for a different architecture).
		// Right now (2024-11), ReportResolvedReference is only used in c/common/libimage, where the caller only extracts the image ID,
		// so the name does not matter; to give us options, copy.Options.ReportResolvedReference is explicitly refusing to document
		// whether the value contains a name.
		resolved, err := newReference(s.imageRef.transport, nil, intendedID)
		if err != nil {
			return fmt.Errorf("creating a resolved reference for (%s, %s): %w", s.imageRef.StringWithinTransport(), intendedID, err)
		}
		*options.ReportResolvedReference = resolved
	}

	commitSucceeded = true
	return nil
}

// PutManifest writes the manifest to the destination.
func (s *storageImageDestination) PutManifest(ctx context.Context, manifestBlob []byte, instanceDigest *digest.Digest) error {
	digest, err := manifest.Digest(manifestBlob)
	if err != nil {
		return err
	}
	s.manifest = bytes.Clone(manifestBlob)
	if s.manifest == nil { // Make sure PutManifest can never succeed with s.manifest == nil
		s.manifest = []byte{}
	}
	s.manifestMIMEType = manifest.GuessMIMEType(s.manifest)
	s.manifestDigest = digest
	return nil
}

// PutSignaturesWithFormat writes a set of signatures to the destination.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to write or overwrite the signatures for
// (when the primary manifest is a manifest list); this should always be nil if the primary manifest is not a manifest list.
// MUST be called after PutManifest (signatures may reference manifest contents).
func (s *storageImageDestination) PutSignaturesWithFormat(ctx context.Context, signatures []signature.Signature, instanceDigest *digest.Digest) error {
	sizes := []int{}
	sigblob := []byte{}
	for _, sigWithFormat := range signatures {
		sig, err := signature.Blob(sigWithFormat)
		if err != nil {
			return err
		}
		sizes = append(sizes, len(sig))
		sigblob = append(sigblob, sig...)
	}
	if instanceDigest == nil {
		s.signatures = sigblob
		s.metadata.SignatureSizes = sizes
		if s.manifest != nil {
			manifestDigest := s.manifestDigest
			instanceDigest = &manifestDigest
		}
	}
	if instanceDigest != nil {
		s.signatureses[*instanceDigest] = sigblob
		s.metadata.SignaturesSizes[*instanceDigest] = sizes
	}
	return nil
}
