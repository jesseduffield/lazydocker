package buildah

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/containers/buildah/define"
	encconfig "github.com/containers/ocicrypt/config"
	"go.podman.io/common/libimage"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

// PullOptions can be used to alter how an image is copied in from somewhere.
type PullOptions struct {
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
	// BlobDirectory is the name of a directory in which we'll attempt to
	// store copies of layer blobs that we pull down, if any.  It should
	// already exist.
	//
	// Not applicable if DestinationLookupReferenceFunc is set.
	BlobDirectory string
	// AllTags is a boolean value that determines if all tagged images
	// will be downloaded from the repository. The default is false.
	AllTags bool
	// RemoveSignatures causes any existing signatures for the image to be
	// discarded when pulling it.
	RemoveSignatures bool
	// MaxRetries is the maximum number of attempts we'll make to pull any
	// one image from the external registry if the first attempt fails.
	MaxRetries int
	// RetryDelay is how long to wait before retrying a pull attempt.
	RetryDelay time.Duration
	// OciDecryptConfig contains the config that can be used to decrypt an image if it is
	// encrypted if non-nil. If nil, it does not attempt to decrypt an image.
	OciDecryptConfig *encconfig.DecryptConfig
	// PullPolicy takes the value PullIfMissing, PullAlways, PullIfNewer, or PullNever.
	PullPolicy define.PullPolicy
	// SourceLookupReference provides a function to look up source
	// references.
	SourceLookupReferenceFunc libimage.LookupReferenceFunc
	// DestinationLookupReference provides a function to look up destination
	// references. Overrides BlobDirectory, if set.
	DestinationLookupReferenceFunc libimage.LookupReferenceFunc
}

// Pull copies the contents of the image from somewhere else to local storage.  Returns the
// ID of the local image or an error.
func Pull(_ context.Context, imageName string, options PullOptions) (imageID string, err error) {
	libimageOptions := &libimage.PullOptions{}
	libimageOptions.SignaturePolicyPath = options.SignaturePolicyPath
	libimageOptions.Writer = options.ReportWriter
	libimageOptions.RemoveSignatures = options.RemoveSignatures
	libimageOptions.OciDecryptConfig = options.OciDecryptConfig
	libimageOptions.AllTags = options.AllTags
	libimageOptions.RetryDelay = &options.RetryDelay
	libimageOptions.SourceLookupReferenceFunc = options.SourceLookupReferenceFunc
	if options.DestinationLookupReferenceFunc != nil {
		libimageOptions.DestinationLookupReferenceFunc = options.DestinationLookupReferenceFunc
	} else {
		libimageOptions.DestinationLookupReferenceFunc = cacheLookupReferenceFunc(options.BlobDirectory, types.PreserveOriginal)
	}

	if options.MaxRetries > 0 {
		retries := uint(options.MaxRetries)
		libimageOptions.MaxRetries = &retries
	}

	pullPolicy, err := config.ParsePullPolicy(options.PullPolicy.String())
	if err != nil {
		return "", err
	}

	// Note: It is important to do this before we pull any images/create containers.
	// The default backend detection logic needs an empty store to correctly detect
	// that we can use netavark, if the store was not empty it will use CNI to not break existing installs.
	_, err = getNetworkInterface(options.Store, "", "")
	if err != nil {
		return "", err
	}

	runtime, err := libimage.RuntimeFromStore(options.Store, &libimage.RuntimeOptions{SystemContext: options.SystemContext})
	if err != nil {
		return "", err
	}

	pulledImages, err := runtime.Pull(context.Background(), imageName, pullPolicy, libimageOptions)
	if err != nil {
		return "", err
	}

	if len(pulledImages) == 0 {
		return "", fmt.Errorf("internal error pulling %s: no image pulled and no error", imageName)
	}

	return pulledImages[0].ID(), nil
}
