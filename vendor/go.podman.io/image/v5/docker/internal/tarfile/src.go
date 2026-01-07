package tarfile

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"

	digest "github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/imagesource/impl"
	"go.podman.io/image/v5/internal/imagesource/stubs"
	"go.podman.io/image/v5/internal/iolimits"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/types"
)

// Source is a partial implementation of types.ImageSource for reading from tarPath.
type Source struct {
	impl.Compat
	impl.PropertyMethodsInitialize
	impl.NoSignatures
	impl.DoesNotAffectLayerInfosForCopy
	stubs.NoGetBlobAtInitialize

	archive      *Reader
	closeArchive bool // .Close() the archive when the source is closed.
	// If ref is nil and sourceIndex is -1, indicates the only image in the archive.
	ref         reference.NamedTagged // May be nil
	sourceIndex int                   // May be -1
	// The following data is only available after ensureCachedDataIsPresent() succeeds
	tarManifest       *ManifestItem // nil if not available yet.
	configBytes       []byte
	configDigest      digest.Digest
	orderedDiffIDList []digest.Digest
	knownLayers       map[digest.Digest]*layerInfo
	// Other state
	generatedManifest []byte    // Private cache for GetManifest(), nil if not set yet.
	cacheDataLock     sync.Once // Private state for ensureCachedDataIsPresent to make it concurrency-safe
	cacheDataResult   error     // Private state for ensureCachedDataIsPresent
}

type layerInfo struct {
	path string
	size int64
}

// NewSource returns a tarfile.Source for an image in the specified archive matching ref
// and sourceIndex (or the only image if they are (nil, -1)).
// The archive will be closed if closeArchive
func NewSource(archive *Reader, closeArchive bool, transportName string, ref reference.NamedTagged, sourceIndex int) *Source {
	s := &Source{
		PropertyMethodsInitialize: impl.PropertyMethods(impl.Properties{
			HasThreadSafeGetBlob: true,
		}),
		NoGetBlobAtInitialize: stubs.NoGetBlobAtRaw(transportName),

		archive:      archive,
		closeArchive: closeArchive,
		ref:          ref,
		sourceIndex:  sourceIndex,
	}
	s.Compat = impl.AddCompat(s)
	return s
}

// ensureCachedDataIsPresent loads data necessary for any of the public accessors.
// It is safe to call this from multi-threaded code.
func (s *Source) ensureCachedDataIsPresent() error {
	s.cacheDataLock.Do(func() {
		s.cacheDataResult = s.ensureCachedDataIsPresentPrivate()
	})
	return s.cacheDataResult
}

// ensureCachedDataIsPresentPrivate is a private implementation detail of ensureCachedDataIsPresent.
// Call ensureCachedDataIsPresent instead.
func (s *Source) ensureCachedDataIsPresentPrivate() error {
	tarManifest, _, err := s.archive.ChooseManifestItem(s.ref, s.sourceIndex)
	if err != nil {
		return err
	}

	// Read and parse config.
	configBytes, err := s.archive.readTarComponent(tarManifest.Config, iolimits.MaxConfigBodySize)
	if err != nil {
		return err
	}
	var parsedConfig manifest.Schema2Image // There's a lot of info there, but we only really care about layer DiffIDs.
	if err := json.Unmarshal(configBytes, &parsedConfig); err != nil {
		return fmt.Errorf("decoding tar config %q: %w", tarManifest.Config, err)
	}
	if parsedConfig.RootFS == nil {
		return fmt.Errorf("Invalid image config (rootFS is not set): %q", tarManifest.Config)
	}

	knownLayers, err := s.prepareLayerData(tarManifest, &parsedConfig)
	if err != nil {
		return err
	}

	// Success; commit.
	s.tarManifest = tarManifest
	s.configBytes = configBytes
	s.configDigest = digest.FromBytes(configBytes)
	s.orderedDiffIDList = parsedConfig.RootFS.DiffIDs
	s.knownLayers = knownLayers
	return nil
}

// Close removes resources associated with an initialized Source, if any.
func (s *Source) Close() error {
	if s.closeArchive {
		return s.archive.Close()
	}
	return nil
}

// TarManifest returns contents of manifest.json
func (s *Source) TarManifest() []ManifestItem {
	return s.archive.Manifest
}

func (s *Source) prepareLayerData(tarManifest *ManifestItem, parsedConfig *manifest.Schema2Image) (map[digest.Digest]*layerInfo, error) {
	// Collect layer data available in manifest and config.
	if len(tarManifest.Layers) != len(parsedConfig.RootFS.DiffIDs) {
		return nil, fmt.Errorf("Inconsistent layer count: %d in manifest, %d in config", len(tarManifest.Layers), len(parsedConfig.RootFS.DiffIDs))
	}
	knownLayers := map[digest.Digest]*layerInfo{}
	unknownLayerSizes := map[string]*layerInfo{} // Points into knownLayers, a "to do list" of items with unknown sizes.
	for i, diffID := range parsedConfig.RootFS.DiffIDs {
		if _, ok := knownLayers[diffID]; ok {
			// Apparently it really can happen that a single image contains the same layer diff more than once.
			// In that case, the diffID validation ensures that both layers truly are the same, and it should not matter
			// which of the tarManifest.Layers paths is used; (docker save) actually makes the duplicates symlinks to the original.
			continue
		}
		layerPath := path.Clean(tarManifest.Layers[i])
		if _, ok := unknownLayerSizes[layerPath]; ok {
			return nil, fmt.Errorf("Layer tarfile %q used for two different DiffID values", layerPath)
		}
		li := &layerInfo{ // A new element in each iteration
			path: layerPath,
			size: -1,
		}
		knownLayers[diffID] = li
		unknownLayerSizes[layerPath] = li
	}

	// Scan the tar file to collect layer sizes.
	file, err := os.Open(s.archive.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	t := tar.NewReader(file)
	for {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		layerPath := path.Clean(h.Name)
		// FIXME: Cache this data across images in Reader.
		if li, ok := unknownLayerSizes[layerPath]; ok {
			// Since GetBlob will decompress layers that are compressed we need
			// to do the decompression here as well, otherwise we will
			// incorrectly report the size. Pretty critical, since tools like
			// umoci always compress layer blobs. Obviously we only bother with
			// the slower method of checking if it's compressed.
			uncompressedStream, isCompressed, err := compression.AutoDecompress(t)
			if err != nil {
				return nil, fmt.Errorf("auto-decompressing %q to determine its size: %w", layerPath, err)
			}
			defer uncompressedStream.Close()

			uncompressedSize := h.Size
			if isCompressed {
				uncompressedSize, err = io.Copy(io.Discard, uncompressedStream)
				if err != nil {
					return nil, fmt.Errorf("reading %q to find its size: %w", layerPath, err)
				}
			}
			li.size = uncompressedSize
			delete(unknownLayerSizes, layerPath)
		}
	}
	if len(unknownLayerSizes) != 0 {
		return nil, errors.New("Some layer tarfiles are missing in the tarball") // This could do with a better error reporting, if this ever happened in practice.
	}

	return knownLayers, nil
}

// GetManifest returns the image's manifest along with its MIME type (which may be empty when it can't be determined but the manifest is available).
// It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve (when the primary manifest is a manifest list);
// this never happens if the primary manifest is not a manifest list (e.g. if the source never returns manifest lists).
// This source implementation does not support manifest lists, so the passed-in instanceDigest should always be nil,
// as the primary manifest can not be a list, so there can be no secondary instances.
func (s *Source) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	if instanceDigest != nil {
		// How did we even get here? GetManifest(ctx, nil) has returned a manifest.DockerV2Schema2MediaType.
		return nil, "", errors.New(`Manifest lists are not supported by "docker-daemon:"`)
	}
	if s.generatedManifest == nil {
		if err := s.ensureCachedDataIsPresent(); err != nil {
			return nil, "", err
		}
		m := manifest.Schema2{
			SchemaVersion: 2,
			MediaType:     manifest.DockerV2Schema2MediaType,
			ConfigDescriptor: manifest.Schema2Descriptor{
				MediaType: manifest.DockerV2Schema2ConfigMediaType,
				Size:      int64(len(s.configBytes)),
				Digest:    s.configDigest,
			},
			LayersDescriptors: []manifest.Schema2Descriptor{},
		}
		for _, diffID := range s.orderedDiffIDList {
			li, ok := s.knownLayers[diffID]
			if !ok {
				return nil, "", fmt.Errorf("Internal inconsistency: Information about layer %s missing", diffID)
			}
			m.LayersDescriptors = append(m.LayersDescriptors, manifest.Schema2Descriptor{
				Digest:    diffID, // diffID is a digest of the uncompressed tarball
				MediaType: manifest.DockerV2Schema2LayerMediaType,
				Size:      li.size,
			})
		}
		manifestBytes, err := json.Marshal(&m)
		if err != nil {
			return nil, "", err
		}
		s.generatedManifest = manifestBytes
	}
	return s.generatedManifest, manifest.DockerV2Schema2MediaType, nil
}

// uncompressedReadCloser is an io.ReadCloser that closes both the uncompressed stream and the underlying input.
type uncompressedReadCloser struct {
	io.Reader
	underlyingCloser   func() error
	uncompressedCloser func() error
}

func (r uncompressedReadCloser) Close() error {
	var res error
	if err := r.uncompressedCloser(); err != nil {
		res = err
	}
	if err := r.underlyingCloser(); err != nil && res == nil {
		res = err
	}
	return res
}

// GetBlob returns a stream for the specified blob, and the blob’s size (or -1 if unknown).
// The Digest field in BlobInfo is guaranteed to be provided, Size may be -1 and MediaType may be optionally provided.
// May update BlobInfoCache, preferably after it knows for certain that a blob truly exists at a specific location.
func (s *Source) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	if err := s.ensureCachedDataIsPresent(); err != nil {
		return nil, 0, err
	}

	if info.Digest == s.configDigest { // FIXME? Implement a more general algorithm matching instead of assuming sha256.
		return io.NopCloser(bytes.NewReader(s.configBytes)), int64(len(s.configBytes)), nil
	}

	if li, ok := s.knownLayers[info.Digest]; ok { // diffID is a digest of the uncompressed tarball,
		underlyingStream, err := s.archive.openTarComponent(li.path)
		if err != nil {
			return nil, 0, err
		}
		closeUnderlyingStream := true
		defer func() {
			if closeUnderlyingStream {
				underlyingStream.Close()
			}
		}()

		// In order to handle the fact that digests != diffIDs (and thus that a
		// caller which is trying to verify the blob will run into problems),
		// we need to decompress blobs. This is a bit ugly, but it's a
		// consequence of making everything addressable by their DiffID rather
		// than by their digest...
		//
		// In particular, because the v2s2 manifest being generated uses
		// DiffIDs, any caller of GetBlob is going to be asking for DiffIDs of
		// layers not their _actual_ digest. The result is that copy/... will
		// be verifying a "digest" which is not the actual layer's digest (but
		// is instead the DiffID).

		uncompressedStream, _, err := compression.AutoDecompress(underlyingStream)
		if err != nil {
			return nil, 0, fmt.Errorf("auto-decompressing blob %s: %w", info.Digest, err)
		}

		newStream := uncompressedReadCloser{
			Reader:             uncompressedStream,
			underlyingCloser:   underlyingStream.Close,
			uncompressedCloser: uncompressedStream.Close,
		}
		closeUnderlyingStream = false

		return newStream, li.size, nil
	}

	return nil, 0, fmt.Errorf("Unknown blob %s", info.Digest)
}
