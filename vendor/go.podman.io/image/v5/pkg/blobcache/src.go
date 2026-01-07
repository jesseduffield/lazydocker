package blobcache

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"sync"

	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/internal/imagesource"
	"go.podman.io/image/v5/internal/imagesource/impl"
	"go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

type blobCacheSource struct {
	impl.Compat

	reference *BlobCache
	source    private.ImageSource
	sys       types.SystemContext
	// this mutex synchronizes the counters below
	mu          sync.Mutex
	cacheHits   int64
	cacheMisses int64
	cacheErrors int64
}

func (b *BlobCache) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	src, err := b.reference.NewImageSource(ctx, sys)
	if err != nil {
		return nil, fmt.Errorf("error creating new image source %q: %w", transports.ImageName(b.reference), err)
	}
	logrus.Debugf("starting to read from image %q using blob cache in %q (compression=%v)", transports.ImageName(b.reference), b.directory, b.compress)
	s := &blobCacheSource{reference: b, source: imagesource.FromPublic(src), sys: *sys}
	s.Compat = impl.AddCompat(s)
	return s, nil
}

func (s *blobCacheSource) Reference() types.ImageReference {
	return s.reference
}

func (s *blobCacheSource) Close() error {
	logrus.Debugf("finished reading from image %q using blob cache: cache had %d hits, %d misses, %d errors", transports.ImageName(s.reference), s.cacheHits, s.cacheMisses, s.cacheErrors)
	return s.source.Close()
}

func (s *blobCacheSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	if instanceDigest != nil {
		filename, err := s.reference.blobPath(*instanceDigest, false)
		if err != nil {
			return nil, "", err
		}
		manifestBytes, err := os.ReadFile(filename)
		if err == nil {
			s.cacheHits++
			return manifestBytes, manifest.GuessMIMEType(manifestBytes), nil
		}
		if !os.IsNotExist(err) {
			s.cacheErrors++
			return nil, "", fmt.Errorf("checking for manifest file: %w", err)
		}
	}
	s.cacheMisses++
	return s.source.GetManifest(ctx, instanceDigest)
}

func (s *blobCacheSource) HasThreadSafeGetBlob() bool {
	return s.source.HasThreadSafeGetBlob()
}

func (s *blobCacheSource) GetBlob(ctx context.Context, blobinfo types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	blobPath, size, _, err := s.reference.findBlob(blobinfo)
	if err != nil {
		return nil, -1, err
	}
	if blobPath != "" {
		f, err := os.Open(blobPath)
		if err == nil {
			s.mu.Lock()
			s.cacheHits++
			s.mu.Unlock()
			return f, size, nil
		}
		if !os.IsNotExist(err) {
			s.mu.Lock()
			s.cacheErrors++
			s.mu.Unlock()
			return nil, -1, fmt.Errorf("checking for cache: %w", err)
		}
	}
	s.mu.Lock()
	s.cacheMisses++
	s.mu.Unlock()
	rc, size, err := s.source.GetBlob(ctx, blobinfo, cache)
	if err != nil {
		return rc, size, fmt.Errorf("error reading blob from source image %q: %w", transports.ImageName(s.reference), err)
	}
	return rc, size, nil
}

// GetSignaturesWithFormat returns the image's signatures.  It may use a remote (= slow) service.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve signatures for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
func (s *blobCacheSource) GetSignaturesWithFormat(ctx context.Context, instanceDigest *digest.Digest) ([]signature.Signature, error) {
	return s.source.GetSignaturesWithFormat(ctx, instanceDigest)
}

// layerInfoForCopy returns a possibly-updated version of info for LayerInfosForCopy
func (s *blobCacheSource) layerInfoForCopy(info types.BlobInfo) (types.BlobInfo, error) {
	var replaceDigestBytes []byte
	blobFile, err := s.reference.blobPath(info.Digest, false)
	if err != nil {
		return types.BlobInfo{}, err
	}
	switch s.reference.compress {
	case types.Compress:
		replaceDigestBytes, err = os.ReadFile(blobFile + compressedNote)
	case types.Decompress:
		replaceDigestBytes, err = os.ReadFile(blobFile + decompressedNote)
	}
	if err != nil {
		return info, nil
	}
	replaceDigest, err := digest.Parse(string(replaceDigestBytes))
	if err != nil {
		return info, nil
	}
	alternate, err := s.reference.blobPath(replaceDigest, false)
	if err != nil {
		return types.BlobInfo{}, err
	}
	fileInfo, err := os.Stat(alternate)
	if err != nil {
		return info, nil
	}

	switch info.MediaType {
	case v1.MediaTypeImageLayer, v1.MediaTypeImageLayerGzip:
		switch s.reference.compress {
		case types.Compress:
			info.MediaType = v1.MediaTypeImageLayerGzip
			info.CompressionAlgorithm = &compression.Gzip
		case types.Decompress: // FIXME: This should remove zstd:chunked annotations (but those annotations being left with incorrect values should not break pulls)
			info.MediaType = v1.MediaTypeImageLayer
			info.CompressionAlgorithm = nil
		}
	case manifest.DockerV2SchemaLayerMediaTypeUncompressed, manifest.DockerV2Schema2LayerMediaType:
		switch s.reference.compress {
		case types.Compress:
			info.MediaType = manifest.DockerV2Schema2LayerMediaType
			info.CompressionAlgorithm = &compression.Gzip
		case types.Decompress:
			// nope, not going to suggest anything, it's not allowed by the spec
			return info, nil
		}
	}
	logrus.Debugf("suggesting cached blob with digest %q, type %q, and compression %v in place of blob with digest %q", replaceDigest.String(), info.MediaType, s.reference.compress, info.Digest.String())
	info.CompressionOperation = s.reference.compress
	info.Digest = replaceDigest
	info.Size = fileInfo.Size()
	logrus.Debugf("info = %#v", info)
	return info, nil
}

func (s *blobCacheSource) LayerInfosForCopy(ctx context.Context, instanceDigest *digest.Digest) ([]types.BlobInfo, error) {
	signatures, err := s.source.GetSignaturesWithFormat(ctx, instanceDigest)
	if err != nil {
		return nil, fmt.Errorf("error checking if image %q has signatures: %w", transports.ImageName(s.reference), err)
	}
	canReplaceBlobs := len(signatures) == 0

	infos, err := s.source.LayerInfosForCopy(ctx, instanceDigest)
	if err != nil {
		return nil, fmt.Errorf("error getting layer infos for copying image %q through cache: %w", transports.ImageName(s.reference), err)
	}
	if infos == nil {
		img, err := image.FromUnparsedImage(ctx, &s.sys, image.UnparsedInstance(s.source, instanceDigest))
		if err != nil {
			return nil, fmt.Errorf("error opening image to get layer infos for copying image %q through cache: %w", transports.ImageName(s.reference), err)
		}
		infos = img.LayerInfos()
	}

	if canReplaceBlobs && s.reference.compress != types.PreserveOriginal {
		replacedInfos := make([]types.BlobInfo, 0, len(infos))
		for _, info := range infos {
			info, err = s.layerInfoForCopy(info)
			if err != nil {
				return nil, err
			}
			replacedInfos = append(replacedInfos, info)
		}
		infos = replacedInfos
	}

	return infos, nil
}

// SupportsGetBlobAt() returns true if GetBlobAt (BlobChunkAccessor) is supported.
func (s *blobCacheSource) SupportsGetBlobAt() bool {
	return s.source.SupportsGetBlobAt()
}

// streamChunksFromFile generates the channels returned by GetBlobAt for chunks of seekable file
func streamChunksFromFile(streams chan io.ReadCloser, errs chan error, file io.ReadSeekCloser,
	chunks []private.ImageSourceChunk) {
	defer close(streams)
	defer close(errs)
	defer file.Close()

	for _, c := range chunks {
		// Always seek to the desired offset; that way we don’t need to care about the consumer
		// not reading all of the chunk, or about the position going backwards.
		if _, err := file.Seek(int64(c.Offset), io.SeekStart); err != nil {
			errs <- err
			break
		}
		var stream io.Reader
		if c.Length != math.MaxUint64 {
			stream = io.LimitReader(file, int64(c.Length))
		} else {
			stream = file
		}
		s := signalCloseReader{
			closed: make(chan struct{}),
			stream: stream,
		}
		streams <- s

		// Wait until the stream is closed before going to the next chunk
		<-s.closed
	}
}

type signalCloseReader struct {
	closed chan struct{}
	stream io.Reader
}

func (s signalCloseReader) Read(p []byte) (int, error) {
	return s.stream.Read(p)
}

func (s signalCloseReader) Close() error {
	close(s.closed)
	return nil
}

// GetBlobAt returns a sequential channel of readers that contain data for the requested
// blob chunks, and a channel that might get a single error value.
// The specified chunks must be not overlapping and sorted by their offset.
// The readers must be fully consumed, in the order they are returned, before blocking
// to read the next chunk.
// If the Length for the last chunk is set to math.MaxUint64, then it
// fully fetches the remaining data from the offset to the end of the blob.
func (s *blobCacheSource) GetBlobAt(ctx context.Context, info types.BlobInfo, chunks []private.ImageSourceChunk) (chan io.ReadCloser, chan error, error) {
	blobPath, _, _, err := s.reference.findBlob(info)
	if err != nil {
		return nil, nil, err
	}
	if blobPath != "" {
		f, err := os.Open(blobPath)
		if err == nil {
			s.mu.Lock()
			s.cacheHits++
			s.mu.Unlock()
			streams := make(chan io.ReadCloser)
			errs := make(chan error)
			go streamChunksFromFile(streams, errs, f, chunks)
			return streams, errs, nil
		}
		if !os.IsNotExist(err) {
			s.mu.Lock()
			s.cacheErrors++
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("checking for cache: %w", err)
		}
	}
	s.mu.Lock()
	s.cacheMisses++
	s.mu.Unlock()
	streams, errs, err := s.source.GetBlobAt(ctx, info, chunks)
	if err != nil {
		return streams, errs, fmt.Errorf("error reading blob chunks from source image %q: %w", transports.ImageName(s.reference), err)
	}
	return streams, errs, nil
}
