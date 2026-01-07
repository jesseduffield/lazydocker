package archive

import (
	"context"
	"fmt"

	"go.podman.io/image/v5/docker/internal/tarfile"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/types"
)

type archiveImageDestination struct {
	*tarfile.Destination // Implements most of types.ImageDestination
	ref                  archiveReference
	writer               *Writer // Should be closed if closeWriter
	closeWriter          bool
}

func newImageDestination(sys *types.SystemContext, ref archiveReference) (private.ImageDestination, error) {
	if ref.sourceIndex != -1 {
		return nil, fmt.Errorf("Destination reference must not contain a manifest index @%d", ref.sourceIndex)
	}

	var writer *Writer
	var closeWriter bool
	if ref.writer != nil {
		writer = ref.writer
		closeWriter = false
	} else {
		w, err := NewWriter(sys, ref.path)
		if err != nil {
			return nil, err
		}
		writer = w
		closeWriter = true
	}
	d := &archiveImageDestination{
		ref:         ref,
		writer:      writer,
		closeWriter: closeWriter,
	}
	tarDest := tarfile.NewDestination(sys, writer.archive, ref.Transport().Name(), ref.ref, d.CommitWithOptions)
	if sys != nil && sys.DockerArchiveAdditionalTags != nil {
		tarDest.AddRepoTags(sys.DockerArchiveAdditionalTags)
	}
	d.Destination = tarDest
	return d, nil
}

// Reference returns the reference used to set up this destination.  Note that this should directly correspond to user's intent,
// e.g. it should use the public hostname instead of the result of resolving CNAMEs or following redirects.
func (d *archiveImageDestination) Reference() types.ImageReference {
	return d.ref
}

// Close removes resources associated with an initialized ImageDestination, if any.
func (d *archiveImageDestination) Close() error {
	if d.closeWriter {
		return d.writer.Close()
	}
	return nil
}

// CommitWithOptions marks the process of storing the image as successful and asks for the image to be persisted.
// WARNING: This does not have any transactional semantics:
// - Uploaded data MAY be visible to others before CommitWithOptions() is called
// - Uploaded data MAY be removed or MAY remain around if Close() is called without CommitWithOptions() (i.e. rollback is allowed but not guaranteed)
func (d *archiveImageDestination) CommitWithOptions(ctx context.Context, options private.CommitOptions) error {
	d.writer.imageCommitted()
	if d.closeWriter {
		// We could do this only in .Close(), but failures in .Close() are much more likely to be
		// ignored by callers that use defer. So, in single-image destinations, try to complete
		// the archive here.
		// But if Commit() is never called, let .Close() clean up.
		err := d.writer.Close()
		d.closeWriter = false
		return err
	}
	return nil
}
