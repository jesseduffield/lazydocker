package copy

import (
	"errors"
	"fmt"
	"io"
	"maps"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	internalblobinfocache "go.podman.io/image/v5/internal/blobinfocache"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
	chunkedToc "go.podman.io/storage/pkg/chunked/toc"
)

var (
	// defaultCompressionFormat is used if the destination transport requests
	// compression, and the user does not explicitly instruct us to use an algorithm.
	defaultCompressionFormat = &compression.Gzip

	// compressionBufferSize is the buffer size used to compress a blob
	compressionBufferSize = 1048576

	// expectedBaseCompressionFormats is used to check if a blob with a specified media type is compressed
	// using the algorithm that the media type says it should be compressed with
	expectedBaseCompressionFormats = map[string]*compressiontypes.Algorithm{
		imgspecv1.MediaTypeImageLayerGzip:         &compression.Gzip,
		imgspecv1.MediaTypeImageLayerZstd:         &compression.Zstd,
		manifest.DockerV2Schema2LayerMediaType:    &compression.Gzip,
		manifest.DockerV2SchemaLayerMediaTypeZstd: &compression.Zstd,
	}
)

// bpDetectCompressionStepData contains data that the copy pipeline needs about the “detect compression” step.
type bpDetectCompressionStepData struct {
	isCompressed                 bool
	format                       compressiontypes.Algorithm        // Valid if isCompressed
	decompressor                 compressiontypes.DecompressorFunc // Valid if isCompressed
	srcCompressorBaseVariantName string                            // Compressor name to possibly record in the blob info cache for the source blob.
}

// blobPipelineDetectCompressionStep updates *stream to detect its current compression format.
// srcInfo is only used for error messages.
// Returns data for other steps.
func blobPipelineDetectCompressionStep(stream *sourceStream, srcInfo types.BlobInfo) (bpDetectCompressionStepData, error) {
	// This requires us to “peek ahead” into the stream to read the initial part, which requires us to chain through another io.Reader returned by DetectCompression.
	format, decompressor, reader, err := compression.DetectCompressionFormat(stream.reader) // We could skip this in some cases, but let's keep the code path uniform
	if err != nil {
		return bpDetectCompressionStepData{}, fmt.Errorf("reading blob %s: %w", srcInfo.Digest, err)
	}
	stream.reader = reader

	if decompressor != nil && format.Name() == compressiontypes.ZstdAlgorithmName {
		tocDigest, err := chunkedToc.GetTOCDigest(srcInfo.Annotations)
		if err != nil {
			return bpDetectCompressionStepData{}, err
		}
		if tocDigest != nil {
			format = compression.ZstdChunked
		}

	}
	res := bpDetectCompressionStepData{
		isCompressed: decompressor != nil,
		format:       format,
		decompressor: decompressor,
	}
	if res.isCompressed {
		res.srcCompressorBaseVariantName = format.BaseVariantName()
	} else {
		res.srcCompressorBaseVariantName = internalblobinfocache.Uncompressed
	}

	if expectedBaseFormat, known := expectedBaseCompressionFormats[stream.info.MediaType]; known && res.isCompressed && format.BaseVariantName() != expectedBaseFormat.Name() {
		logrus.Debugf("blob %s with type %s should be compressed with %s, but compressor appears to be %s", srcInfo.Digest.String(), srcInfo.MediaType, expectedBaseFormat.Name(), format.Name())
	}
	return res, nil
}

// bpCompressionStepData contains data that the copy pipeline needs about the compression step.
type bpCompressionStepData struct {
	operation                             bpcOperation                // What we are actually doing
	uploadedOperation                     types.LayerCompression      // Operation to use for updating the blob metadata (matching the end state, not necessarily what we do)
	uploadedAlgorithm                     *compressiontypes.Algorithm // An algorithm parameter for the compressionOperation edits.
	uploadedAnnotations                   map[string]string           // Compression-related annotations that should be set on the uploaded blob. WARNING: This is only set after the srcStream.reader is fully consumed.
	srcCompressorBaseVariantName          string                      // Compressor base variant name to record in the blob info cache for the source blob.
	uploadedCompressorBaseVariantName     string                      // Compressor base variant name to record in the blob info cache for the uploaded blob.
	uploadedCompressorSpecificVariantName string                      // Compressor specific variant name to record in the blob info cache for the uploaded blob.
	closers                               []io.Closer                 // Objects to close after the upload is done, if any.
}

type bpcOperation int

const (
	bpcOpInvalid              bpcOperation = iota
	bpcOpPreserveOpaque                    // We are preserving something where compression is not applicable
	bpcOpPreserveCompressed                // We are preserving a compressed, and decompressible, layer
	bpcOpPreserveUncompressed              // We are preserving an uncompressed, and compressible, layer
	bpcOpCompressUncompressed              // We are compressing uncompressed data
	bpcOpRecompressCompressed              // We are recompressing compressed data
	bpcOpDecompressCompressed              // We are decompressing compressed data
)

// blobPipelineCompressionStep updates *stream to compress and/or decompress it.
// srcInfo is primarily used for error messages.
// Returns data for other steps; the caller should eventually call updateCompressionEdits and perhaps recordValidatedBlobData,
// and must eventually call close.
func (ic *imageCopier) blobPipelineCompressionStep(stream *sourceStream, canModifyBlob bool, srcInfo types.BlobInfo,
	detected bpDetectCompressionStepData) (*bpCompressionStepData, error) {
	// WARNING: If you are adding new reasons to change the blob, update also the OptimizeDestinationImageAlreadyExists
	// short-circuit conditions
	layerCompressionChangeSupported := ic.src.CanChangeLayerCompression(stream.info.MediaType)
	if !layerCompressionChangeSupported {
		logrus.Debugf("Compression change for blob %s (%q) not supported", srcInfo.Digest, stream.info.MediaType)
	}
	if canModifyBlob && layerCompressionChangeSupported {
		for _, fn := range []func(*sourceStream, bpDetectCompressionStepData) (*bpCompressionStepData, error){
			ic.bpcPreserveEncrypted,
			ic.bpcCompressUncompressed,
			ic.bpcRecompressCompressed,
			ic.bpcDecompressCompressed,
		} {
			res, err := fn(stream, detected)
			if err != nil {
				return nil, err
			}
			if res != nil {
				return res, nil
			}
		}
	}
	return ic.bpcPreserveOriginal(stream, detected, layerCompressionChangeSupported), nil
}

// bpcPreserveEncrypted checks if the input is encrypted, and returns a *bpCompressionStepData if so.
func (ic *imageCopier) bpcPreserveEncrypted(stream *sourceStream, _ bpDetectCompressionStepData) (*bpCompressionStepData, error) {
	if isOciEncrypted(stream.info.MediaType) {
		// We can’t do anything with an encrypted blob unless decrypted.
		logrus.Debugf("Using original blob without modification for encrypted blob")
		return &bpCompressionStepData{
			operation:                             bpcOpPreserveOpaque,
			uploadedOperation:                     types.PreserveOriginal,
			uploadedAlgorithm:                     nil,
			srcCompressorBaseVariantName:          internalblobinfocache.UnknownCompression,
			uploadedCompressorBaseVariantName:     internalblobinfocache.UnknownCompression,
			uploadedCompressorSpecificVariantName: internalblobinfocache.UnknownCompression,
		}, nil
	}
	return nil, nil
}

// bpcCompressUncompressed checks if we should be compressing an uncompressed input, and returns a *bpCompressionStepData if so.
func (ic *imageCopier) bpcCompressUncompressed(stream *sourceStream, detected bpDetectCompressionStepData) (*bpCompressionStepData, error) {
	if ic.c.dest.DesiredLayerCompression() == types.Compress && !detected.isCompressed {
		logrus.Debugf("Compressing blob on the fly")
		var uploadedAlgorithm *compressiontypes.Algorithm
		if ic.compressionFormat != nil {
			uploadedAlgorithm = ic.compressionFormat
		} else {
			uploadedAlgorithm = defaultCompressionFormat
		}

		reader, annotations := ic.compressedStream(stream.reader, *uploadedAlgorithm)
		// Note: reader must be closed on all return paths.
		stream.reader = reader
		stream.info = types.BlobInfo{ // FIXME? Should we preserve more data in src.info?
			Digest: "",
			Size:   -1,
		}
		specificVariantName := uploadedAlgorithm.Name()
		if specificVariantName == uploadedAlgorithm.BaseVariantName() {
			specificVariantName = internalblobinfocache.UnknownCompression
		}
		return &bpCompressionStepData{
			operation:                             bpcOpCompressUncompressed,
			uploadedOperation:                     types.Compress,
			uploadedAlgorithm:                     uploadedAlgorithm,
			uploadedAnnotations:                   annotations,
			srcCompressorBaseVariantName:          detected.srcCompressorBaseVariantName,
			uploadedCompressorBaseVariantName:     uploadedAlgorithm.BaseVariantName(),
			uploadedCompressorSpecificVariantName: specificVariantName,
			closers:                               []io.Closer{reader},
		}, nil
	}
	return nil, nil
}

// bpcRecompressCompressed checks if we should be recompressing a compressed input to another format, and returns a *bpCompressionStepData if so.
func (ic *imageCopier) bpcRecompressCompressed(stream *sourceStream, detected bpDetectCompressionStepData) (*bpCompressionStepData, error) {
	if ic.c.dest.DesiredLayerCompression() == types.Compress && detected.isCompressed &&
		ic.compressionFormat != nil &&
		(ic.compressionFormat.Name() != detected.format.Name() && ic.compressionFormat.Name() != detected.format.BaseVariantName()) {
		// When the blob is compressed, but the desired format is different, it first needs to be decompressed and finally
		// re-compressed using the desired format.
		logrus.Debugf("Blob will be converted")

		decompressed, err := detected.decompressor(stream.reader)
		if err != nil {
			return nil, err
		}
		succeeded := false
		defer func() {
			if !succeeded {
				decompressed.Close()
			}
		}()

		recompressed, annotations := ic.compressedStream(decompressed, *ic.compressionFormat)
		// Note: recompressed must be closed on all return paths.
		stream.reader = recompressed
		stream.info = types.BlobInfo{ // FIXME? Should we preserve more data in src.info? Notably the current approach correctly removes zstd:chunked metadata annotations.
			Digest: "",
			Size:   -1,
		}
		specificVariantName := ic.compressionFormat.Name()
		if specificVariantName == ic.compressionFormat.BaseVariantName() {
			specificVariantName = internalblobinfocache.UnknownCompression
		}
		succeeded = true
		return &bpCompressionStepData{
			operation:                             bpcOpRecompressCompressed,
			uploadedOperation:                     types.PreserveOriginal,
			uploadedAlgorithm:                     ic.compressionFormat,
			uploadedAnnotations:                   annotations,
			srcCompressorBaseVariantName:          detected.srcCompressorBaseVariantName,
			uploadedCompressorBaseVariantName:     ic.compressionFormat.BaseVariantName(),
			uploadedCompressorSpecificVariantName: specificVariantName,
			closers:                               []io.Closer{decompressed, recompressed},
		}, nil
	}
	return nil, nil
}

// bpcDecompressCompressed checks if we should be decompressing a compressed input, and returns a *bpCompressionStepData if so.
func (ic *imageCopier) bpcDecompressCompressed(stream *sourceStream, detected bpDetectCompressionStepData) (*bpCompressionStepData, error) {
	if ic.c.dest.DesiredLayerCompression() == types.Decompress && detected.isCompressed {
		logrus.Debugf("Blob will be decompressed")
		s, err := detected.decompressor(stream.reader)
		if err != nil {
			return nil, err
		}
		// Note: s must be closed on all return paths.
		stream.reader = s
		stream.info = types.BlobInfo{ // FIXME? Should we preserve more data in src.info? Notably the current approach correctly removes zstd:chunked metadata annotations.
			Digest: "",
			Size:   -1,
		}
		return &bpCompressionStepData{
			operation:                             bpcOpDecompressCompressed,
			uploadedOperation:                     types.Decompress,
			uploadedAlgorithm:                     nil,
			srcCompressorBaseVariantName:          detected.srcCompressorBaseVariantName,
			uploadedCompressorBaseVariantName:     internalblobinfocache.Uncompressed,
			uploadedCompressorSpecificVariantName: internalblobinfocache.UnknownCompression,
			closers:                               []io.Closer{s},
		}, nil
	}
	return nil, nil
}

// bpcPreserveOriginal returns a *bpCompressionStepData for not changing the original blob.
// This does not change the sourceStream parameter; we include it for symmetry with other
// pipeline steps.
func (ic *imageCopier) bpcPreserveOriginal(_ *sourceStream, detected bpDetectCompressionStepData,
	layerCompressionChangeSupported bool) *bpCompressionStepData {
	logrus.Debugf("Using original blob without modification")
	// Remember if the original blob was compressed, and if so how, so that if
	// LayerInfosForCopy() returned something that differs from what was in the
	// source's manifest, and UpdatedImage() needs to call UpdateLayerInfos(),
	// it will be able to correctly derive the MediaType for the copied blob.
	//
	// But don’t touch blobs in objects where we can’t change compression,
	// so that src.UpdatedImage() doesn’t fail; assume that for such blobs
	// LayerInfosForCopy() should not be making any changes in the first place.
	var bpcOp bpcOperation
	var uploadedOp types.LayerCompression
	var algorithm *compressiontypes.Algorithm
	switch {
	case !layerCompressionChangeSupported:
		bpcOp = bpcOpPreserveOpaque
		uploadedOp = types.PreserveOriginal
		algorithm = nil
	case detected.isCompressed:
		bpcOp = bpcOpPreserveCompressed
		uploadedOp = types.PreserveOriginal
		algorithm = &detected.format
	default:
		bpcOp = bpcOpPreserveUncompressed
		uploadedOp = types.Decompress
		algorithm = nil
	}
	return &bpCompressionStepData{
		operation:                    bpcOp,
		uploadedOperation:            uploadedOp,
		uploadedAlgorithm:            algorithm,
		srcCompressorBaseVariantName: detected.srcCompressorBaseVariantName,
		// We only record the base variant of the format on upload; we didn’t do anything with
		// the TOC, we don’t know whether it matches the blob digest, so we don’t want to trigger
		// reuse of any kind between the blob digest and the TOC digest.
		uploadedCompressorBaseVariantName:     detected.srcCompressorBaseVariantName,
		uploadedCompressorSpecificVariantName: internalblobinfocache.UnknownCompression,
	}
}

// updateCompressionEdits sets *operation, *algorithm and updates *annotations, if necessary.
func (d *bpCompressionStepData) updateCompressionEdits(operation *types.LayerCompression, algorithm **compressiontypes.Algorithm, annotations *map[string]string) {
	*operation = d.uploadedOperation
	// If we can modify the layer's blob, set the desired algorithm for it to be set in the manifest.
	*algorithm = d.uploadedAlgorithm
	if *annotations == nil {
		*annotations = map[string]string{}
	}
	maps.Copy(*annotations, d.uploadedAnnotations)
}

// recordValidatedDigestData updates b.blobInfoCache with data about the created uploadedInfo (as returned by PutBlob)
// and the original srcInfo (which the caller guarantees has been validated).
// This must ONLY be called if all data has been validated by OUR code, and is not coming from third parties.
func (d *bpCompressionStepData) recordValidatedDigestData(c *copier, uploadedInfo types.BlobInfo, srcInfo types.BlobInfo,
	encryptionStep *bpEncryptionStepData, decryptionStep *bpDecryptionStepData) error {
	// Don’t record any associations that involve encrypted data. This is a bit crude,
	// some blob substitutions (replacing pulls of encrypted data with local reuse of known decryption outcomes)
	// might be safe, but it’s not trivially obvious, so let’s be conservative for now.
	// This crude approach also means we don’t need to record whether a blob is encrypted
	// in the blob info cache (which would probably be necessary for any more complex logic),
	// and the simplicity is attractive.
	if !encryptionStep.encrypting && !decryptionStep.decrypting {
		// If d.operation != bpcOpPreserve*, we now have two reliable digest values:
		// srcinfo.Digest describes the pre-d.operation input, verified by digestingReader
		// uploadedInfo.Digest describes the post-d.operation output, computed by PutBlob
		// (because we set stream.info.Digest == "", this must have been computed afresh).
		switch d.operation {
		case bpcOpPreserveOpaque:
			// No useful information
		case bpcOpCompressUncompressed:
			c.blobInfoCache.RecordDigestUncompressedPair(uploadedInfo.Digest, srcInfo.Digest)
			if d.uploadedAnnotations != nil {
				tocDigest, err := chunkedToc.GetTOCDigest(d.uploadedAnnotations)
				if err != nil {
					return fmt.Errorf("parsing just-created compression annotations: %w", err)
				}
				if tocDigest != nil {
					c.blobInfoCache.RecordTOCUncompressedPair(*tocDigest, srcInfo.Digest)
				}
			}
		case bpcOpDecompressCompressed:
			c.blobInfoCache.RecordDigestUncompressedPair(srcInfo.Digest, uploadedInfo.Digest)
		case bpcOpRecompressCompressed, bpcOpPreserveCompressed:
			// We know one or two compressed digests. BlobInfoCache associates compression variants via the uncompressed digest,
			// and we don’t know that one.
			// That also means that repeated copies with the same recompression don’t identify reuse opportunities (unless
			// RecordDigestUncompressedPair was called for both compressed variants for some other reason).
		case bpcOpPreserveUncompressed:
			c.blobInfoCache.RecordDigestUncompressedPair(srcInfo.Digest, srcInfo.Digest)
		case bpcOpInvalid:
			fallthrough
		default:
			return fmt.Errorf("Internal error: Unexpected d.operation value %#v", d.operation)
		}
	}
	if d.srcCompressorBaseVariantName == "" || d.uploadedCompressorBaseVariantName == "" || d.uploadedCompressorSpecificVariantName == "" {
		return fmt.Errorf("internal error: missing compressor names (src base: %q, uploaded base: %q, uploaded specific: %q)",
			d.srcCompressorBaseVariantName, d.uploadedCompressorBaseVariantName, d.uploadedCompressorSpecificVariantName)
	}
	if d.uploadedCompressorBaseVariantName != internalblobinfocache.UnknownCompression {
		c.blobInfoCache.RecordDigestCompressorData(uploadedInfo.Digest, internalblobinfocache.DigestCompressorData{
			BaseVariantCompressor:      d.uploadedCompressorBaseVariantName,
			SpecificVariantCompressor:  d.uploadedCompressorSpecificVariantName,
			SpecificVariantAnnotations: d.uploadedAnnotations,
		})
	}
	if srcInfo.Digest != "" && srcInfo.Digest != uploadedInfo.Digest &&
		d.srcCompressorBaseVariantName != internalblobinfocache.UnknownCompression {
		// If the source is already using some TOC-dependent variant, we either copied the
		// blob as is, or perhaps decompressed it; either way we don’t trust the TOC digest,
		// so record neither the variant name, nor the TOC digest.
		c.blobInfoCache.RecordDigestCompressorData(srcInfo.Digest, internalblobinfocache.DigestCompressorData{
			BaseVariantCompressor:      d.srcCompressorBaseVariantName,
			SpecificVariantCompressor:  internalblobinfocache.UnknownCompression,
			SpecificVariantAnnotations: nil,
		})
	}
	return nil
}

// close closes objects that carry state throughout the compression/decompression operation.
func (d *bpCompressionStepData) close() {
	for _, c := range d.closers {
		c.Close()
	}
}

// doCompression reads all input from src and writes its compressed equivalent to dest.
func doCompression(dest io.Writer, src io.Reader, metadata map[string]string, compressionFormat compressiontypes.Algorithm, compressionLevel *int) error {
	compressor, err := compression.CompressStreamWithMetadata(dest, metadata, compressionFormat, compressionLevel)
	if err != nil {
		return err
	}

	buf := make([]byte, compressionBufferSize)

	_, err = io.CopyBuffer(compressor, src, buf) // Sets err to nil, i.e. causes dest.Close()
	if err != nil {
		compressor.Close()
		return err
	}

	return compressor.Close()
}

// compressGoroutine reads all input from src and writes its compressed equivalent to dest.
func (ic *imageCopier) compressGoroutine(dest *io.PipeWriter, src io.Reader, metadata map[string]string, compressionFormat compressiontypes.Algorithm) {
	err := errors.New("Internal error: unexpected panic in compressGoroutine")
	defer func() { // Note that this is not the same as {defer dest.CloseWithError(err)}; we need err to be evaluated lazily.
		_ = dest.CloseWithError(err) // CloseWithError(nil) is equivalent to Close(), always returns nil
	}()

	err = doCompression(dest, src, metadata, compressionFormat, ic.compressionLevel)
}

// compressedStream returns a stream the input reader compressed using format, and a metadata map.
// The caller must close the returned reader.
// AFTER the stream is consumed, metadata will be updated with annotations to use on the data.
func (ic *imageCopier) compressedStream(reader io.Reader, algorithm compressiontypes.Algorithm) (io.ReadCloser, map[string]string) {
	pipeReader, pipeWriter := io.Pipe()
	annotations := map[string]string{}
	// If this fails while writing data, it will do pipeWriter.CloseWithError(); if it fails otherwise,
	// e.g. because we have exited and due to pipeReader.Close() above further writing to the pipe has failed,
	// we don’t care.
	go ic.compressGoroutine(pipeWriter, reader, annotations, algorithm) // Closes pipeWriter
	return pipeReader, annotations
}
