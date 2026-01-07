package chrootarchive

import (
	"io"

	"go.podman.io/storage/pkg/archive"
)

type unpackDestination struct {
	dest string
}

func (dst *unpackDestination) Close() error {
	return nil
}

// newUnpackDestination is a no-op on this platform
func newUnpackDestination(root, dest string) (*unpackDestination, error) {
	return &unpackDestination{
		dest: dest,
	}, nil
}

func invokeUnpack(decompressedArchive io.Reader,
	dest *unpackDestination,
	options *archive.TarOptions,
) error {
	return archive.Unpack(decompressedArchive, dest.dest, options)
}

func invokePack(srcPath string, options *archive.TarOptions, root string) (io.ReadCloser, error) {
	_ = root // Restricting the operation to this root is not implemented on macOS
	return archive.TarWithOptions(srcPath, options)
}
