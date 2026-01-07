package blobcache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	digest "github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

const (
	compressedNote   = ".compressed"
	decompressedNote = ".decompressed"
)

// BlobCache is an object which saves copies of blobs that are written to it while passing them
// through to some real destination, and which can be queried directly in order to read them
// back.
//
// Implements types.ImageReference.
type BlobCache struct {
	reference types.ImageReference
	// WARNING: The contents of this directory may be accessed concurrently,
	// both within this process and by multiple different processes
	directory string
	compress  types.LayerCompression
}

// NewBlobCache creates a new blob cache that wraps an image reference.  Any blobs which are
// written to the destination image created from the resulting reference will also be stored
// as-is to the specified directory or a temporary directory.
// The compress argument controls whether or not the cache will try to substitute a compressed
// or different version of a blob when preparing the list of layers when reading an image.
func NewBlobCache(ref types.ImageReference, directory string, compress types.LayerCompression) (*BlobCache, error) {
	if directory == "" {
		return nil, fmt.Errorf("error creating cache around reference %q: no directory specified", transports.ImageName(ref))
	}
	switch compress {
	case types.Compress, types.Decompress, types.PreserveOriginal:
		// valid value, accept it
	default:
		return nil, fmt.Errorf("unhandled LayerCompression value %v", compress)
	}
	return &BlobCache{
		reference: ref,
		directory: directory,
		compress:  compress,
	}, nil
}

func (b *BlobCache) Transport() types.ImageTransport {
	return b.reference.Transport()
}

func (b *BlobCache) StringWithinTransport() string {
	return b.reference.StringWithinTransport()
}

func (b *BlobCache) DockerReference() reference.Named {
	return b.reference.DockerReference()
}

func (b *BlobCache) PolicyConfigurationIdentity() string {
	return b.reference.PolicyConfigurationIdentity()
}

func (b *BlobCache) PolicyConfigurationNamespaces() []string {
	return b.reference.PolicyConfigurationNamespaces()
}

func (b *BlobCache) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	return b.reference.DeleteImage(ctx, sys)
}

// blobPath returns the path appropriate for storing a blob with digest.
func (b *BlobCache) blobPath(digest digest.Digest, isConfig bool) (string, error) {
	if err := digest.Validate(); err != nil { // Make sure digest.String() does not contain any unexpected characters
		return "", err
	}
	baseName := digest.String()
	if isConfig {
		baseName += ".config"
	}
	return filepath.Join(b.directory, baseName), nil
}

// findBlob checks if we have a blob for info in cache (whether a config or not)
// and if so, returns it path and size, and whether it was stored as a config.
// It returns ("", -1, nil) if the blob is not
func (b *BlobCache) findBlob(info types.BlobInfo) (string, int64, bool, error) {
	if info.Digest == "" {
		return "", -1, false, nil
	}

	for _, isConfig := range []bool{false, true} {
		path, err := b.blobPath(info.Digest, isConfig)
		if err != nil {
			return "", -1, false, err
		}
		fileInfo, err := os.Stat(path)
		if err == nil && (info.Size == -1 || info.Size == fileInfo.Size()) {
			return path, fileInfo.Size(), isConfig, nil
		}
		if !os.IsNotExist(err) {
			return "", -1, false, fmt.Errorf("checking size: %w", err)
		}
	}

	return "", -1, false, nil

}

func (b *BlobCache) HasBlob(blobinfo types.BlobInfo) (bool, int64, error) {
	path, size, _, err := b.findBlob(blobinfo)
	if err != nil {
		return false, -1, err
	}
	if path != "" {
		return true, size, nil
	}
	return false, -1, nil
}

func (b *BlobCache) Directory() string {
	return b.directory
}

func (b *BlobCache) ClearCache() error {
	f, err := os.Open(b.directory)
	if err != nil {
		return err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("error reading directory %q: %w", b.directory, err)
	}
	for _, name := range names {
		pathname := filepath.Join(b.directory, name)
		if err = os.RemoveAll(pathname); err != nil {
			return fmt.Errorf("clearing cache for %q: %w", transports.ImageName(b), err)
		}
	}
	return nil
}

func (b *BlobCache) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return image.FromReference(ctx, sys, b)
}
