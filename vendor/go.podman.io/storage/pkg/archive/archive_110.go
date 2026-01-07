//go:build go1.10

package archive

import (
	"archive/tar"
	"time"
)

func copyPassHeader(hdr *tar.Header) {
	hdr.Format = tar.FormatPAX
}

func maybeTruncateHeaderModTime(hdr *tar.Header) {
	if hdr.Format == tar.FormatUnknown {
		// one of the first things archive/tar does is round this
		// value, possibly up, if the format isn't specified, while we
		// are much better equipped to handle truncation when scanning
		// for changes between source and an extracted copy of this
		hdr.ModTime = hdr.ModTime.Truncate(time.Second)
	}
}
