// Package image consolidates knowledge about various container image formats
// (as opposed to image storage mechanisms, which are handled by types.ImageSource)
// and exposes all of them using an unified interface.
package image

import (
	"context"

	"go.podman.io/image/v5/types"
)

// FromReference returns a types.ImageCloser implementation for the default instance reading from reference.
// If reference points to a manifest list, .Manifest() still returns the manifest list,
// but other methods transparently return data from an appropriate image instance.
//
// The caller must call .Close() on the returned ImageCloser.
//
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage instead of calling this function.
func FromReference(ctx context.Context, sys *types.SystemContext, ref types.ImageReference) (types.ImageCloser, error) {
	src, err := ref.NewImageSource(ctx, sys)
	if err != nil {
		return nil, err
	}
	img, err := FromSource(ctx, sys, src)
	if err != nil {
		src.Close()
		return nil, err
	}
	return img, nil
}

// imageCloser implements types.ImageCloser, perhaps allowing simple users
// to use a single object without having keep a reference to a types.ImageSource
// only to call types.ImageSource.Close().
type imageCloser struct {
	types.Image
	src types.ImageSource
}

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
//
// Most callers can use either FromUnparsedImage or FromReference instead.
//
// This is publicly visible as c/image/image.FromSource.
func FromSource(ctx context.Context, sys *types.SystemContext, src types.ImageSource) (types.ImageCloser, error) {
	img, err := FromUnparsedImage(ctx, sys, UnparsedInstance(src, nil))
	if err != nil {
		return nil, err
	}
	return &imageCloser{
		Image: img,
		src:   src,
	}, nil
}

func (ic *imageCloser) Close() error {
	return ic.src.Close()
}

// SourcedImage is a general set of utilities for working with container images,
// whatever is their underlying transport (i.e. ImageSource-independent).
// Note the existence of docker.Image and image.memoryImage: various instances
// of a types.Image may not be a SourcedImage directly.
//
// Most external users of `types.Image` do not care, and those who care about `docker.Image` know they do.
//
// Internal users may depend on methods available in SourcedImage but not (yet?) in types.Image.
type SourcedImage struct {
	*UnparsedImage
	ManifestBlob     []byte // The manifest of the relevant instance
	ManifestMIMEType string // MIME type of ManifestBlob
	// genericManifest contains data corresponding to manifestBlob.
	// NOTE: The manifest may have been modified in the process; DO NOT reserialize and store genericManifest
	// if you want to preserve the original manifest; use manifestBlob directly.
	genericManifest
}

// FromUnparsedImage returns a types.Image implementation for unparsed.
// If unparsed represents a manifest list, .Manifest() still returns the manifest list,
// but other methods transparently return data from an appropriate single image.
//
// The Image must not be used after the underlying ImageSource is Close()d.
//
// This is publicly visible as c/image/image.FromUnparsedImage.
func FromUnparsedImage(ctx context.Context, sys *types.SystemContext, unparsed *UnparsedImage) (*SourcedImage, error) {
	// Note that the input parameter above is specifically *image.UnparsedImage, not types.UnparsedImage:
	// we want to be able to use unparsed.src.  We could make that an explicit interface, but, well,
	// this is the only UnparsedImage implementation around, anyway.

	// NOTE: It is essential for signature verification that all parsing done in this object happens on the same manifest which is returned by unparsed.Manifest().
	manifestBlob, manifestMIMEType, err := unparsed.Manifest(ctx)
	if err != nil {
		return nil, err
	}

	parsedManifest, err := manifestInstanceFromBlob(ctx, sys, unparsed.src, manifestBlob, manifestMIMEType)
	if err != nil {
		return nil, err
	}

	return &SourcedImage{
		UnparsedImage:    unparsed,
		ManifestBlob:     manifestBlob,
		ManifestMIMEType: manifestMIMEType,
		genericManifest:  parsedManifest,
	}, nil
}

// Size returns the size of the image as stored, if it's known, or -1 if it isn't.
func (i *SourcedImage) Size() (int64, error) {
	return -1, nil
}

// Manifest overrides the UnparsedImage.Manifest to always use the fields which we have already fetched.
func (i *SourcedImage) Manifest(ctx context.Context) ([]byte, string, error) {
	return i.ManifestBlob, i.ManifestMIMEType, nil
}

func (i *SourcedImage) LayerInfosForCopy(ctx context.Context) ([]types.BlobInfo, error) {
	return i.UnparsedImage.src.LayerInfosForCopy(ctx, i.UnparsedImage.instanceDigest)
}
