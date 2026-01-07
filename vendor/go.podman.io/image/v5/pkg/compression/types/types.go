package types

import (
	"go.podman.io/image/v5/pkg/compression/internal"
)

// DecompressorFunc returns the decompressed stream, given a compressed stream.
// The caller must call Close() on the decompressed stream (even if the compressed input stream does not need closing!).
type DecompressorFunc = internal.DecompressorFunc

// Algorithm is a compression algorithm provided and supported by pkg/compression.
// It can’t be supplied from the outside.
type Algorithm = internal.Algorithm

const (
	// GzipAlgorithmName is the name used by pkg/compression.Gzip.
	// NOTE: Importing only this /types package does not inherently guarantee a Gzip algorithm
	// will actually be available. (In fact it is intended for this types package not to depend
	// on any of the implementations.)
	GzipAlgorithmName = "gzip"
	// Bzip2AlgorithmName is the name used by pkg/compression.Bzip2.
	// NOTE: Importing only this /types package does not inherently guarantee a Bzip2 algorithm
	// will actually be available. (In fact it is intended for this types package not to depend
	// on any of the implementations.)
	Bzip2AlgorithmName = "bzip2"
	// XzAlgorithmName is the name used by pkg/compression.Xz.
	// NOTE: Importing only this /types package does not inherently guarantee a Xz algorithm
	// will actually be available. (In fact it is intended for this types package not to depend
	// on any of the implementations.)
	XzAlgorithmName = "Xz"
	// ZstdAlgorithmName is the name used by pkg/compression.Zstd.
	// NOTE: Importing only this /types package does not inherently guarantee a Zstd algorithm
	// will actually be available. (In fact it is intended for this types package not to depend
	// on any of the implementations.)
	ZstdAlgorithmName = "zstd"
	// ZstdChunkedAlgorithmName is the name used by pkg/compression.ZstdChunked.
	// NOTE: Importing only this /types package does not inherently guarantee a ZstdChunked algorithm
	// will actually be available. (In fact it is intended for this types package not to depend
	// on any of the implementations.)
	ZstdChunkedAlgorithmName = "zstd:chunked"
)
