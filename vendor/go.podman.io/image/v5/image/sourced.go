// Package image consolidates knowledge about various container image formats
// (as opposed to image storage mechanisms, which are handled by types.ImageSource)
// and exposes all of them using an unified interface.
package image

import (
	"context"

	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/types"
)

// FromSource returns a types.ImageCloser implementation for the default instance of source.
// If source is a manifest list, .Manifest() still returns the manifest list,
// but other methods transparently return data from an appropriate image instance.
//
// The caller must call .Close() on the returned ImageCloser.
//
// FromSource “takes ownership” of the input ImageSource and will call src.Close()
// when the image is closed.  (This does not prevent callers from using both the
// Image and ImageSource objects simultaneously, but it means that they only need to
// the Image.)
//
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage instead of calling this function.
func FromSource(ctx context.Context, sys *types.SystemContext, src types.ImageSource) (types.ImageCloser, error) {
	return image.FromSource(ctx, sys, src)
}

// FromUnparsedImage returns a types.Image implementation for unparsed.
// If unparsed represents a manifest list, .Manifest() still returns the manifest list,
// but other methods transparently return data from an appropriate single image.
//
// The Image must not be used after the underlying ImageSource is Close()d.
func FromUnparsedImage(ctx context.Context, sys *types.SystemContext, unparsed *UnparsedImage) (types.Image, error) {
	return image.FromUnparsedImage(ctx, sys, unparsed)
}
