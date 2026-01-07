//go:build !containers_image_storage_stub

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sync"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/internal/imagesource/impl"
	"go.podman.io/image/v5/internal/imagesource/stubs"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/internal/tmpdir"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chunked/toc"
	"go.podman.io/storage/pkg/ioutils"
)

type storageImageSource struct {
	impl.Compat
	impl.PropertyMethodsInitialize
	stubs.NoGetBlobAtInitialize

	imageRef               storageReference
	image                  *storage.Image
	systemContext          *types.SystemContext // SystemContext used in GetBlob() to create temporary files
	metadata               storageImageMetadata
	cachedManifest         []byte     // A cached copy of the manifest, if already known, or nil
	cachedManifestMIMEType string     // Valid if cachedManifest != nil
	getBlobMutex           sync.Mutex // Mutex to sync state for parallel GetBlob executions
	getBlobMutexProtected  getBlobMutexProtected
}

// getBlobMutexProtected contains storageImageSource data protected by getBlobMutex.
type getBlobMutexProtected struct {
	// digestToLayerID is a lookup map from a possibly-untrusted uncompressed layer digest (as returned by LayerInfosForCopy) to the
	// layer ID in the store.
	digestToLayerID map[digest.Digest]string

	// layerPosition stores where we are in reading a blob's layers
	layerPosition map[digest.Digest]int
}

// expectedLayerDiffIDFlag is a per-layer flag containing an UNTRUSTED uncompressed digest of the layer.
// It is set when pulling a layer by TOC; later, this value is used with digestToLayerID
// to allow identifying the layer — and the consumer is expected to verify the blob returned by GetBlob against the digest.
const expectedLayerDiffIDFlag = "expected-layer-diffid"

// newImageSource sets up an image for reading.
func newImageSource(sys *types.SystemContext, imageRef storageReference) (*storageImageSource, error) {
	// First, locate the image.
	img, err := imageRef.resolveImage(sys)
	if err != nil {
		return nil, err
	}

	// Build the reader object.
	image := &storageImageSource{
		PropertyMethodsInitialize: impl.PropertyMethods(impl.Properties{
			HasThreadSafeGetBlob: true,
		}),
		NoGetBlobAtInitialize: stubs.NoGetBlobAt(imageRef),

		imageRef:      imageRef,
		systemContext: sys,
		image:         img,
		metadata: storageImageMetadata{
			SignatureSizes:  []int{},
			SignaturesSizes: make(map[digest.Digest][]int),
		},
		getBlobMutexProtected: getBlobMutexProtected{
			digestToLayerID: make(map[digest.Digest]string),
			layerPosition:   make(map[digest.Digest]int),
		},
	}
	image.Compat = impl.AddCompat(image)
	if img.Metadata != "" {
		if err := json.Unmarshal([]byte(img.Metadata), &image.metadata); err != nil {
			return nil, fmt.Errorf("decoding metadata for source image: %w", err)
		}
	}
	return image, nil
}

// Reference returns the image reference that we used to find this image.
func (s *storageImageSource) Reference() types.ImageReference {
	return s.imageRef
}

// Close cleans up any resources we tied up while reading the image.
func (s *storageImageSource) Close() error {
	return nil
}

// GetBlob returns a stream for the specified blob, and the blob’s size (or -1 if unknown).
// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
func (s *storageImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	// We need a valid digest value.
	digest := info.Digest

	if err := digest.Validate(); err != nil {
		return nil, 0, err
	}

	if digest == image.GzippedEmptyLayerDigest {
		return io.NopCloser(bytes.NewReader(image.GzippedEmptyLayer)), int64(len(image.GzippedEmptyLayer)), nil
	}

	var layers []storage.Layer

	// This lookup path is strictly necessary for layers identified by TOC digest
	// (where LayersByUncompressedDigest might not find our layer);
	// for other layers it is an optimization to avoid the cost of the LayersByUncompressedDigest call.
	s.getBlobMutex.Lock()
	layerID, found := s.getBlobMutexProtected.digestToLayerID[digest]
	s.getBlobMutex.Unlock()

	if found {
		if layer, err := s.imageRef.transport.store.Layer(layerID); err == nil {
			layers = []storage.Layer{*layer}
		}
	} else {
		// Check if the blob corresponds to a diff that was used to initialize any layers.  Our
		// callers should try to retrieve layers using their uncompressed digests, so no need to
		// check if they're using one of the compressed digests, which we can't reproduce anyway.
		layers, _ = s.imageRef.transport.store.LayersByUncompressedDigest(digest)
	}

	// If it's not a layer, then it must be a data item.
	if len(layers) == 0 {
		b, err := s.imageRef.transport.store.ImageBigData(s.image.ID, digest.String())
		if err != nil {
			return nil, 0, err
		}
		r := bytes.NewReader(b)
		logrus.Debugf("exporting opaque data as blob %q", digest.String())
		return io.NopCloser(r), int64(r.Len()), nil
	}

	// NOTE: the blob is first written to a temporary file and subsequently
	// closed.  The intention is to keep the time we own the storage lock
	// as short as possible to allow other processes to access the storage.
	rc, n, _, err := s.getBlobAndLayerID(digest, layers)
	if err != nil {
		return nil, 0, err
	}
	defer rc.Close()

	tmpFile, err := tmpdir.CreateBigFileTemp(s.systemContext, "")
	if err != nil {
		return nil, 0, err
	}
	success := false
	tmpFileRemovePending := true
	defer func() {
		if !success {
			tmpFile.Close()
			if tmpFileRemovePending {
				os.Remove(tmpFile.Name())
			}
		}
	}()
	// On Unix and modern Windows (2022 at least) we can eagerly unlink the file to ensure it's automatically
	// cleaned up on process termination (or if the caller forgets to invoke Close())
	// On older versions of Windows we will have to fallback to relying on the caller to invoke Close()
	if err := os.Remove(tmpFile.Name()); err == nil {
		tmpFileRemovePending = false
	}

	if _, err := io.Copy(tmpFile, rc); err != nil {
		return nil, 0, err
	}
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}

	success = true

	if tmpFileRemovePending {
		return ioutils.NewReadCloserWrapper(tmpFile, func() error {
			tmpFile.Close()
			return os.Remove(tmpFile.Name())
		}), n, nil
	}

	return tmpFile, n, nil
}

// getBlobAndLayer reads the data blob or filesystem layer which matches the digest and size, if given.
func (s *storageImageSource) getBlobAndLayerID(digest digest.Digest, layers []storage.Layer) (rc io.ReadCloser, n int64, layerID string, err error) {
	var layer storage.Layer
	var diffOptions *storage.DiffOptions

	// Step through the list of matching layers.  Tests may want to verify that if we have multiple layers
	// which claim to have the same contents, that we actually do have multiple layers, otherwise we could
	// just go ahead and use the first one every time.
	s.getBlobMutex.Lock()
	i := s.getBlobMutexProtected.layerPosition[digest]
	s.getBlobMutexProtected.layerPosition[digest] = i + 1
	s.getBlobMutex.Unlock()
	if len(layers) > 0 {
		layer = layers[i%len(layers)]
	}
	// Force the storage layer to not try to match any compression that was used when the layer was first
	// handed to it.
	noCompression := archive.Uncompressed
	diffOptions = &storage.DiffOptions{
		Compression: &noCompression,
	}
	if layer.UncompressedSize < 0 {
		n = -1
	} else {
		n = layer.UncompressedSize
	}
	logrus.Debugf("exporting filesystem layer %q without compression for blob %q", layer.ID, digest)
	rc, err = s.imageRef.transport.store.Diff("", layer.ID, diffOptions)
	if err != nil {
		return nil, -1, "", err
	}
	return rc, n, layer.ID, err
}

// GetManifest() reads the image's manifest.
func (s *storageImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) (manifestBlob []byte, mimeType string, err error) {
	if instanceDigest != nil {
		key, err := manifestBigDataKey(*instanceDigest)
		if err != nil {
			return nil, "", err
		}
		blob, err := s.imageRef.transport.store.ImageBigData(s.image.ID, key)
		if err != nil {
			return nil, "", fmt.Errorf("reading manifest for image instance %q: %w", *instanceDigest, err)
		}
		return blob, manifest.GuessMIMEType(blob), err
	}
	if s.cachedManifest == nil {
		// The manifest is stored as a big data item.
		// Prefer the manifest corresponding to the user-specified digest, if available.
		if s.imageRef.named != nil {
			if digested, ok := s.imageRef.named.(reference.Digested); ok {
				key, err := manifestBigDataKey(digested.Digest())
				if err != nil {
					return nil, "", err
				}
				blob, err := s.imageRef.transport.store.ImageBigData(s.image.ID, key)
				if err != nil && !os.IsNotExist(err) { // os.IsNotExist is true if the image exists but there is no data corresponding to key
					return nil, "", err
				}
				if err == nil {
					s.cachedManifest = blob
				}
			}
		}
		// If the user did not specify a digest, or this is an old image stored before manifestBigDataKey was introduced, use the default manifest.
		// Note that the manifest may not match the expected digest, and that is likely to fail eventually, e.g. in c/image/image/UnparsedImage.Manifest().
		if s.cachedManifest == nil {
			cachedBlob, err := s.imageRef.transport.store.ImageBigData(s.image.ID, storage.ImageDigestBigDataKey)
			if err != nil {
				return nil, "", err
			}
			s.cachedManifest = cachedBlob
		}
		s.cachedManifestMIMEType = manifest.GuessMIMEType(s.cachedManifest)
	}
	return s.cachedManifest, s.cachedManifestMIMEType, err
}

// LayerInfosForCopy() returns the list of layer blobs that make up the root filesystem of
// the image, after they've been decompressed.
func (s *storageImageSource) LayerInfosForCopy(ctx context.Context, instanceDigest *digest.Digest) ([]types.BlobInfo, error) {
	manifestBlob, manifestType, err := s.GetManifest(ctx, instanceDigest)
	if err != nil {
		return nil, fmt.Errorf("reading image manifest for %q: %w", s.image.ID, err)
	}
	if manifest.MIMETypeIsMultiImage(manifestType) {
		return nil, errors.New("can't copy layers for a manifest list (shouldn't be attempted)")
	}
	man, err := manifest.FromBlob(manifestBlob, manifestType)
	if err != nil {
		return nil, fmt.Errorf("parsing image manifest for %q: %w", s.image.ID, err)
	}

	uncompressedLayerType := ""
	gzipCompressedLayerType := ""
	switch manifestType {
	case imgspecv1.MediaTypeImageManifest:
		uncompressedLayerType = imgspecv1.MediaTypeImageLayer
		gzipCompressedLayerType = imgspecv1.MediaTypeImageLayerGzip
	case manifest.DockerV2Schema1MediaType, manifest.DockerV2Schema1SignedMediaType, manifest.DockerV2Schema2MediaType:
		uncompressedLayerType = manifest.DockerV2SchemaLayerMediaTypeUncompressed
		gzipCompressedLayerType = manifest.DockerV2Schema2LayerMediaType
	}

	physicalBlobInfos := []layerForCopy{} // Built reversed
	layerID := s.image.TopLayer
	for layerID != "" {
		layer, err := s.imageRef.transport.store.Layer(layerID)
		if err != nil {
			return nil, fmt.Errorf("reading layer %q in image %q: %w", layerID, s.image.ID, err)
		}

		blobDigest := layer.UncompressedDigest
		if blobDigest == "" {
			if layer.TOCDigest == "" {
				return nil, fmt.Errorf("uncompressed digest and TOC digest for layer %q is unknown", layerID)
			}
			if layer.Flags == nil || layer.Flags[expectedLayerDiffIDFlag] == nil {
				return nil, fmt.Errorf("TOC digest %q for layer %q is present but %q flag is not set", layer.TOCDigest, layerID, expectedLayerDiffIDFlag)
			}
			expectedDigest, ok := layer.Flags[expectedLayerDiffIDFlag].(string)
			if !ok {
				return nil, fmt.Errorf("TOC digest %q for layer %q is present but %q flag is not a string", layer.TOCDigest, layerID, expectedLayerDiffIDFlag)
			}
			// If the layer is stored by its TOC, report the expected diffID as the layer Digest;
			// the generic code is responsible for validating the digest.
			// We can locate the layer without further c/storage help using s.getBlobMutexProtected.digestToLayerID.
			blobDigest, err = digest.Parse(expectedDigest)
			if err != nil {
				return nil, fmt.Errorf("parsing expected diffID %q for layer %q: %w", expectedDigest, layerID, err)
			}
		}
		size := layer.UncompressedSize
		if size < 0 {
			size = -1
		}
		s.getBlobMutex.Lock()
		s.getBlobMutexProtected.digestToLayerID[blobDigest] = layer.ID
		s.getBlobMutex.Unlock()
		layerInfo := layerForCopy{
			digest:    blobDigest,
			size:      size,
			mediaType: uncompressedLayerType,
		}
		physicalBlobInfos = append(physicalBlobInfos, layerInfo)
		layerID = layer.Parent
	}
	slices.Reverse(physicalBlobInfos)

	res, err := buildLayerInfosForCopy(man.LayerInfos(), physicalBlobInfos, gzipCompressedLayerType)
	if err != nil {
		return nil, fmt.Errorf("creating LayerInfosForCopy of image %q: %w", s.image.ID, err)
	}
	return res, nil
}

// layerForCopy is information about a physical layer, an edit to be made by buildLayerInfosForCopy.
type layerForCopy struct {
	digest    digest.Digest
	size      int64
	mediaType string
}

// buildLayerInfosForCopy builds a LayerInfosForCopy return value based on manifestInfos from the original manifest,
// but using layer data which we can actually produce — physicalInfos for non-empty layers,
// and image.GzippedEmptyLayer with gzipCompressedLayerType for empty ones.
// (This is split basically only to allow easily unit-testing the part that has no dependencies on the external environment.)
func buildLayerInfosForCopy(manifestInfos []manifest.LayerInfo, physicalInfos []layerForCopy, gzipCompressedLayerType string) ([]types.BlobInfo, error) {
	nextPhysical := 0
	res := make([]types.BlobInfo, len(manifestInfos))
	for i, mi := range manifestInfos {
		if mi.EmptyLayer {
			res[i] = types.BlobInfo{
				Digest:      image.GzippedEmptyLayerDigest,
				Size:        int64(len(image.GzippedEmptyLayer)),
				URLs:        mi.URLs,
				Annotations: mi.Annotations,
				MediaType:   gzipCompressedLayerType,
			}
		} else {
			if nextPhysical >= len(physicalInfos) {
				return nil, fmt.Errorf("expected more than %d physical layers to exist", len(physicalInfos))
			}
			res[i] = types.BlobInfo{
				Digest:      physicalInfos[nextPhysical].digest,
				Size:        physicalInfos[nextPhysical].size,
				URLs:        mi.URLs,
				Annotations: mi.Annotations,
				MediaType:   physicalInfos[nextPhysical].mediaType,
			}
			nextPhysical++
		}
		// We have changed the compression format, so strip compression-related annotations.
		if res[i].Annotations != nil {
			maps.DeleteFunc(res[i].Annotations, func(key string, _ string) bool {
				_, ok := toc.ChunkedAnnotations[key]
				return ok
			})
			if len(res[i].Annotations) == 0 {
				res[i].Annotations = nil
			}
		}
	}
	if nextPhysical != len(physicalInfos) {
		return nil, fmt.Errorf("used only %d out of %d physical layers", nextPhysical, len(physicalInfos))
	}
	return res, nil
}

// GetSignaturesWithFormat returns the image's signatures.  It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
func (s *storageImageSource) GetSignaturesWithFormat(ctx context.Context, instanceDigest *digest.Digest) ([]signature.Signature, error) {
	var offset int
	signatureBlobs := []byte{}
	signatureSizes := s.metadata.SignatureSizes
	key := "signatures"
	instance := "default instance"
	if instanceDigest != nil {
		signatureSizes = s.metadata.SignaturesSizes[*instanceDigest]
		k, err := signatureBigDataKey(*instanceDigest)
		if err != nil {
			return nil, err
		}
		key = k
		if err := instanceDigest.Validate(); err != nil { // digest.Digest.Encoded() panics on failure, so validate explicitly.
			return nil, err
		}
		instance = instanceDigest.Encoded()
	}
	if len(signatureSizes) > 0 {
		data, err := s.imageRef.transport.store.ImageBigData(s.image.ID, key)
		if err != nil {
			return nil, fmt.Errorf("looking up signatures data for image %q (%s): %w", s.image.ID, instance, err)
		}
		signatureBlobs = data
	}
	res := []signature.Signature{}
	for _, length := range signatureSizes {
		if offset+length > len(signatureBlobs) {
			return nil, fmt.Errorf("looking up signatures data for image %q (%s): expected at least %d bytes, only found %d", s.image.ID, instance, len(signatureBlobs), offset+length)
		}
		sig, err := signature.FromBlob(signatureBlobs[offset : offset+length])
		if err != nil {
			return nil, fmt.Errorf("parsing signature at (%d, %d): %w", offset, length, err)
		}
		res = append(res, sig)
		offset += length
	}
	if offset != len(signatureBlobs) {
		return nil, fmt.Errorf("signatures data (%s) contained %d extra bytes", instance, len(signatureBlobs)-offset)
	}
	return res, nil
}

// getSize() adds up the sizes of the image's data blobs (which includes the configuration blob), the
// signatures, and the uncompressed sizes of all of the image's layers.
func (s *storageImageSource) getSize() (int64, error) {
	var sum int64
	// Size up the data blobs.
	dataNames, err := s.imageRef.transport.store.ListImageBigData(s.image.ID)
	if err != nil {
		return -1, fmt.Errorf("reading image %q: %w", s.image.ID, err)
	}
	for _, dataName := range dataNames {
		bigSize, err := s.imageRef.transport.store.ImageBigDataSize(s.image.ID, dataName)
		if err != nil {
			return -1, fmt.Errorf("reading data blob size %q for %q: %w", dataName, s.image.ID, err)
		}
		sum += bigSize
	}
	// Add the signature sizes.
	for _, sigSize := range s.metadata.SignatureSizes {
		sum += int64(sigSize)
	}
	// Walk the layer list.
	layerID := s.image.TopLayer
	for layerID != "" {
		layer, err := s.imageRef.transport.store.Layer(layerID)
		if err != nil {
			return -1, err
		}
		if (layer.TOCDigest == "" && layer.UncompressedDigest == "") || (layer.TOCDigest == "" && layer.UncompressedSize < 0) {
			return -1, fmt.Errorf("size for layer %q is unknown, failing getSize()", layerID)
		}
		// FIXME: We allow layer.UncompressedSize < 0 above, because currently images in an Additional Layer Store don’t provide that value.
		// Right now, various callers in Podman (and, also, newImage in this package) don’t expect the size computation to fail.
		// Should we update the callers, or do we need to continue returning inaccurate information here? Or should we pay the cost
		// to compute the size from the diff?
		if layer.UncompressedSize >= 0 {
			sum += layer.UncompressedSize
		}
		if layer.Parent == "" {
			break
		}
		layerID = layer.Parent
	}
	return sum, nil
}

// Size() adds up the sizes of the image's data blobs (which includes the configuration blob), the
// signatures, and the uncompressed sizes of all of the image's layers.
func (s *storageImageSource) Size() (int64, error) {
	return s.getSize()
}
