//go:build !remote

package libimage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	dirTransport "go.podman.io/image/v5/directory"
	dockerArchiveTransport "go.podman.io/image/v5/docker/archive"
	ociArchiveTransport "go.podman.io/image/v5/oci/archive"
	ociTransport "go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
)

type LoadOptions struct {
	CopyOptions
}

// doLoadReference does the heavy lifting for LoadReference() and Load(),
// without adding debug messages or handling defaults.
func (r *Runtime) doLoadReference(ctx context.Context, ref types.ImageReference, options *LoadOptions) (images []string, transportName string, err error) {
	transportName = ref.Transport().Name()
	switch transportName {
	case dockerArchiveTransport.Transport.Name():
		images, err = r.loadMultiImageDockerArchive(ctx, ref, &options.CopyOptions)
	default:
		_, images, err = r.copyFromDefault(ctx, ref, &options.CopyOptions)
	}
	return images, ref.Transport().Name(), err
}

// LoadReference loads one or more images from the specified location.
func (r *Runtime) LoadReference(ctx context.Context, ref types.ImageReference, options *LoadOptions) ([]string, error) {
	logrus.Debugf("Loading image from %q", transports.ImageName(ref))

	if options == nil {
		options = &LoadOptions{}
	}
	images, _, err := r.doLoadReference(ctx, ref, options)
	return images, err
}

// Load loads one or more images (depending on the transport) from the
// specified path.  The path may point to an image the following transports:
// oci, oci-archive, dir, docker-archive.
//
// Load returns a string slice with names of recently loaded images.
// If images are unnamed in the source, it returns a string slice of image IDs instead.
func (r *Runtime) Load(ctx context.Context, path string, options *LoadOptions) ([]string, error) {
	logrus.Debugf("Loading image from %q", path)

	if options == nil {
		options = &LoadOptions{}
	}

	// we have 4 functions, so a maximum of 4 errors
	loadErrors := make([]error, 0, 4)
	for _, f := range []func() ([]string, string, error){
		// OCI
		func() ([]string, string, error) {
			logrus.Debugf("-> Attempting to load %q as an OCI directory", path)
			ref, err := ociTransport.NewReference(path, "")
			if err != nil {
				return nil, ociTransport.Transport.Name(), err
			}
			return r.doLoadReference(ctx, ref, options)
		},

		// OCI-ARCHIVE
		func() ([]string, string, error) {
			logrus.Debugf("-> Attempting to load %q as an OCI archive", path)
			ref, err := ociArchiveTransport.NewReference(path, "")
			if err != nil {
				return nil, ociArchiveTransport.Transport.Name(), err
			}
			return r.doLoadReference(ctx, ref, options)
		},

		// DOCKER-ARCHIVE
		func() ([]string, string, error) {
			logrus.Debugf("-> Attempting to load %q as a Docker archive", path)
			ref, err := dockerArchiveTransport.ParseReference(path)
			if err != nil {
				return nil, dockerArchiveTransport.Transport.Name(), err
			}
			return r.doLoadReference(ctx, ref, options)
		},

		// DIR
		func() ([]string, string, error) {
			logrus.Debugf("-> Attempting to load %q as a Docker dir", path)
			ref, err := dirTransport.NewReference(path)
			if err != nil {
				return nil, dirTransport.Transport.Name(), err
			}
			return r.doLoadReference(ctx, ref, options)
		},
	} {
		loadedImages, transportName, err := f()
		if err == nil {
			if r.eventChannel != nil {
				err = r.writeLoadEvents(path, loadedImages)
			}
			return loadedImages, err
		}
		logrus.Debugf("Error loading %s (%s): %v", path, transportName, err)
		loadErrors = append(loadErrors, fmt.Errorf("%s: %v", transportName, err))
	}

	// Give a decent error message if nothing above worked.
	// we want the colon here for the multiline error
	//nolint:revive,staticcheck
	loadError := errors.New("payload does not match any of the supported image formats:")
	for _, err := range loadErrors {
		loadError = fmt.Errorf("%v\n * %v", loadError, err)
	}

	return nil, loadError
}

// writeLoadEvents writes the events of the loaded image.
func (r *Runtime) writeLoadEvents(path string, loadedImages []string) error {
	for _, name := range loadedImages {
		image, _, err := r.LookupImage(name, nil)
		if err != nil {
			return fmt.Errorf("locating pulled image %q name in containers storage: %w", name, err)
		}
		r.writeEvent(&Event{ID: image.ID(), Name: path, Time: time.Now(), Type: EventTypeImageLoad})
	}
	return nil
}

// loadMultiImageDockerArchive loads the docker archive specified by ref.  In
// case the path@reference notation was used, only the specified image will be
// loaded.  Otherwise, all images will be loaded.
func (r *Runtime) loadMultiImageDockerArchive(ctx context.Context, ref types.ImageReference, options *CopyOptions) ([]string, error) {
	// If we cannot stat the path, it either does not exist OR the correct
	// syntax to reference an image within the archive was used, so we
	// should.
	path := ref.StringWithinTransport()
	if err := fileutils.Exists(path); err != nil {
		_, names, err := r.copyFromDockerArchive(ctx, ref, options)
		return names, err
	}

	reader, err := dockerArchiveTransport.NewReader(r.systemContextCopy(), path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			logrus.Errorf("Closing reader of docker archive: %v", err)
		}
	}()

	refLists, err := reader.List()
	if err != nil {
		return nil, err
	}

	var copiedImages []string
	for _, list := range refLists {
		for _, listRef := range list {
			_, names, err := r.copyFromDockerArchiveReaderReference(ctx, reader, listRef, options)
			if err != nil {
				return nil, err
			}
			copiedImages = append(copiedImages, names...)
		}
	}

	return copiedImages, nil
}
