package buildah

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/containers/buildah/pkg/blobcache"
	encconfig "github.com/containers/ocicrypt/config"
	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/archive"
)

// cacheLookupReferenceFunc wraps a BlobCache into a
// libimage.LookupReferenceFunc to allow for using a BlobCache during
// image-copy operations.
func cacheLookupReferenceFunc(directory string, compress types.LayerCompression) libimage.LookupReferenceFunc {
	// Using a closure here allows us to reference a BlobCache without
	// having to explicitly maintain it in the libimage API.
	return func(ref types.ImageReference) (types.ImageReference, error) {
		if directory == "" {
			return ref, nil
		}
		ref, err := blobcache.NewBlobCache(ref, directory, compress)
		if err != nil {
			return nil, fmt.Errorf("using blobcache %q: %w", directory, err)
		}
		return ref, nil
	}
}

// PushOptions can be used to alter how an image is copied somewhere.
type PushOptions struct {
	// Compression specifies the type of compression which is applied to
	// layer blobs.  The default is to not use compression, but
	// archive.Gzip is recommended.
	// OBSOLETE: Use CompressionFormat instead.
	Compression archive.Compression
	// SignaturePolicyPath specifies an override location for the signature
	// policy which should be used for verifying the new image as it is
	// being written.  Except in specific circumstances, no value should be
	// specified, indicating that the shared, system-wide default policy
	// should be used.
	SignaturePolicyPath string
	// ReportWriter is an io.Writer which will be used to log the writing
	// of the new image.
	ReportWriter io.Writer
	// Store is the local storage store which holds the source image.
	Store storage.Store
	// github.com/containers/image/types SystemContext to hold credentials
	// and other authentication/authorization information.
	SystemContext *types.SystemContext
	// ManifestType is the format to use
	// possible options are oci, v2s1, and v2s2
	ManifestType string
	// BlobDirectory is the name of a directory in which we'll look for
	// prebuilt copies of layer blobs that we might otherwise need to
	// regenerate from on-disk layers, substituting them in the list of
	// blobs to copy whenever possible.
	//
	// Not applicable if SourceLookupReferenceFunc is set.
	BlobDirectory string
	// Quiet is a boolean value that determines if minimal output to
	// the user will be displayed, this is best used for logging.
	// The default is false.
	Quiet bool
	// SignBy is the fingerprint of a GPG key to use for signing the image.
	SignBy string
	// RemoveSignatures causes any existing signatures for the image to be
	// discarded for the pushed copy.
	RemoveSignatures bool
	// MaxRetries is the maximum number of attempts we'll make to push any
	// one image to the external registry if the first attempt fails.
	MaxRetries int
	// RetryDelay is how long to wait before retrying a push attempt.
	RetryDelay time.Duration
	// OciEncryptConfig when non-nil indicates that an image should be encrypted.
	// The encryption options is derived from the construction of EncryptConfig object.
	OciEncryptConfig *encconfig.EncryptConfig
	// OciEncryptLayers represents the list of layers to encrypt.
	// If nil, don't encrypt any layers.
	// If non-nil and len==0, denotes encrypt all layers.
	// integers in the slice represent 0-indexed layer indices, with support for negative
	// indexing. i.e. 0 is the first layer, -1 is the last (top-most) layer.
	OciEncryptLayers *[]int
	// SourceLookupReference provides a function to look up source
	// references. Overrides BlobDirectory, if set.
	SourceLookupReferenceFunc libimage.LookupReferenceFunc
	// DestinationLookupReference provides a function to look up destination
	// references.
	DestinationLookupReferenceFunc libimage.LookupReferenceFunc

	// CompressionFormat is the format to use for the compression of the blobs
	CompressionFormat *compression.Algorithm
	// CompressionLevel specifies what compression level is used
	CompressionLevel *int
	// ForceCompressionFormat ensures that the compression algorithm set in
	// CompressionFormat is used exclusively, and blobs of other compression
	// algorithms are not reused.
	ForceCompressionFormat bool
}

// Push copies the contents of the image to a new location.
func Push(ctx context.Context, image string, dest types.ImageReference, options PushOptions) (reference.Canonical, digest.Digest, error) {
	libimageOptions := &libimage.PushOptions{}
	libimageOptions.SignaturePolicyPath = options.SignaturePolicyPath
	libimageOptions.Writer = options.ReportWriter
	libimageOptions.ManifestMIMEType = options.ManifestType
	libimageOptions.SignBy = options.SignBy
	libimageOptions.RemoveSignatures = options.RemoveSignatures
	libimageOptions.RetryDelay = &options.RetryDelay
	libimageOptions.OciEncryptConfig = options.OciEncryptConfig
	libimageOptions.OciEncryptLayers = options.OciEncryptLayers
	libimageOptions.CompressionFormat = options.CompressionFormat
	libimageOptions.CompressionLevel = options.CompressionLevel
	libimageOptions.ForceCompressionFormat = options.ForceCompressionFormat
	libimageOptions.PolicyAllowStorage = true

	if options.Quiet {
		libimageOptions.Writer = nil
	}

	compress := types.PreserveOriginal
	if options.Compression == archive.Gzip {
		compress = types.Compress
	}
	if options.SourceLookupReferenceFunc != nil {
		libimageOptions.SourceLookupReferenceFunc = options.SourceLookupReferenceFunc
	} else {
		libimageOptions.SourceLookupReferenceFunc = cacheLookupReferenceFunc(options.BlobDirectory, compress)
	}
	libimageOptions.DestinationLookupReferenceFunc = options.DestinationLookupReferenceFunc

	runtime, err := libimage.RuntimeFromStore(options.Store, &libimage.RuntimeOptions{SystemContext: options.SystemContext})
	if err != nil {
		return nil, "", err
	}

	destString := fmt.Sprintf("%s:%s", dest.Transport().Name(), dest.StringWithinTransport())
	manifestBytes, err := runtime.Push(ctx, image, destString, libimageOptions)
	if err != nil {
		return nil, "", err
	}

	manifestDigest, err := manifest.Digest(manifestBytes)
	if err != nil {
		return nil, "", fmt.Errorf("computing digest of manifest of new image %q: %w", transports.ImageName(dest), err)
	}

	var ref reference.Canonical
	if name := dest.DockerReference(); name != nil {
		ref, err = reference.WithDigest(name, manifestDigest)
		if err != nil {
			logrus.Warnf("error generating canonical reference with name %q and digest %s: %v", name, manifestDigest.String(), err)
		}
	}

	return ref, manifestDigest, nil
}
