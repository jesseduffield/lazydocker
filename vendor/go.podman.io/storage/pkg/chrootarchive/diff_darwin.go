package chrootarchive

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.podman.io/storage/pkg/archive"
)

// applyLayerHandler parses a diff in the standard layer format from `layer`, and
// applies it to the directory `dest`. Returns the size in bytes of the
// contents of the layer.
func applyLayerHandler(dest string, layer io.Reader, options *archive.TarOptions, decompress bool) (size int64, err error) {
	dest = filepath.Clean(dest)

	if decompress {
		decompressed, err := archive.DecompressStream(layer)
		if err != nil {
			return 0, err
		}
		defer decompressed.Close()

		layer = decompressed
	}

	tmpDir, err := os.MkdirTemp(os.Getenv("temp"), "temp-storage-extract")
	if err != nil {
		return 0, fmt.Errorf("ApplyLayer failed to create temp-storage-extract under %s: %w", dest, err)
	}

	s, err := archive.UnpackLayer(dest, layer, options)
	os.RemoveAll(tmpDir)
	if err != nil {
		return 0, fmt.Errorf("ApplyLayer %s failed UnpackLayer to %s: %w", layer, dest, err)
	}

	return s, nil
}
