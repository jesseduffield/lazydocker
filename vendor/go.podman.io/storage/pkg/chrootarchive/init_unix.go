//go:build !windows && !darwin

package chrootarchive

import (
	"fmt"
	"io"
	"os"

	"go.podman.io/storage/pkg/reexec"
)

func init() {
	reexec.Register("storage-applyLayer", applyLayer)
	reexec.Register("storage-untar", untar)
	reexec.Register("storage-tar", tar)
}

func fatal(err error) {
	fmt.Fprint(os.Stderr, err)
	os.Exit(1)
}

// flush consumes all the bytes from the reader discarding
// any errors
func flush(r io.Reader) (bytes int64, err error) {
	return io.Copy(io.Discard, r)
}
