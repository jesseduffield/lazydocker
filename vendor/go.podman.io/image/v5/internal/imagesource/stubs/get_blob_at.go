package stubs

import (
	"context"
	"fmt"
	"io"

	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/types"
)

// NoGetBlobAtInitialize implements parts of private.ImageSource
// for transports that don’t support GetBlobAt().
// See NoGetBlobAt() below.
type NoGetBlobAtInitialize struct {
	transportName string
}

// NoGetBlobAt() creates a NoGetBlobAtInitialize for ref.
func NoGetBlobAt(ref types.ImageReference) NoGetBlobAtInitialize {
	return NoGetBlobAtRaw(ref.Transport().Name())
}

// NoGetBlobAtRaw is the same thing as NoGetBlobAt, but it can be used
// in situations where no ImageReference is available.
func NoGetBlobAtRaw(transportName string) NoGetBlobAtInitialize {
	return NoGetBlobAtInitialize{
		transportName: transportName,
	}
}

// SupportsGetBlobAt() returns true if GetBlobAt (BlobChunkAccessor) is supported.
func (stub NoGetBlobAtInitialize) SupportsGetBlobAt() bool {
	return false
}

// GetBlobAt returns a sequential channel of readers that contain data for the requested
// blob chunks, and a channel that might get a single error value.
// The specified chunks must be not overlapping and sorted by their offset.
// The readers must be fully consumed, in the order they are returned, before blocking
// to read the next chunk.
// If the Length for the last chunk is set to math.MaxUint64, then it
// fully fetches the remaining data from the offset to the end of the blob.
func (stub NoGetBlobAtInitialize) GetBlobAt(ctx context.Context, info types.BlobInfo, chunks []private.ImageSourceChunk) (chan io.ReadCloser, chan error, error) {
	return nil, nil, fmt.Errorf("internal error: GetBlobAt is not supported by the %q transport", stub.transportName)
}

// ImplementsGetBlobAt implements SupportsGetBlobAt() that returns true.
type ImplementsGetBlobAt struct{}

// SupportsGetBlobAt() returns true if GetBlobAt (BlobChunkAccessor) is supported.
func (stub ImplementsGetBlobAt) SupportsGetBlobAt() bool {
	return true
}
