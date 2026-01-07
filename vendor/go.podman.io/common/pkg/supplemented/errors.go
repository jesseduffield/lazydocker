package supplemented

import (
	"errors"

	"go.podman.io/common/pkg/manifests"
)

var (
	// ErrDigestNotFound is returned when we look for an image instance
	// with a particular digest in a list or index, and fail to find it.
	ErrDigestNotFound = manifests.ErrDigestNotFound
	// ErrBlobNotFound is returned when try to figure out which supplemental
	// image we should ask for a blob with the specified characteristics,
	// based on the information in each of the supplemental images' manifests.
	ErrBlobNotFound = errors.New("location of blob could not be determined")
)
