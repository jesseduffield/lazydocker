package overlay

import (
	//"fmt"
	//"os"
	//"path/filepath"
	//"strings"
	//"syscall"
	"errors"

	//"go.podman.io/storage/pkg/unshare"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// MountWithOptions returns a specs.Mount which makes the contents of ${source}
// visible at ${dest} in the container.
// Options allows the caller to configure whether or not the mount should be
// read-only.
// This API is used by podman.
func MountWithOptions(contentDir, source, dest string, opts *Options) (mount specs.Mount, Err error) {
	if opts == nil {
		opts = &Options{}
	}
	if opts.ReadOnly {
		// Read-only overlay mounts can be simulated with nullfs
		mount.Source = source
		mount.Destination = dest
		mount.Type = "nullfs"
		mount.Options = []string{"ro"}
		return mount, nil
	} else {
		return mount, errors.New("read/write overlay mounts not supported on freebsd")
	}
}
