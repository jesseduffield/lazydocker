package copy

import (
	"fmt"
	"hash"
	"io"

	digest "github.com/opencontainers/go-digest"
)

type digestingReader struct {
	source              io.Reader
	digester            digest.Digester
	hash                hash.Hash
	expectedDigest      digest.Digest
	validationFailed    bool
	validationSucceeded bool
}

// newDigestingReader returns an io.Reader implementation with contents of source, which will eventually return a non-EOF error
// or set validationSucceeded/validationFailed to true if the source stream does/does not match expectedDigest.
// (neither is set if EOF is never reached).
func newDigestingReader(source io.Reader, expectedDigest digest.Digest) (*digestingReader, error) {
	var digester digest.Digester
	if err := expectedDigest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid digest specification %q: %w", expectedDigest, err)
	}
	digestAlgorithm := expectedDigest.Algorithm()
	if !digestAlgorithm.Available() {
		return nil, fmt.Errorf("invalid digest specification %q: unsupported digest algorithm %q", expectedDigest, digestAlgorithm)
	}
	digester = digestAlgorithm.Digester()

	return &digestingReader{
		source:           source,
		digester:         digester,
		hash:             digester.Hash(),
		expectedDigest:   expectedDigest,
		validationFailed: false,
	}, nil
}

func (d *digestingReader) Read(p []byte) (int, error) {
	n, err := d.source.Read(p)
	if n > 0 {
		if n2, err := d.hash.Write(p[:n]); n2 != n || err != nil {
			// Coverage: This should not happen, the hash.Hash interface requires
			// d.digest.Write to never return an error, and the io.Writer interface
			// requires n2 == len(input) if no error is returned.
			return 0, fmt.Errorf("updating digest during verification: %d vs. %d: %w", n2, n, err)
		}
	}
	if err == io.EOF {
		actualDigest := d.digester.Digest()
		if actualDigest != d.expectedDigest {
			d.validationFailed = true
			return 0, fmt.Errorf("Digest did not match, expected %s, got %s", d.expectedDigest, actualDigest)
		}
		d.validationSucceeded = true
	}
	return n, err
}
