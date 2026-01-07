package tarball

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

const (
	transportName = "tarball"
	separator     = ":"
)

var (
	// Transport implements the types.ImageTransport interface for "tarball:" images,
	// which are makeshift images constructed using one or more possibly-compressed tar
	// archives.
	Transport = &tarballTransport{}
)

type tarballTransport struct {
}

func (t *tarballTransport) Name() string {
	return transportName
}

func (t *tarballTransport) ParseReference(reference string) (types.ImageReference, error) {
	var stdin []byte
	var err error
	filenames := strings.Split(reference, separator)
	for _, filename := range filenames {
		if filename == "-" {
			stdin, err = io.ReadAll(os.Stdin)
			if err != nil {
				return nil, fmt.Errorf("error buffering stdin: %w", err)
			}
			continue
		}
		f, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("error opening %q: %w", filename, err)
		}
		f.Close()
	}
	return NewReference(filenames, stdin)
}

// NewReference creates a new "tarball:" reference for the listed fileNames.
// If any of the fileNames is "-", the contents of stdin are used instead.
func NewReference(fileNames []string, stdin []byte) (types.ImageReference, error) {
	for _, path := range fileNames {
		if strings.Contains(path, separator) {
			return nil, fmt.Errorf("Invalid path %q: paths including the separator %q are not supported", path, separator)
		}
	}
	return &tarballReference{
		filenames: fileNames,
		stdin:     stdin,
	}, nil
}

func (t *tarballTransport) ValidatePolicyConfigurationScope(scope string) error {
	// See the explanation in daemonReference.PolicyConfigurationIdentity.
	return errors.New(`tarball: does not support any scopes except the default "" one`)
}

func init() {
	transports.Register(Transport)
}
