package streamdigest

import (
	"fmt"
	"io"
	"os"

	"go.podman.io/image/v5/internal/putblobdigest"
	"go.podman.io/image/v5/internal/tmpdir"
	"go.podman.io/image/v5/types"
)

// ComputeBlobInfo streams a blob to a temporary file and populates Digest and Size in inputInfo.
// The temporary file is returned as an io.Reader along with a cleanup function.
// It is the caller's responsibility to call the cleanup function, which closes and removes the temporary file.
// If an error occurs, inputInfo is not modified.
func ComputeBlobInfo(sys *types.SystemContext, stream io.Reader, inputInfo *types.BlobInfo) (io.Reader, func(), error) {
	diskBlob, err := tmpdir.CreateBigFileTemp(sys, "stream-blob")
	if err != nil {
		return nil, nil, fmt.Errorf("creating temporary on-disk layer: %w", err)
	}
	cleanup := func() {
		diskBlob.Close()
		os.Remove(diskBlob.Name())
	}
	digester, stream := putblobdigest.DigestIfCanonicalUnknown(stream, *inputInfo)
	written, err := io.Copy(diskBlob, stream)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("writing to temporary on-disk layer: %w", err)
	}
	_, err = diskBlob.Seek(0, io.SeekStart)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("rewinding temporary on-disk layer: %w", err)
	}
	inputInfo.Digest = digester.Digest()
	inputInfo.Size = written
	return diskBlob, cleanup, nil
}
