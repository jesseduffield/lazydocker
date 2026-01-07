package tarball

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/types"
)

// ConfigUpdater is an interface that ImageReferences for "tarball" images also
// implement.  It can be used to set values for a configuration, and to set
// image annotations which will be present in the images returned by the
// reference's NewImage() or NewImageSource() methods.
type ConfigUpdater interface {
	ConfigUpdate(config imgspecv1.Image, annotations map[string]string) error
}

type tarballReference struct {
	config      imgspecv1.Image
	annotations map[string]string
	filenames   []string
	stdin       []byte
}

// ConfigUpdate updates the image's default configuration and adds annotations
// which will be visible in source images created using this reference.
func (r *tarballReference) ConfigUpdate(config imgspecv1.Image, annotations map[string]string) error {
	r.config = config
	if r.annotations == nil {
		r.annotations = make(map[string]string)
	}
	maps.Copy(r.annotations, annotations)
	return nil
}

func (r *tarballReference) Transport() types.ImageTransport {
	return Transport
}

func (r *tarballReference) StringWithinTransport() string {
	return strings.Join(r.filenames, ":")
}

func (r *tarballReference) DockerReference() reference.Named {
	return nil
}

func (r *tarballReference) PolicyConfigurationIdentity() string {
	return ""
}

func (r *tarballReference) PolicyConfigurationNamespaces() []string {
	return nil
}

// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned ImageCloser.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
func (r *tarballReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return image.FromReference(ctx, sys, r)
}

func (r *tarballReference) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	for _, filename := range r.filenames {
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("error removing %q: %w", filename, err)
		}
	}
	return nil
}

func (r *tarballReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return nil, fmt.Errorf(`"tarball:" locations can only be read from, not written to`)
}
