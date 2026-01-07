package chunked

import (
	"io"

	"go.podman.io/storage/pkg/chunked/compressor"
	"go.podman.io/storage/pkg/chunked/internal/minimal"
)

const (
	TypeReg     = minimal.TypeReg
	TypeChunk   = minimal.TypeChunk
	TypeLink    = minimal.TypeLink
	TypeChar    = minimal.TypeChar
	TypeBlock   = minimal.TypeBlock
	TypeDir     = minimal.TypeDir
	TypeFifo    = minimal.TypeFifo
	TypeSymlink = minimal.TypeSymlink
)

// ZstdCompressor is a CompressorFunc for the zstd compression algorithm.
// Deprecated: Use pkg/chunked/compressor.ZstdCompressor.
func ZstdCompressor(r io.Writer, metadata map[string]string, level *int) (io.WriteCloser, error) {
	return compressor.ZstdCompressor(r, metadata, level)
}
