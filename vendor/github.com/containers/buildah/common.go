package buildah

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"time"

	"github.com/containers/buildah/define"
	encconfig "github.com/containers/ocicrypt/config"
	"go.podman.io/common/pkg/retry"
	cp "go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/signature"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/unshare"
)

const (
	// OCI used to define the "oci" image format
	OCI = define.OCI
	// DOCKER used to define the "docker" image format
	DOCKER = define.DOCKER
)

func getCopyOptions(store storage.Store, reportWriter io.Writer, sourceSystemContext *types.SystemContext, destinationSystemContext *types.SystemContext, manifestType string, removeSignatures bool, addSigner string, ociEncryptLayers *[]int, ociEncryptConfig *encconfig.EncryptConfig, ociDecryptConfig *encconfig.DecryptConfig, destinationTimestamp *time.Time) *cp.Options {
	sourceCtx := getSystemContext(store, nil, "")
	if sourceSystemContext != nil {
		*sourceCtx = *sourceSystemContext
	}

	destinationCtx := getSystemContext(store, nil, "")
	if destinationSystemContext != nil {
		*destinationCtx = *destinationSystemContext
	}
	return &cp.Options{
		ReportWriter:          reportWriter,
		SourceCtx:             sourceCtx,
		DestinationCtx:        destinationCtx,
		ForceManifestMIMEType: manifestType,
		RemoveSignatures:      removeSignatures,
		SignBy:                addSigner,
		OciEncryptConfig:      ociEncryptConfig,
		OciDecryptConfig:      ociDecryptConfig,
		OciEncryptLayers:      ociEncryptLayers,
		DestinationTimestamp:  destinationTimestamp,
	}
}

func getSystemContext(store storage.Store, defaults *types.SystemContext, signaturePolicyPath string) *types.SystemContext {
	sc := &types.SystemContext{}
	if defaults != nil {
		*sc = *defaults
	}
	if signaturePolicyPath != "" {
		sc.SignaturePolicyPath = signaturePolicyPath
	}
	if store != nil {
		if sc.SystemRegistriesConfPath == "" && unshare.IsRootless() {
			userRegistriesFile := filepath.Join(store.GraphRoot(), "registries.conf")
			if err := fileutils.Exists(userRegistriesFile); err == nil {
				sc.SystemRegistriesConfPath = userRegistriesFile
			}
		}
	}
	return sc
}

func retryCopyImage(ctx context.Context, policyContext *signature.PolicyContext, maybeWrappedDest, maybeWrappedSrc, directDest types.ImageReference, copyOptions *cp.Options, maxRetries int, retryDelay time.Duration) ([]byte, error) {
	return retryCopyImageWithOptions(ctx, policyContext, maybeWrappedDest, maybeWrappedSrc, directDest, copyOptions, maxRetries, retryDelay, true)
}

func retryCopyImageWithOptions(ctx context.Context, policyContext *signature.PolicyContext, maybeWrappedDest, maybeWrappedSrc, directDest types.ImageReference, copyOptions *cp.Options, maxRetries int, retryDelay time.Duration, retryOnLayerUnknown bool) ([]byte, error) {
	var (
		manifestBytes []byte
		err           error
	)
	err = retry.IfNecessary(ctx, func() error {
		manifestBytes, err = cp.Image(ctx, policyContext, maybeWrappedDest, maybeWrappedSrc, copyOptions)
		return err
	}, &retry.RetryOptions{MaxRetry: maxRetries, Delay: retryDelay, IsErrorRetryable: func(err error) bool {
		if retryOnLayerUnknown && directDest.Transport().Name() == is.Transport.Name() && errors.Is(err, storage.ErrLayerUnknown) {
			// we were trying to reuse a layer that belonged to an
			// image that was deleted at just the right (worst
			// possible) time? yeah, try again
			return true
		}
		if directDest.Transport().Name() != docker.Transport.Name() {
			// if we're not talking to a registry, then nah
			return false
		}
		// hand it off to the default should-this-be-retried logic
		return retry.IsErrorRetryable(err)
	}})
	return manifestBytes, err
}
