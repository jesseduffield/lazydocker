package archive

import (
	"fmt"

	"go.podman.io/image/v5/docker/internal/tarfile"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

// Reader manages a single Docker archive, allows listing its contents and accessing
// individual images with less overhead than creating image references individually
// (because the archive is, if necessary, copied or decompressed only once).
type Reader struct {
	path    string // The original, user-specified path; not the maintained temporary file, if any
	archive *tarfile.Reader
}

// NewReader returns a Reader for path.
// The caller should call .Close() on the returned object.
func NewReader(sys *types.SystemContext, path string) (*Reader, error) {
	archive, err := tarfile.NewReaderFromFile(sys, path)
	if err != nil {
		return nil, err
	}
	return &Reader{
		path:    path,
		archive: archive,
	}, nil
}

// Close deletes temporary files associated with the Reader, if any.
func (r *Reader) Close() error {
	return r.archive.Close()
}

// NewReaderForReference creates a Reader from a Reader-independent imageReference, which must be from docker/archive.Transport,
// and a variant of imageReference that points at the same image within the reader.
// The caller should call .Close() on the returned Reader.
func NewReaderForReference(sys *types.SystemContext, ref types.ImageReference) (*Reader, types.ImageReference, error) {
	standalone, ok := ref.(archiveReference)
	if !ok {
		return nil, nil, fmt.Errorf("Internal error: NewReaderForReference called for a non-docker/archive ImageReference %s", transports.ImageName(ref))
	}
	if standalone.archiveReader != nil {
		return nil, nil, fmt.Errorf("Internal error: NewReaderForReference called for a reader-bound reference %s", standalone.StringWithinTransport())
	}
	reader, err := NewReader(sys, standalone.path)
	if err != nil {
		return nil, nil, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			reader.Close()
		}
	}()
	readerRef, err := newReference(standalone.path, standalone.ref, standalone.sourceIndex, reader.archive, nil)
	if err != nil {
		return nil, nil, err
	}
	succeeded = true
	return reader, readerRef, nil
}

// List returns the a set of references for images in the Reader,
// grouped by the image the references point to.
// The references are valid only until the Reader is closed.
func (r *Reader) List() ([][]types.ImageReference, error) {
	res := [][]types.ImageReference{}
	for imageIndex, image := range r.archive.Manifest {
		refs := []types.ImageReference{}
		for _, tag := range image.RepoTags {
			parsedTag, err := reference.ParseNormalizedNamed(tag)
			if err != nil {
				return nil, fmt.Errorf("Invalid tag %#v in manifest item @%d: %w", tag, imageIndex, err)
			}
			nt, ok := parsedTag.(reference.NamedTagged)
			if !ok {
				return nil, fmt.Errorf("Invalid tag %q (%s): does not contain a tag", tag, parsedTag.String())
			}
			ref, err := newReference(r.path, nt, -1, r.archive, nil)
			if err != nil {
				return nil, fmt.Errorf("creating a reference for tag %#v in manifest item @%d: %w", tag, imageIndex, err)
			}
			refs = append(refs, ref)
		}
		if len(refs) == 0 {
			ref, err := newReference(r.path, nil, imageIndex, r.archive, nil)
			if err != nil {
				return nil, fmt.Errorf("creating a reference for manifest item @%d: %w", imageIndex, err)
			}
			refs = append(refs, ref)
		}
		res = append(res, refs)
	}
	return res, nil
}

// ManifestTagsForReference returns the set of tags “matching” ref in reader, as strings
// (i.e. exposing the short names before normalization).
// The function reports an error if ref does not identify a single image.
// If ref contains a NamedTagged reference, only a single tag “matching” ref is returned;
// If ref contains a source index, or neither a NamedTagged nor a source index, all tags
// matching the image are returned.
// Almost all users should use List() or ImageReference.DockerReference() instead.
func (r *Reader) ManifestTagsForReference(ref types.ImageReference) ([]string, error) {
	archiveRef, ok := ref.(archiveReference)
	if !ok {
		return nil, fmt.Errorf("Internal error: ManifestTagsForReference called for a non-docker/archive ImageReference %s", transports.ImageName(ref))
	}
	manifestItem, tagIndex, err := r.archive.ChooseManifestItem(archiveRef.ref, archiveRef.sourceIndex)
	if err != nil {
		return nil, err
	}
	if tagIndex != -1 {
		return []string{manifestItem.RepoTags[tagIndex]}, nil
	}
	return manifestItem.RepoTags, nil
}
