//go:build !freebsd && !linux

package overlay

import (
	"fmt"
	"runtime"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// MountWithOptions creates a subdir of the contentDir based on the source directory
// from the source system.  It then mounts up the source directory on to the
// generated mount point and returns the mount point to the caller.
// But allows api to set custom workdir, upperdir and other overlay options
// Following API is being used by podman at the moment
func MountWithOptions(contentDir, source, dest string, opts *Options) (mount specs.Mount, err error) {
	return mount, fmt.Errorf("read/write overlay mounts not supported on %q", runtime.GOOS)
}
