package buildah

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/containers/buildah/pkg/blobcache"
	"github.com/containers/buildah/util"
	encconfig "github.com/containers/ocicrypt/config"
	digest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage"
	"go.podman.io/common/libimage/manifests"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/signature"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/stringid"
)

const (
	// BuilderIdentityAnnotation is the name of the label which will be set
	// to contain the name and version of the producer of the image at
	// commit-time.  (N.B. yes, the constant's name includes "Annotation",
	// but it's added as a label.)
	BuilderIdentityAnnotation = "io.buildah.version"
)

// CommitOptions can be used to alter how an image is committed.
type CommitOptions struct {
	// PreferredManifestType is the preferred type of image manifest.  The
	// image configuration format will be of a compatible type.
	PreferredManifestType string
	// Compression specifies the type of compression which is applied to
	// layer blobs.  The default is to not use compression, but
	// archive.Gzip is recommended.
	Compression archive.Compression
	// SignaturePolicyPath specifies an override location for the signature
	// policy which should be used for verifying the new image as it is
	// being written.  Except in specific circumstances, no value should be
	// specified, indicating that the shared, system-wide default policy
	// should be used.
	SignaturePolicyPath string
	// AdditionalTags is a list of additional names to add to the image, if
	// the transport to which we're writing the image gives us a way to add
	// them.
	AdditionalTags []string
	// ReportWriter is an io.Writer which will be used to log the writing
	// of the new image.
	ReportWriter io.Writer
	// HistoryTimestamp specifies a timestamp to use for the image's
	// created-on date, the corresponding field in new history entries, and
	// the timestamps to set on contents in new layer diffs.  If left
	// unset, the current time is used for the configuration and manifest,
	// and timestamps of layer contents are used as-is.
	HistoryTimestamp *time.Time
	// SourceDateEpoch specifies a timestamp to use for the image's
	// created-on date and the corresponding field in new history entries.
	// If left unset, the current time is used for the configuration and
	// manifest.
	SourceDateEpoch *time.Time
	// RewriteTimestamp, if set, forces timestamps in generated layers to
	// not be later than the SourceDateEpoch, if it is set.
	RewriteTimestamp bool
	// github.com/containers/image/types SystemContext to hold credentials
	// and other authentication/authorization information.
	SystemContext *types.SystemContext
	// IIDFile tells the builder to write the image ID to the specified file
	IIDFile string
	// Squash tells the builder to produce an image with a single layer
	// instead of with possibly more than one layer.
	Squash bool
	// OmitHistory tells the builder to ignore the history of build layers and
	// base while preparing image-spec, setting this to true will ensure no history
	// is added to the image-spec. (default false)
	OmitHistory bool
	// BlobDirectory is the name of a directory in which we'll look for
	// prebuilt copies of layer blobs that we might otherwise need to
	// regenerate from on-disk layers.  If blobs are available, the
	// manifest of the new image will reference the blobs rather than
	// on-disk layers.
	BlobDirectory string
	// EmptyLayer tells the builder to omit the diff for the working
	// container.
	EmptyLayer bool
	// OmitLayerHistoryEntry tells the builder to omit the diff for the
	// working container and to not add an entry in the commit history.  By
	// default, the rest of the image's history is preserved, subject to
	// the OmitHistory setting.  N.B.: setting this flag, without any
	// PrependedEmptyLayers, AppendedEmptyLayers, PrependedLinkedLayers, or
	// AppendedLinkedLayers will more or less produce a copy of the base
	// image.
	OmitLayerHistoryEntry bool
	// OmitTimestamp forces epoch 0 as created timestamp to allow for
	// deterministic, content-addressable builds.
	// Deprecated: use HistoryTimestamp or SourceDateEpoch (possibly with
	// RewriteTimestamp) instead.
	OmitTimestamp bool
	// SignBy is the fingerprint of a GPG key to use for signing the image.
	SignBy string
	// Manifest list to add the image to.
	Manifest string
	// MaxRetries is the maximum number of attempts we'll make to commit
	// the image to an external registry if the first attempt fails.
	MaxRetries int
	// RetryDelay is how long to wait before retrying a commit attempt to a
	// registry.
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
	// ConfidentialWorkloadOptions is used to force the output image's rootfs to contain a
	// LUKS-compatibly encrypted disk image (for use with krun) instead of the usual
	// contents of a rootfs.
	ConfidentialWorkloadOptions ConfidentialWorkloadOptions
	// UnsetEnvs is a list of environments to not add to final image.
	// Deprecated: use UnsetEnv() before committing, or set OverrideChanges
	// instead.
	UnsetEnvs []string
	// OverrideConfig is an optional Schema2Config which can override parts
	// of the working container's configuration for the image that is being
	// committed.
	OverrideConfig *manifest.Schema2Config
	// OverrideChanges is a slice of Dockerfile-style instructions to make
	// to the configuration of the image that is being committed, after
	// OverrideConfig is applied.
	OverrideChanges []string
	// ExtraImageContent is a map which describes additional content to add
	// to the new layer in the committed image.  The map's keys are
	// filesystem paths in the image and the corresponding values are the
	// paths of files whose contents will be used in their place.  The
	// contents will be owned by 0:0 and have mode 0o644.  Currently only
	// accepts regular files.
	ExtraImageContent map[string]string
	// SBOMScanOptions encapsulates options which control whether or not we
	// run scanners on the rootfs that we're about to commit, and how.
	SBOMScanOptions []SBOMScanOptions
	// CompatSetParent causes the "parent" field to be set when committing
	// the image in Docker format.  Newer BuildKit-based builds don't set
	// this field.
	CompatSetParent types.OptionalBool
	// CompatLayerOmissions causes the "/dev", "/proc", and "/sys"
	// directories to be omitted from the layer diff and related output, as
	// the classic builder did.  Newer BuildKit-based builds include them
	// in the built image by default.
	CompatLayerOmissions types.OptionalBool
	// PrependedLinkedLayers and AppendedLinkedLayers are combinations of
	// history entries and locations of either directory trees (if
	// directories, per os.Stat()) or uncompressed layer blobs which should
	// be added to the image at commit-time.  The order of these relative
	// to PrependedEmptyLayers and AppendedEmptyLayers, and relative to the
	// corresponding members in the Builder object, in the committed image
	// is not guaranteed.
	PrependedLinkedLayers, AppendedLinkedLayers []LinkedLayer
	// UnsetAnnotations is a list of annotations (names only) to withhold
	// from the image.
	UnsetAnnotations []string
	// Annotations is a list of annotations (in the form "key=value") to
	// add to the image.
	Annotations []string
	// CreatedAnnotation controls whether or not an "org.opencontainers.image.created"
	// annotation is present in the output image.
	CreatedAnnotation types.OptionalBool
}

// LinkedLayer combines a history entry with the location of either a directory
// tree (if it's a directory, per os.Stat()) or an uncompressed layer blob
// which should be added to the image at commit-time.  The BlobPath and
// History.EmptyLayer fields should be considered mutually-exclusive.
type LinkedLayer struct {
	History  v1.History // history entry to add
	BlobPath string     // corresponding uncompressed blob file (layer as a tar archive), or directory tree to archive
}

// storageAllowedPolicyScopes overrides the policy for local storage
// to ensure that we can read images from it.
var storageAllowedPolicyScopes = signature.PolicyTransportScopes{
	"": []signature.PolicyRequirement{
		signature.NewPRInsecureAcceptAnything(),
	},
}

// checkRegistrySourcesAllows checks the $BUILD_REGISTRY_SOURCES environment
// variable, if it's set.  The contents are expected to be a JSON-encoded
// github.com/openshift/api/config/v1.Image, set by an OpenShift build
// controller that arranged for us to be run in a container.
func checkRegistrySourcesAllows(forWhat string, dest types.ImageReference) (insecure bool, err error) {
	transport := dest.Transport()
	if transport == nil {
		return false, nil
	}
	if transport.Name() != docker.Transport.Name() {
		return false, nil
	}
	dref := dest.DockerReference()
	if dref == nil || reference.Domain(dref) == "" {
		return false, nil
	}

	if registrySources, ok := os.LookupEnv("BUILD_REGISTRY_SOURCES"); ok && len(registrySources) > 0 {
		// Use local struct instead of github.com/openshift/api/config/v1 RegistrySources
		var sources struct {
			InsecureRegistries []string `json:"insecureRegistries,omitempty"`
			BlockedRegistries  []string `json:"blockedRegistries,omitempty"`
			AllowedRegistries  []string `json:"allowedRegistries,omitempty"`
		}
		if err := json.Unmarshal([]byte(registrySources), &sources); err != nil {
			return false, fmt.Errorf("parsing $BUILD_REGISTRY_SOURCES (%q) as JSON: %w", registrySources, err)
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
			return false, fmt.Errorf("%s registry at %q denied by policy: it is in the blocked registries list", forWhat, reference.Domain(dref))
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
			return false, fmt.Errorf("%s registry at %q denied by policy: not in allowed registries list", forWhat, reference.Domain(dref))
		}
		if len(sources.InsecureRegistries) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (b *Builder) addManifest(ctx context.Context, manifestName string, imageSpec string) (string, error) {
	var create bool
	systemContext := &types.SystemContext{}
	var list manifests.List
	runtime, err := libimage.RuntimeFromStore(b.store, &libimage.RuntimeOptions{SystemContext: systemContext})
	if err != nil {
		return "", err
	}
	manifestList, err := runtime.LookupManifestList(manifestName)
	if err != nil {
		create = true
		list = manifests.Create()
	} else {
		locker, err := manifests.LockerForImage(b.store, manifestList.ID())
		if err != nil {
			return "", err
		}
		locker.Lock()
		defer locker.Unlock()
		_, list, err = manifests.LoadFromImage(b.store, manifestList.ID())
		if err != nil {
			return "", err
		}
	}

	names, err := util.ExpandNames([]string{manifestName}, systemContext, b.store)
	if err != nil {
		return "", fmt.Errorf("encountered while expanding manifest list name %q: %w", manifestName, err)
	}

	ref, err := util.VerifyTagName(imageSpec)
	if err != nil {
		// check if the local image exists
		if ref, _, err = util.FindImage(b.store, "", systemContext, imageSpec); err != nil {
			return "", err
		}
	}

	if _, err = list.Add(ctx, systemContext, ref, true); err != nil {
		return "", err
	}
	var imageID string
	if create {
		imageID, err = list.SaveToImage(b.store, "", names, manifest.DockerV2ListMediaType)
	} else {
		imageID, err = list.SaveToImage(b.store, manifestList.ID(), nil, "")
	}
	return imageID, err
}

// Commit writes the contents of the container, along with its updated
// configuration, to a new image in the specified location, and if we know how,
// add any additional tags that were specified. Returns the ID of the new image
// if commit was successful and the image destination was local.
func (b *Builder) Commit(ctx context.Context, dest types.ImageReference, options CommitOptions) (string, reference.Canonical, digest.Digest, error) {
	var (
		imgID                string
		src                  types.ImageReference
		destinationTimestamp *time.Time
	)

	// If we weren't given a name, build a destination reference using a
	// temporary name that we'll remove later.  The correct thing to do
	// would be to read the manifest and configuration blob, and ask the
	// manifest for the ID that we'd give the image, but that computation
	// requires that we know the digests of the layer blobs, which we don't
	// want to compute here because we'll have to do it again when
	// cp.Image() instantiates a source image, and we don't want to do the
	// work twice.
	if options.OmitTimestamp {
		if options.HistoryTimestamp != nil {
			return imgID, nil, "", fmt.Errorf("OmitTimestamp and HistoryTimestamp can not be used together")
		}
		timestamp := time.Unix(0, 0).UTC()
		options.HistoryTimestamp = &timestamp
	}
	destinationTimestamp = options.HistoryTimestamp
	if options.SourceDateEpoch != nil {
		destinationTimestamp = options.SourceDateEpoch
	}
	nameToRemove := ""
	if dest == nil {
		nameToRemove = stringid.GenerateRandomID() + "-tmp"
		dest2, err := is.Transport.ParseStoreReference(b.store, nameToRemove)
		if err != nil {
			return imgID, nil, "", fmt.Errorf("creating temporary destination reference for image: %w", err)
		}
		dest = dest2
	}

	systemContext := getSystemContext(b.store, options.SystemContext, options.SignaturePolicyPath)

	blocked, err := isReferenceBlocked(dest, systemContext)
	if err != nil {
		return "", nil, "", fmt.Errorf("checking if committing to registry for %q is blocked: %w", transports.ImageName(dest), err)
	}
	if blocked {
		return "", nil, "", fmt.Errorf("commit access to registry for %q is blocked by configuration", transports.ImageName(dest))
	}

	// Load the system signing policy.
	commitPolicy, err := signature.DefaultPolicy(systemContext)
	if err != nil {
		return "", nil, "", fmt.Errorf("obtaining default signature policy: %w", err)
	}
	// Override the settings for local storage to make sure that we can always read the source "image".
	commitPolicy.Transports[is.Transport.Name()] = storageAllowedPolicyScopes

	policyContext, err := signature.NewPolicyContext(commitPolicy)
	if err != nil {
		return imgID, nil, "", fmt.Errorf("creating new signature policy context: %w", err)
	}
	defer func() {
		if err2 := policyContext.Destroy(); err2 != nil {
			logrus.Debugf("error destroying signature policy context: %v", err2)
		}
	}()

	// Check if the commit is blocked by $BUILDER_REGISTRY_SOURCES.
	insecure, err := checkRegistrySourcesAllows("commit to", dest)
	if err != nil {
		return imgID, nil, "", err
	}
	if insecure {
		if systemContext.DockerInsecureSkipTLSVerify == types.OptionalBoolFalse {
			return imgID, nil, "", fmt.Errorf("can't require tls verification on an insecured registry")
		}
		systemContext.DockerInsecureSkipTLSVerify = types.OptionalBoolTrue
		systemContext.OCIInsecureSkipTLSVerify = true
		systemContext.DockerDaemonInsecureSkipTLSVerify = true
	}
	logrus.Debugf("committing image with reference %q is allowed by policy", transports.ImageName(dest))

	// If we need to scan the rootfs, do it now.
	options.ExtraImageContent = maps.Clone(options.ExtraImageContent)
	var extraImageContent, extraLocalContent map[string]string
	if len(options.SBOMScanOptions) != 0 {
		var scansDirectory string
		if extraImageContent, extraLocalContent, scansDirectory, err = b.sbomScan(ctx, options); err != nil {
			return imgID, nil, "", fmt.Errorf("scanning rootfs to generate SBOM for container %q: %w", b.ContainerID, err)
		}
		if scansDirectory != "" {
			defer func() {
				if err := os.RemoveAll(scansDirectory); err != nil {
					logrus.Warnf("removing temporary directory %q: %v", scansDirectory, err)
				}
			}()
		}
		if len(extraImageContent) > 0 {
			if options.ExtraImageContent == nil {
				options.ExtraImageContent = make(map[string]string, len(extraImageContent))
			}
			// merge in the scanner-generated content
			for k, v := range extraImageContent {
				if _, set := options.ExtraImageContent[k]; !set {
					options.ExtraImageContent[k] = v
				}
			}
		}
	}

	// Build an image reference from which we can copy the finished image.
	src, err = b.makeContainerImageRef(options)
	if err != nil {
		return imgID, nil, "", fmt.Errorf("computing layer digests and building metadata for container %q: %w", b.ContainerID, err)
	}
	// In case we're using caching, decide how to handle compression for a cache.
	// If we're using blob caching, set it up for the source.
	maybeCachedSrc := src
	maybeCachedDest := dest
	if options.BlobDirectory != "" {
		compress := types.PreserveOriginal
		if options.Compression != archive.Uncompressed {
			compress = types.Compress
		}
		cache, err := blobcache.NewBlobCache(src, options.BlobDirectory, compress)
		if err != nil {
			return imgID, nil, "", fmt.Errorf("wrapping image reference %q in blob cache at %q: %w", transports.ImageName(src), options.BlobDirectory, err)
		}
		maybeCachedSrc = cache
		cache, err = blobcache.NewBlobCache(dest, options.BlobDirectory, compress)
		if err != nil {
			return imgID, nil, "", fmt.Errorf("wrapping image reference %q in blob cache at %q: %w", transports.ImageName(dest), options.BlobDirectory, err)
		}
		maybeCachedDest = cache
	}
	// "Copy" our image to where it needs to be.
	switch options.Compression {
	case archive.Uncompressed:
		systemContext.OCIAcceptUncompressedLayers = true
	case archive.Gzip:
		systemContext.DirForceCompress = true
	}

	if systemContext.ArchitectureChoice != b.Architecture() {
		systemContext.ArchitectureChoice = b.Architecture()
	}
	if systemContext.OSChoice != b.OS() {
		systemContext.OSChoice = b.OS()
	}

	var manifestBytes []byte
	if manifestBytes, err = retryCopyImage(ctx, policyContext, maybeCachedDest, maybeCachedSrc, dest, getCopyOptions(b.store, options.ReportWriter, nil, systemContext, "", false, options.SignBy, options.OciEncryptLayers, options.OciEncryptConfig, nil, destinationTimestamp), options.MaxRetries, options.RetryDelay); err != nil {
		return imgID, nil, "", fmt.Errorf("copying layers and metadata for container %q: %w", b.ContainerID, err)
	}
	// If we've got more names to attach, and we know how to do that for
	// the transport that we're writing the new image to, add them now.
	if len(options.AdditionalTags) > 0 {
		switch dest.Transport().Name() {
		case is.Transport.Name():
			_, img, err := is.ResolveReference(dest)
			if err != nil {
				return imgID, nil, "", fmt.Errorf("locating just-written image %q: %w", transports.ImageName(dest), err)
			}
			if err = util.AddImageNames(b.store, "", systemContext, img, options.AdditionalTags); err != nil {
				return imgID, nil, "", fmt.Errorf("setting image names to %v: %w", append(img.Names, options.AdditionalTags...), err)
			}
			logrus.Debugf("assigned names %v to image %q", img.Names, img.ID)
		default:
			logrus.Warnf("don't know how to add tags to images stored in %q transport", dest.Transport().Name())
		}
	}

	if dest.Transport().Name() == is.Transport.Name() {
		dest2, img, err := is.ResolveReference(dest)
		if err != nil {
			return imgID, nil, "", fmt.Errorf("locating image %q in local storage: %w", transports.ImageName(dest), err)
		}
		dest = dest2
		imgID = img.ID
		toPruneNames := make([]string, 0, len(img.Names))
		for _, name := range img.Names {
			if nameToRemove != "" && strings.Contains(name, nameToRemove) {
				toPruneNames = append(toPruneNames, name)
			}
		}
		if len(toPruneNames) > 0 {
			if err = b.store.RemoveNames(imgID, toPruneNames); err != nil {
				return imgID, nil, "", fmt.Errorf("failed to remove temporary name from image %q: %w", imgID, err)
			}
			logrus.Debugf("removing %v from assigned names to image %q", nameToRemove, img.ID)
		}
		if options.IIDFile != "" {
			if err = os.WriteFile(options.IIDFile, []byte("sha256:"+img.ID), 0o644); err != nil {
				return imgID, nil, "", err
			}
		}
	}
	// If we're supposed to store SBOM or PURL information in local files, write them now.
	for filename, content := range extraLocalContent {
		err := func() error {
			output, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			defer output.Close()
			input, err := os.Open(content)
			if err != nil {
				return err
			}
			defer input.Close()
			if _, err := io.Copy(output, input); err != nil {
				return fmt.Errorf("copying from %q to %q: %w", content, filename, err)
			}
			return nil
		}()
		if err != nil {
			return imgID, nil, "", err
		}
	}

	// Calculate the as-written digest of the image's manifest and build the digested
	// reference for the image.
	manifestDigest, err := manifest.Digest(manifestBytes)
	if err != nil {
		return imgID, nil, "", fmt.Errorf("computing digest of manifest of new image %q: %w", transports.ImageName(dest), err)
	}
	if imgID == "" {
		parsedManifest, err := manifest.FromBlob(manifestBytes, manifest.GuessMIMEType(manifestBytes))
		if err != nil {
			return imgID, nil, "", fmt.Errorf("parsing written manifest to determine the image's ID: %w", err)
		}
		configInfo := parsedManifest.ConfigInfo()
		if configInfo.Size > 2 && configInfo.Digest.Validate() == nil { // don't be returning a digest of "" or "{}"
			imgID = configInfo.Digest.Encoded()
		}
	}

	var ref reference.Canonical
	if name := dest.DockerReference(); name != nil {
		ref, err = reference.WithDigest(name, manifestDigest)
		if err != nil {
			logrus.Warnf("error generating canonical reference with name %q and digest %s: %v", name, manifestDigest.String(), err)
		}
	}

	if options.Manifest != "" {
		manifestID, err := b.addManifest(ctx, options.Manifest, imgID)
		if err != nil {
			return imgID, nil, "", err
		}
		logrus.Debugf("added imgID %s to manifestID %s", imgID, manifestID)
	}
	return imgID, ref, manifestDigest, nil
}
