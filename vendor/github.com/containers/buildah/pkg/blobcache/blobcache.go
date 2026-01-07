package blobcache

import (
	imageBlobCache "go.podman.io/image/v5/pkg/blobcache"
	"go.podman.io/image/v5/types"
)

// BlobCache is an object which saves copies of blobs that are written to it while passing them
// through to some real destination, and which can be queried directly in order to read them
// back.
type BlobCache interface {
	types.ImageReference
	// HasBlob checks if a blob that matches the passed-in digest (and
	// size, if not -1), is present in the cache.
	HasBlob(types.BlobInfo) (bool, int64, error)
	// Directories returns the list of cache directories.
	Directory() string
	// ClearCache() clears the contents of the cache directories.  Note
	// that this also clears content which was not placed there by this
	// cache implementation.
	ClearCache() error
}

// NewBlobCache creates a new blob cache that wraps an image reference.  Any blobs which are
// written to the destination image created from the resulting reference will also be stored
// as-is to the specified directory or a temporary directory.
// The compress argument controls whether or not the cache will try to substitute a compressed
// or different version of a blob when preparing the list of layers when reading an image.
func NewBlobCache(ref types.ImageReference, directory string, compress types.LayerCompression) (BlobCache, error) {
	return imageBlobCache.NewBlobCache(ref, directory, compress)
}
