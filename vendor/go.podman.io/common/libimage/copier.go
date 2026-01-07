//go:build !remote

package libimage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	encconfig "github.com/containers/ocicrypt/config"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage/platform"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/retry"
	"go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/signature/signer"
	storageTransport "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

const (
	defaultMaxRetries = 3
	defaultRetryDelay = time.Second
)

// CopyOptions allow for customizing image-copy operations.
type CopyOptions struct {
	// If set, will be used for copying the image.  Fields below may
	// override certain settings.
	SystemContext *types.SystemContext
	// Allows for customizing the source reference lookup.  This can be
	// used to use custom blob caches.
	SourceLookupReferenceFunc LookupReferenceFunc
	// Allows for customizing the destination reference lookup.  This can
	// be used to use custom blob caches.
	DestinationLookupReferenceFunc LookupReferenceFunc
	// CompressionFormat is the format to use for the compression of the blobs
	CompressionFormat *compression.Algorithm
	// CompressionLevel specifies what compression level is used
	CompressionLevel *int
	// ForceCompressionFormat ensures that the compression algorithm set in
	// CompressionFormat is used exclusively, and blobs of other compression
	// algorithms are not reused.
	ForceCompressionFormat bool

	// containers-auth.json(5) file to use when authenticating against
	// container registries.
	AuthFilePath string
	// Custom path to a blob-info cache.
	BlobInfoCacheDirPath string
	// Path to the certificates directory.
	CertDirPath string
	// Force layer compression when copying to a `dir` transport destination.
	DirForceCompress bool

	// ImageListSelection is one of CopySystemImage, CopyAllImages, or
	// CopySpecificImages, to control whether, when the source reference is a list,
	// copy.Image() copies only an image which matches the current runtime
	// environment, or all images which match the supplied reference, or only
	// specific images from the source reference.
	ImageListSelection copy.ImageListSelection
	// Allow contacting registries over HTTP, or HTTPS with failed TLS
	// verification. Note that this does not affect other TLS connections.
	InsecureSkipTLSVerify types.OptionalBool
	// Maximum number of retries with exponential backoff when facing
	// transient network errors.  A reasonable default is used if not set.
	// Default 3.
	MaxRetries *uint
	// RetryDelay used for the exponential back off of MaxRetries.
	// Default 1 time.Second.
	RetryDelay *time.Duration
	// ManifestMIMEType is the desired media type the image will be
	// converted to if needed.  Note that it must contain the exact MIME
	// types.  Short forms (e.g., oci, v2s2) used by some tools are not
	// supported.
	ManifestMIMEType string
	// Accept uncompressed layers when copying OCI images.
	OciAcceptUncompressedLayers bool
	// If OciEncryptConfig is non-nil, it indicates that an image should be
	// encrypted.  The encryption options is derived from the construction
	// of EncryptConfig object.  Note: During initial encryption process of
	// a layer, the resultant digest is not known during creation, so
	// newDigestingReader has to be set with validateDigest = false
	OciEncryptConfig *encconfig.EncryptConfig
	// OciEncryptLayers represents the list of layers to encrypt.  If nil,
	// don't encrypt any layers.  If non-nil and len==0, denotes encrypt
	// all layers.  integers in the slice represent 0-indexed layer
	// indices, with support for negative indexing. i.e. 0 is the first
	// layer, -1 is the last (top-most) layer.
	OciEncryptLayers *[]int
	// OciDecryptConfig contains the config that can be used to decrypt an
	// image if it is encrypted if non-nil. If nil, it does not attempt to
	// decrypt an image.
	OciDecryptConfig *encconfig.DecryptConfig
	// Reported to when ProgressInterval has arrived for a single
	// artifact+offset.
	Progress chan types.ProgressProperties
	// If set, allow using the storage transport even if it's disabled by
	// the specified SignaturePolicyPath.
	PolicyAllowStorage bool
	// SignaturePolicyPath to overwrite the default one.
	SignaturePolicyPath string
	// If non-empty, asks for signatures to be added during the copy
	// using the provided signers.
	Signers []*signer.Signer
	// If non-empty, asks for a signature to be added during the copy, and
	// specifies a key ID.
	SignBy string
	// If non-empty, passphrase to use when signing with the key ID from SignBy.
	SignPassphrase string
	// If non-empty, asks for a signature to be added during the copy, using
	// a sigstore private key file at the provided path.
	SignBySigstorePrivateKeyFile string
	// Passphrase to use when signing with SignBySigstorePrivateKeyFile.
	SignSigstorePrivateKeyPassphrase []byte
	// Remove any pre-existing signatures. SignBy will still add a new
	// signature.
	RemoveSignatures bool
	// Writer is used to display copy information including progress bars.
	Writer io.Writer

	// ----- platform -----------------------------------------------------

	// Architecture to use for choosing images.
	Architecture string
	// OS to use for choosing images.
	OS string
	// Variant to use when choosing images.
	Variant string

	// ----- credentials --------------------------------------------------

	// Username to use when authenticating at a container registry.
	Username string
	// Password to use when authenticating at a container registry.
	Password string
	// Credentials is an alternative way to specify credentials in format
	// "username[:password]".  Cannot be used in combination with
	// Username/Password.
	Credentials string
	// IdentityToken is used to authenticate the user and get
	// an access token for the registry.
	IdentityToken string `json:"identitytoken,omitempty"`

	// ----- internal -----------------------------------------------------

	// Additional tags when creating or copying a docker-archive.
	dockerArchiveAdditionalTags []reference.NamedTagged

	// If set it points to a NOTIFY_SOCKET the copier will use to extend
	// the systemd timeout while copying.
	extendTimeoutSocket string
}

// Copier is a helper to conveniently copy images.
type Copier struct {
	extendTimeoutSocket string
	imageCopyOptions    copy.Options
	retryOptions        retry.Options
	systemContext       *types.SystemContext
	policyContext       *signature.PolicyContext

	sourceLookup      LookupReferenceFunc
	destinationLookup LookupReferenceFunc
}

// newCopier creates a Copier based on a runtime's system context.
// Note that fields in options *may* overwrite the counterparts of
// the specified system context.  Please make sure to call `(*Copier).Close()`.
func (r *Runtime) newCopier(options *CopyOptions) (*Copier, error) {
	return NewCopier(options, r.SystemContext())
}

// storageAllowedPolicyScopes overrides the policy for local storage
// to ensure that we can read images from it.
var storageAllowedPolicyScopes = signature.PolicyTransportScopes{
	"": []signature.PolicyRequirement{
		signature.NewPRInsecureAcceptAnything(),
	},
}

// getDockerAuthConfig extracts a docker auth config. Returns nil if
// no credentials are set.
func getDockerAuthConfig(name, passwd, creds, idToken string) (*types.DockerAuthConfig, error) {
	numCredsSources := 0

	if name != "" {
		numCredsSources++
	}
	if creds != "" {
		name, passwd, _ = strings.Cut(creds, ":")
		numCredsSources++
	}
	if idToken != "" {
		numCredsSources++
	}
	authConf := &types.DockerAuthConfig{
		Username:      name,
		Password:      passwd,
		IdentityToken: idToken,
	}

	switch numCredsSources {
	case 0:
		// Return nil if there is no credential source.
		return nil, nil
	case 1:
		return authConf, nil
	default:
		// Cannot use the multiple credential sources.
		return nil, errors.New("cannot use the multiple credential sources")
	}
}

// NewCopier creates a Copier based on a provided system context.
// Note that fields in options *may* overwrite the counterparts of
// the specified system context.  Please make sure to call `(*Copier).Close()`.
func NewCopier(options *CopyOptions, sc *types.SystemContext) (*Copier, error) {
	c := Copier{extendTimeoutSocket: options.extendTimeoutSocket}
	sysContextCopy := *sc
	c.systemContext = &sysContextCopy

	if options.SourceLookupReferenceFunc != nil {
		c.sourceLookup = options.SourceLookupReferenceFunc
	}

	if options.DestinationLookupReferenceFunc != nil {
		c.destinationLookup = options.DestinationLookupReferenceFunc
	}

	if options.InsecureSkipTLSVerify != types.OptionalBoolUndefined {
		c.systemContext.DockerInsecureSkipTLSVerify = options.InsecureSkipTLSVerify
		c.systemContext.OCIInsecureSkipTLSVerify = options.InsecureSkipTLSVerify == types.OptionalBoolTrue
		c.systemContext.DockerDaemonInsecureSkipTLSVerify = options.InsecureSkipTLSVerify == types.OptionalBoolTrue
	}

	c.systemContext.DirForceCompress = c.systemContext.DirForceCompress || options.DirForceCompress

	if options.AuthFilePath != "" {
		c.systemContext.AuthFilePath = options.AuthFilePath
	}

	c.systemContext.DockerArchiveAdditionalTags = options.dockerArchiveAdditionalTags

	c.systemContext.OSChoice, c.systemContext.ArchitectureChoice, c.systemContext.VariantChoice = platform.Normalize(options.OS, options.Architecture, options.Variant)

	if options.SignaturePolicyPath != "" {
		c.systemContext.SignaturePolicyPath = options.SignaturePolicyPath
	}

	dockerAuthConfig, err := getDockerAuthConfig(options.Username, options.Password, options.Credentials, options.IdentityToken)
	if err != nil {
		return nil, err
	}
	if dockerAuthConfig != nil {
		c.systemContext.DockerAuthConfig = dockerAuthConfig
	}

	if options.BlobInfoCacheDirPath != "" {
		c.systemContext.BlobInfoCacheDir = options.BlobInfoCacheDirPath
	}

	if options.CertDirPath != "" {
		c.systemContext.DockerCertPath = options.CertDirPath
	}

	if options.CompressionFormat != nil {
		c.systemContext.CompressionFormat = options.CompressionFormat
	}

	if options.CompressionLevel != nil {
		c.systemContext.CompressionLevel = options.CompressionLevel
	}

	// NOTE: for the sake of consistency it's called Oci* in the CopyOptions.
	c.systemContext.OCIAcceptUncompressedLayers = options.OciAcceptUncompressedLayers

	policy, err := signature.DefaultPolicy(c.systemContext)
	if err != nil {
		return nil, err
	}

	// Buildah compatibility: even if the policy denies _all_ transports,
	// Buildah still wants the storage to be accessible.
	if options.PolicyAllowStorage {
		policy.Transports[storageTransport.Transport.Name()] = storageAllowedPolicyScopes
	}

	policyContext, err := signature.NewPolicyContext(policy)
	if err != nil {
		return nil, err
	}

	c.policyContext = policyContext

	c.retryOptions.MaxRetry = defaultMaxRetries
	if options.MaxRetries != nil {
		c.retryOptions.MaxRetry = int(*options.MaxRetries)
	}
	c.retryOptions.Delay = defaultRetryDelay
	if options.RetryDelay != nil {
		c.retryOptions.Delay = *options.RetryDelay
	}

	c.imageCopyOptions.Progress = options.Progress
	if c.imageCopyOptions.Progress != nil {
		c.imageCopyOptions.ProgressInterval = time.Second
	}

	c.imageCopyOptions.ImageListSelection = options.ImageListSelection
	c.imageCopyOptions.ForceCompressionFormat = options.ForceCompressionFormat
	c.imageCopyOptions.ForceManifestMIMEType = options.ManifestMIMEType
	c.imageCopyOptions.SourceCtx = c.systemContext
	c.imageCopyOptions.DestinationCtx = c.systemContext
	c.imageCopyOptions.OciEncryptConfig = options.OciEncryptConfig
	c.imageCopyOptions.OciEncryptLayers = options.OciEncryptLayers
	c.imageCopyOptions.OciDecryptConfig = options.OciDecryptConfig
	c.imageCopyOptions.RemoveSignatures = options.RemoveSignatures
	c.imageCopyOptions.Signers = options.Signers
	c.imageCopyOptions.SignBy = options.SignBy
	c.imageCopyOptions.SignPassphrase = options.SignPassphrase
	c.imageCopyOptions.SignBySigstorePrivateKeyFile = options.SignBySigstorePrivateKeyFile
	c.imageCopyOptions.SignSigstorePrivateKeyPassphrase = options.SignSigstorePrivateKeyPassphrase
	c.imageCopyOptions.ReportWriter = options.Writer

	defaultContainerConfig, err := config.Default()
	if err != nil {
		logrus.Warnf("Failed to get container config for copy options: %v", err)
	} else {
		c.imageCopyOptions.MaxParallelDownloads = defaultContainerConfig.Engine.ImageParallelCopies
	}

	return &c, nil
}

// Close open resources.
func (c *Copier) Close() error {
	return c.policyContext.Destroy()
}

// Copy the source to the destination.  Returns the bytes of the copied
// manifest which may be used for digest computation.
func (c *Copier) Copy(ctx context.Context, source, destination types.ImageReference) ([]byte, error) {
	return c.copyInternal(ctx, source, destination, nil)
}

// Copy the source to the destination.  Returns the bytes of the copied
// manifest which may be used for digest computation.
func (c *Copier) copyInternal(ctx context.Context, source, destination types.ImageReference, reportResolvedReference *types.ImageReference) ([]byte, error) {
	logrus.Debugf("Copying source image %s to destination image %s", source.StringWithinTransport(), destination.StringWithinTransport())

	// Avoid running out of time when running inside a systemd unit by
	// regularly increasing the timeout.
	if c.extendTimeoutSocket != "" {
		socketAddr := &net.UnixAddr{
			Name: c.extendTimeoutSocket,
			Net:  "unixgram",
		}
		conn, err := net.DialUnix(socketAddr.Net, nil, socketAddr)
		if err != nil {
			return nil, err
		}
		defer conn.Close()

		numExtensions := 10
		extension := 30 * time.Second
		timerFrequency := 25 * time.Second // Fire the timer at a higher frequency to avoid a race
		timer := time.NewTicker(timerFrequency)
		socketCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		defer timer.Stop()

		if c.imageCopyOptions.ReportWriter != nil {
			fmt.Fprintf(c.imageCopyOptions.ReportWriter,
				"Pulling image %s inside systemd: setting pull timeout to %s\n",
				source.StringWithinTransport(),
				time.Duration(numExtensions)*extension,
			)
		}

		// From `man systemd.service(5)`:
		//
		// "If a service of Type=notify/Type=notify-reload sends "EXTEND_TIMEOUT_USEC=...", this may cause
		// the start time to be extended beyond TimeoutStartSec=. The first receipt of this message must
		// occur before TimeoutStartSec= is exceeded, and once the start time has extended beyond
		// TimeoutStartSec=, the service manager will allow the service to continue to start, provided the
		// service repeats "EXTEND_TIMEOUT_USEC=..."  within the interval specified until the service startup
		// status is finished by "READY=1"."
		extendValue := fmt.Appendf(nil, "EXTEND_TIMEOUT_USEC=%d", extension.Microseconds())
		extendTimeout := func() {
			if _, err := conn.Write(extendValue); err != nil {
				logrus.Errorf("Increasing EXTEND_TIMEOUT_USEC failed: %v", err)
			}
			numExtensions--
		}

		extendTimeout()
		go func() {
			for {
				select {
				case <-socketCtx.Done():
					return
				case <-timer.C:
					if numExtensions == 0 {
						return
					}
					extendTimeout()
				}
			}
		}()
	}

	var err error

	if c.sourceLookup != nil {
		source, err = c.sourceLookup(source)
		if err != nil {
			return nil, err
		}
	}

	if c.destinationLookup != nil {
		destination, err = c.destinationLookup(destination)
		if err != nil {
			return nil, err
		}
	}

	// Buildah compat: used when running in OpenShift.
	sourceInsecure, err := checkRegistrySourcesAllows(source)
	if err != nil {
		return nil, err
	}
	destinationInsecure, err := checkRegistrySourcesAllows(destination)
	if err != nil {
		return nil, err
	}

	// Sanity checks for Buildah.
	if sourceInsecure != nil && *sourceInsecure {
		if c.systemContext.DockerInsecureSkipTLSVerify == types.OptionalBoolFalse {
			return nil, errors.New("can't require tls verification on an insecured registry")
		}
	}
	if destinationInsecure != nil && *destinationInsecure {
		if c.systemContext.DockerInsecureSkipTLSVerify == types.OptionalBoolFalse {
			return nil, errors.New("can't require tls verification on an insecured registry")
		}
	}

	var returnManifest []byte
	f := func() error {
		opts := c.imageCopyOptions
		// This is already set when `newCopier` was called but there is an option
		// to override it by callers if needed.
		if reportResolvedReference != nil {
			opts.ReportResolvedReference = reportResolvedReference
		}
		if sourceInsecure != nil {
			value := types.NewOptionalBool(*sourceInsecure)
			opts.SourceCtx.DockerInsecureSkipTLSVerify = value
		}
		if destinationInsecure != nil {
			value := types.NewOptionalBool(*destinationInsecure)
			opts.DestinationCtx.DockerInsecureSkipTLSVerify = value
		}

		copiedManifest, err := copy.Image(ctx, c.policyContext, destination, source, &opts)
		if err == nil {
			returnManifest = copiedManifest
		}
		return err
	}
	return returnManifest, retry.IfNecessary(ctx, f, &c.retryOptions)
}

func (c *Copier) copyToStorage(ctx context.Context, source, destination types.ImageReference) (*storage.Image, error) {
	var resolvedReference types.ImageReference
	_, err := c.copyInternal(ctx, source, destination, &resolvedReference)
	if err != nil {
		return nil, fmt.Errorf("unable to copy from source %s: %w", transports.ImageName(source), err)
	}
	if resolvedReference == nil {
		return nil, fmt.Errorf("internal error: After attempting to copy %s, resolvedReference is nil", source)
	}
	_, image, err := storageTransport.ResolveReference(resolvedReference)
	if err != nil {
		return nil, fmt.Errorf("resolving an already-resolved reference %q to the pulled image: %w", transports.ImageName(resolvedReference), err)
	}
	return image, nil
}

// checkRegistrySourcesAllows checks the $BUILD_REGISTRY_SOURCES environment
// variable, if it's set.  The contents are expected to be a JSON-encoded
// github.com/openshift/api/config/v1.Image, set by an OpenShift build
// controller that arranged for us to be run in a container.
//
// If set, the insecure return value indicates whether the registry is set to
// be insecure.
//
// NOTE: this functionality is required by Buildah for OpenShift.
func checkRegistrySourcesAllows(dest types.ImageReference) (insecure *bool, err error) {
	registrySources, ok := os.LookupEnv("BUILD_REGISTRY_SOURCES")
	if !ok || registrySources == "" {
		return nil, nil
	}

	logrus.Debugf("BUILD_REGISTRY_SOURCES set %q", registrySources)

	dref := dest.DockerReference()
	if dref == nil || reference.Domain(dref) == "" {
		return nil, nil
	}

	// Use local struct instead of github.com/openshift/api/config/v1 RegistrySources
	var sources struct {
		InsecureRegistries []string `json:"insecureRegistries,omitempty"`
		BlockedRegistries  []string `json:"blockedRegistries,omitempty"`
		AllowedRegistries  []string `json:"allowedRegistries,omitempty"`
	}
	if err := json.Unmarshal([]byte(registrySources), &sources); err != nil {
		return nil, fmt.Errorf("parsing $BUILD_REGISTRY_SOURCES (%q) as JSON: %w", registrySources, err)
	}
	blocked := false
	if len(sources.BlockedRegistries) > 0 {
		for _, blockedDomain := range sources.BlockedRegistries {
			if blockedDomain == reference.Domain(dref) {
				blocked = true
			}
		}
	}
	if blocked {
		return nil, fmt.Errorf("registry %q denied by policy: it is in the blocked registries list (%s)", reference.Domain(dref), registrySources)
	}
	allowed := true
	if len(sources.AllowedRegistries) > 0 {
		allowed = false
		for _, allowedDomain := range sources.AllowedRegistries {
			if allowedDomain == reference.Domain(dref) {
				allowed = true
			}
		}
	}
	if !allowed {
		return nil, fmt.Errorf("registry %q denied by policy: not in allowed registries list (%s)", reference.Domain(dref), registrySources)
	}

	if slices.Contains(sources.InsecureRegistries, reference.Domain(dref)) {
		insecure := true
		return &insecure, nil
	}

	return nil, nil
}
