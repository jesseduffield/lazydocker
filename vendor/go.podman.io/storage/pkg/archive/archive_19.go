//go:build !go1.10

package archive

import (
	"archive/tar"
)

func copyPassHeader(hdr *tar.Header) {
}

func maybeTruncateHeaderModTime(hdr *tar.Header) {
}
