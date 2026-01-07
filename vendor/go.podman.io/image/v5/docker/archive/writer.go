package archive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"go.podman.io/image/v5/docker/internal/tarfile"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
)

// Writer manages a single in-progress Docker archive and allows adding images to it.
type Writer struct {
	path        string // The original, user-specified path; not the maintained temporary file, if any
	regularFile bool   // path refers to a regular file (e.g. not a pipe)
	archive     *tarfile.Writer
	writer      io.Closer

	// The following state can only be accessed with the mutex held.
	mutex     sync.Mutex
	hadCommit bool // At least one successful commit has happened
}

// NewWriter returns a Writer for path.
// The caller should call .Close() on the returned object.
func NewWriter(sys *types.SystemContext, path string) (*Writer, error) {
	// path can be either a pipe or a regular file
	// in the case of a pipe, we require that we can open it for write
	// in the case of a regular file, we don't want to overwrite any pre-existing file
	// so we check for Size() == 0 below (This is racy, but using O_EXCL would also be racy,
	// only in a different way. Either way, it’s up to the user to not have two writers to the same path.)
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening file %q: %w", path, err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			fh.Close()
		}
	}()

	fhStat, err := fh.Stat()
	if err != nil {
		return nil, fmt.Errorf("statting file %q: %w", path, err)
	}
	regularFile := fhStat.Mode().IsRegular()
	if regularFile && fhStat.Size() != 0 {
		return nil, errors.New("docker-archive doesn't support modifying existing images")
	}

	archive := tarfile.NewWriter(fh)

	succeeded = true
	return &Writer{
		path:        path,
		regularFile: regularFile,
		archive:     archive,
		writer:      fh,
		hadCommit:   false,
	}, nil
}

// imageCommitted notifies the Writer that at least one image was successfully committed to the stream.
func (w *Writer) imageCommitted() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.hadCommit = true
}

// Close writes all outstanding data about images to the archive, and
// releases state associated with the Writer, if any.
// No more images can be added after this is called.
func (w *Writer) Close() error {
	err := w.archive.Close()
	if err2 := w.writer.Close(); err2 != nil && err == nil {
		err = err2
	}
	if err == nil && w.regularFile && !w.hadCommit {
		// Writing to the destination never had a success; delete the destination if we created it.
		// This is done primarily because we don’t implement adding another image to a pre-existing image, so if we
		// left a partial archive around (notably because reading from the _source_ has failed), we couldn’t retry without
		// the caller manually deleting the partial archive. So, delete it instead.
		//
		// Archives with at least one successfully created image are left around; they might still be valuable.
		//
		// Note a corner case: If there _originally_ was an empty file (which is not a valid archive anyway), this deletes it.
		// Ideally, if w.regularFile, we should write the full contents to a temporary file and use os.Rename here, only on success.
		if err2 := os.Remove(w.path); err2 != nil {
			err = err2
		}
	}
	return err
}

// NewReference returns an ImageReference that allows adding an image to Writer,
// with an optional reference.
func (w *Writer) NewReference(destinationRef reference.NamedTagged) (types.ImageReference, error) {
	return newReference(w.path, destinationRef, -1, nil, w)
}
