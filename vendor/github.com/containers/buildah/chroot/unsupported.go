//go:build !linux && !freebsd

package chroot

import (
	"fmt"
	"io"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// RunUsingChroot is not supported.
func RunUsingChroot(spec *specs.Spec, bundlePath, homeDir string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	return fmt.Errorf("--isolation chroot is not supported on this platform")
}
