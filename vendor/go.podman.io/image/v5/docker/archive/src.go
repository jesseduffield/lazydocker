package archive

import (
	"go.podman.io/image/v5/docker/internal/tarfile"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/types"
)

type archiveImageSource struct {
	*tarfile.Source // Implements most of types.ImageSource
	ref             archiveReference
}

// newImageSource returns a types.ImageSource for the specified image reference.
// The caller must call .Close() on the returned ImageSource.
func newImageSource(sys *types.SystemContext, ref archiveReference) (private.ImageSource, error) {
	var archive *tarfile.Reader
	var closeArchive bool
	if ref.archiveReader != nil {
		archive = ref.archiveReader
		closeArchive = false
	} else {
		a, err := tarfile.NewReaderFromFile(sys, ref.path)
		if err != nil {
			return nil, err
		}
		archive = a
		closeArchive = true
	}
	src := tarfile.NewSource(archive, closeArchive, ref.Transport().Name(), ref.ref, ref.sourceIndex)
	return &archiveImageSource{
		Source: src,
		ref:    ref,
	}, nil
}

// Reference returns the reference used to set up this source, _as specified by the user_
// (not as the image itself, or its underlying storage, claims).  This can be used e.g. to determine which public keys are trusted for this image.
func (s *archiveImageSource) Reference() types.ImageReference {
	return s.ref
}
