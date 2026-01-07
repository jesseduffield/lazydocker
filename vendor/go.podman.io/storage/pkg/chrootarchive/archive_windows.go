package chrootarchive

import (
	"io"

	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/longpath"
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

// chroot is not supported by Windows
func chroot(path string) error {
	return nil
}

func invokeUnpack(decompressedArchive io.Reader,
	dest *unpackDestination,
	options *archive.TarOptions,
) error {
	// Windows is different to Linux here because Windows does not support
	// chroot. Hence there is no point sandboxing a chrooted process to
	// do the unpack. We call inline instead within the daemon process.
	return archive.Unpack(decompressedArchive, longpath.AddPrefix(dest.dest), options)
}

func invokePack(srcPath string, options *archive.TarOptions, root string) (io.ReadCloser, error) {
	// Windows is different to Linux here because Windows does not support
	// chroot. Hence there is no point sandboxing a chrooted process to
	// do the pack. We call inline instead within the daemon process.
	return archive.TarWithOptions(srcPath, options)
}
