package manifests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	digest "github.com/opencontainers/go-digest"
	imgspec "github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/internal"
	"go.podman.io/common/pkg/manifests"
	"go.podman.io/common/pkg/retry"
	"go.podman.io/common/pkg/supplemented"
	cp "go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	ocilayout "go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/pkg/compression"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/signature/signer"
	is "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/lockfile"
)

const (
	defaultMaxRetries = 3
)

const (
	instancesData                = "instances.json"
	artifactsData                = "artifacts.json"
	pushingArtifactsSubdirectory = "referenced-artifacts"
)

// LookupReferenceFunc return an image reference based on the specified one.
// The returned reference can return custom ImageSource or ImageDestination
// objects which intercept or filter blobs, manifests, and signatures as
// they are read and written.
type LookupReferenceFunc func(ref types.ImageReference) (types.ImageReference, error)

// ErrListImageUnknown is returned when we attempt to create an image reference
// for a List that has not yet been saved to an image.
var ErrListImageUnknown = errors.New("unable to determine which image holds the manifest list")

type artifactsDetails struct {
	Manifests map[digest.Digest]string          `json:"manifests,omitempty"` // artifact (and other?) manifest digests ￫ manifest contents
	Files     map[digest.Digest][]string        `json:"files,omitempty"`     // artifact (and other?) manifest digests ￫ file paths (mainly for display)
	Configs   map[digest.Digest]digest.Digest   `json:"config,omitempty"`    // artifact (and other?) manifest digests ￫ referenced config digests
	Layers    map[digest.Digest][]digest.Digest `json:"layers,omitempty"`    // artifact (and other?) manifest digests ￫ referenced layer digests
	Detached  map[digest.Digest]string          `json:"detached,omitempty"`  // "config" and "layer" (and other?) digests in (usually artifact) manifests ￫ file paths
	Blobs     map[digest.Digest][]byte          `json:"blobs,omitempty"`     // "config" and "layer" (and other?) manifest digests ￫ inlined blob contents
}

type list struct {
	manifests.List
	instances map[digest.Digest]string // instance manifest digests ￫ image references
	artifacts artifactsDetails
}

// List is a manifest list or image index, either created using Create(), or
// loaded from local storage using LoadFromImage().
type List interface {
	manifests.List
	SaveToImage(store storage.Store, imageID string, names []string, mimeType string) (string, error)
	Reference(store storage.Store, multiple cp.ImageListSelection, instances []digest.Digest) (types.ImageReference, error)
	Push(ctx context.Context, dest types.ImageReference, options PushOptions) (reference.Canonical, digest.Digest, error)
	Add(ctx context.Context, sys *types.SystemContext, ref types.ImageReference, all bool) (digest.Digest, error)
	AddArtifact(ctx context.Context, sys *types.SystemContext, options AddArtifactOptions, files ...string) (digest.Digest, error)
	InstanceByFile(file string) (digest.Digest, error)
	Files(instanceDigest digest.Digest) ([]string, error)
}

// PushOptions includes various settings which are needed for pushing the
// manifest list and its instances.
type PushOptions struct {
	Store                            storage.Store
	SystemContext                    *types.SystemContext  // github.com/containers/image/types.SystemContext
	ImageListSelection               cp.ImageListSelection // set to either CopySystemImage, CopyAllImages, or CopySpecificImages
	Instances                        []digest.Digest       // instances to copy if ImageListSelection == CopySpecificImages
	ReportWriter                     io.Writer             // will be used to log the writing of the list and any blobs
	Signers                          []*signer.Signer      // if non-empty, asks for signatures to be added during the copy using the provided signers.
	SignBy                           string                // fingerprint of GPG key to use to sign images
	SignPassphrase                   string                // passphrase to use when signing with the key ID from SignBy.
	SignBySigstorePrivateKeyFile     string                // if non-empty, asks for a signature to be added during the copy, using a sigstore private key file at the provided path.
	SignSigstorePrivateKeyPassphrase []byte                // passphrase to use when signing with SignBySigstorePrivateKeyFile.
	RemoveSignatures                 bool                  // true to discard signatures in images
	ManifestType                     string                // the format to use when saving the list - possible options are oci, v2s1, and v2s2
	SourceFilter                     LookupReferenceFunc   // filter the list source
	AddCompression                   []string              // add existing instances with requested compression algorithms to manifest list
	ForceCompressionFormat           bool                  // force push with requested compression ignoring the blobs which can be reused.
	// Maximum number of retries with exponential backoff when facing
	// transient network errors. Default 3.
	MaxRetries *uint
	// RetryDelay used for the exponential back off of MaxRetries.
	RetryDelay *time.Duration
}

// Create creates a new list containing information about the specified image,
// computing its manifest's digest, and retrieving OS and architecture
// information from its configuration blob.  Returns the new list, and the
// instanceDigest for the initial image.
func Create() List {
	return &list{
		List:      manifests.Create(),
		instances: make(map[digest.Digest]string),
		artifacts: artifactsDetails{
			Manifests: make(map[digest.Digest]string),
			Files:     make(map[digest.Digest][]string),
			Configs:   make(map[digest.Digest]digest.Digest),
			Layers:    make(map[digest.Digest][]digest.Digest),
			Detached:  make(map[digest.Digest]string),
			Blobs:     make(map[digest.Digest][]byte),
		},
	}
}

// LoadFromImage reads the manifest list or image index, and additional
// information about where the various instances that it contains live, from an
// image record with the specified ID in local storage.
func LoadFromImage(store storage.Store, image string) (string, List, error) {
	img, err := store.Image(image)
	if err != nil {
		return "", nil, fmt.Errorf("locating image %q for loading manifest list: %w", image, err)
	}
	manifestBytes, err := store.ImageBigData(img.ID, storage.ImageDigestManifestBigDataNamePrefix)
	if err != nil {
		return "", nil, fmt.Errorf("locating image %q for loading manifest list: %w", image, err)
	}
	manifestList, err := manifests.FromBlob(manifestBytes)
	if err != nil {
		return "", nil, fmt.Errorf("decoding manifest blob for image %q: %w", image, err)
	}
	list := &list{
		List:      manifestList,
		instances: make(map[digest.Digest]string),
		artifacts: artifactsDetails{
			Manifests: make(map[digest.Digest]string),
			Files:     make(map[digest.Digest][]string),
			Configs:   make(map[digest.Digest]digest.Digest),
			Layers:    make(map[digest.Digest][]digest.Digest),
			Detached:  make(map[digest.Digest]string),
			Blobs:     make(map[digest.Digest][]byte),
		},
	}
	instancesBytes, err := store.ImageBigData(img.ID, instancesData)
	if err != nil {
		return "", nil, fmt.Errorf("locating image %q for loading instance list: %w", image, err)
	}
	if err := json.Unmarshal(instancesBytes, &list.instances); err != nil {
		return "", nil, fmt.Errorf("decoding instance list for image %q: %w", image, err)
	}
	artifactsBytes, err := store.ImageBigData(img.ID, artifactsData)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("locating image %q for loading instance list: %w", image, err)
		}
		artifactsBytes = []byte("{}")
	}
	if err := json.Unmarshal(artifactsBytes, &list.artifacts); err != nil {
		return "", nil, fmt.Errorf("decoding artifact list for image %q: %w", image, err)
	}
	list.instances[""] = img.ID
	return img.ID, list, nil
}

// SaveToImage saves the manifest list or image index as the manifest of an
// Image record with the specified names in local storage, generating a random
// image ID if none is specified.  It also stores information about where the
// images whose manifests are included in the list can be found.
func (l *list) SaveToImage(store storage.Store, imageID string, names []string, mimeType string) (string, error) {
	manifestBytes, err := l.List.Serialize(mimeType)
	if err != nil {
		return "", err
	}
	instancesBytes, err := json.Marshal(&l.instances)
	if err != nil {
		return "", err
	}
	artifactsBytes, err := json.Marshal(&l.artifacts)
	if err != nil {
		return "", err
	}
	manifestDigest, err := manifest.Digest(manifestBytes)
	if err != nil {
		return "", err
	}
	imageOptions := &storage.ImageOptions{
		BigData: []storage.ImageBigDataOption{
			{Key: storage.ImageDigestManifestBigDataNamePrefix, Data: manifestBytes, Digest: manifestDigest},
			{Key: instancesData, Data: instancesBytes},
			{Key: artifactsData, Data: artifactsBytes},
		},
	}
	img, err := store.CreateImage(imageID, names, "", "", imageOptions)
	if err != nil {
		if imageID != "" && errors.Is(err, storage.ErrDuplicateID) {
			for _, bd := range imageOptions.BigData {
				digester := manifest.Digest
				if !strings.HasPrefix(bd.Key, storage.ImageDigestManifestBigDataNamePrefix) {
					digester = nil
				}
				err := store.SetImageBigData(imageID, bd.Key, bd.Data, digester)
				if err != nil {
					return "", fmt.Errorf("saving manifest list to image %q: %w", imageID, err)
				}
			}
			return imageID, nil
		}
		return "", err
	}
	l.instances[""] = img.ID
	return img.ID, nil
}

// Files returns the list of files associated with a particular artifact
// instance in the image index, primarily for display purposes.
func (l *list) Files(instanceDigest digest.Digest) ([]string, error) {
	return slices.Clone(l.artifacts.Files[instanceDigest]), nil
}

// instanceByFile returns the instanceDigest of the first manifest in the index
// which refers to the named file.  The name will be passed to filepath.Abs()
// before searching for an instance which references it.
func (l *list) InstanceByFile(file string) (digest.Digest, error) {
	if parsedDigest, err := digest.Parse(file); err == nil {
		// nice try, but that's already a digest!
		return parsedDigest, nil
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	for instanceDigest, files := range l.artifacts.Files {
		if slices.Contains(files, abs) {
			return instanceDigest, nil
		}
	}
	return "", os.ErrNotExist
}

// Reference returns an image reference for the composite image being built
// in the list, or an error if the list has never been saved to a local image.
func (l *list) Reference(store storage.Store, multiple cp.ImageListSelection, instances []digest.Digest) (types.ImageReference, error) {
	if l.instances[""] == "" {
		return nil, fmt.Errorf("building reference to list, appears to have not been saved first: %w", ErrListImageUnknown)
	}
	s, err := is.Transport.ParseStoreReference(store, l.instances[""])
	if err != nil {
		return nil, fmt.Errorf("creating ImageReference from image %q: %w", l.instances[""], err)
	}
	references := make([]types.ImageReference, 0, len(l.instances))
	whichInstances := make([]digest.Digest, 0, len(l.instances))
	switch multiple {
	case cp.CopyAllImages, cp.CopySystemImage:
		for instance := range l.instances {
			if instance != "" {
				whichInstances = append(whichInstances, instance)
			}
		}
	case cp.CopySpecificImages:
		for instance := range l.instances {
			if slices.Contains(instances, instance) {
				whichInstances = append(whichInstances, instance)
			}
		}
	}
	if len(l.artifacts.Manifests) > 0 {
		img, err := is.Transport.GetImage(s)
		if err != nil {
			return nil, fmt.Errorf("locating image %s: %w", transports.ImageName(s), err)
		}
		imgDirectory, err := store.ImageDirectory(img.ID)
		if err != nil {
			return nil, fmt.Errorf("locating per-image directory for %s: %w", img.ID, err)
		}
		tmp, err := os.MkdirTemp(imgDirectory, pushingArtifactsSubdirectory)
		if err != nil {
			return nil, err
		}
		subdir := 0
		for artifactManifestDigest, contents := range l.artifacts.Manifests {
			// create the blobs directory
			subdir++
			tmp := filepath.Join(tmp, strconv.Itoa(subdir))
			blobsDir := filepath.Join(tmp, "blobs", artifactManifestDigest.Algorithm().String())
			if err := os.MkdirAll(blobsDir, 0o700); err != nil {
				return nil, fmt.Errorf("creating directory for blobs: %w", err)
			}
			// write the artifact manifest
			if err := os.WriteFile(filepath.Join(blobsDir, artifactManifestDigest.Encoded()), []byte(contents), 0o644); err != nil {
				return nil, fmt.Errorf("writing artifact manifest as blob: %w", err)
			}
			// symlink all of the referenced files and write the inlined blobs into the blobs directory
			var referencedBlobDigests []digest.Digest
			var symlinkedFiles []string
			if referencedConfigDigest, ok := l.artifacts.Configs[artifactManifestDigest]; ok {
				referencedBlobDigests = append(referencedBlobDigests, referencedConfigDigest)
			}
			referencedBlobDigests = append(referencedBlobDigests, l.artifacts.Layers[artifactManifestDigest]...)
			for _, referencedBlobDigest := range referencedBlobDigests {
				referencedFile, knownFile := l.artifacts.Detached[referencedBlobDigest]
				referencedBlob, knownBlob := l.artifacts.Blobs[referencedBlobDigest]
				if !knownFile && !knownBlob {
					return nil, fmt.Errorf(`internal error: no file or blob with artifact "config" or "layer" digest %q recorded`, referencedBlobDigest)
				}
				expectedLayerBlobPath := filepath.Join(blobsDir, referencedBlobDigest.Encoded())
				if err := fileutils.Lexists(expectedLayerBlobPath); err == nil {
					// did this one already
					continue
				} else if knownFile {
					if err := os.Symlink(referencedFile, expectedLayerBlobPath); err != nil {
						return nil, err
					}
					symlinkedFiles = append(symlinkedFiles, referencedFile)
				} else if knownBlob {
					if err := os.WriteFile(expectedLayerBlobPath, referencedBlob, 0o600); err != nil {
						return nil, err
					}
				}
			}
			// write the index that refers to this one artifact image
			indexFile := filepath.Join(tmp, v1.ImageIndexFile)
			index := v1.Index{
				Versioned: imgspec.Versioned{
					SchemaVersion: 2,
				},
				MediaType: v1.MediaTypeImageIndex,
				Manifests: []v1.Descriptor{{
					MediaType: v1.MediaTypeImageManifest,
					Digest:    artifactManifestDigest,
					Size:      int64(len(contents)),
				}},
			}
			indexBytes, err := json.Marshal(&index)
			if err != nil {
				return nil, fmt.Errorf("encoding image index for OCI layout: %w", err)
			}
			if err := os.WriteFile(indexFile, indexBytes, 0o644); err != nil {
				return nil, fmt.Errorf("writing image index for OCI layout: %w", err)
			}
			// write the layout file
			layoutFile := filepath.Join(tmp, v1.ImageLayoutFile)
			layoutBytes, err := json.Marshal(v1.ImageLayout{Version: v1.ImageLayoutVersion})
			if err != nil {
				return nil, fmt.Errorf("encoding image layout for OCI layout: %w", err)
			}
			if err := os.WriteFile(layoutFile, layoutBytes, 0o644); err != nil {
				return nil, fmt.Errorf("writing oci-layout file: %w", err)
			}
			// build the reference to this artifact image's oci layout
			ref, err := ocilayout.NewReference(tmp, "")
			if err != nil {
				return nil, fmt.Errorf("creating ImageReference for artifact with files %q: %w", symlinkedFiles, err)
			}
			references = append(references, ref)
		}
	}
	for _, instance := range whichInstances {
		imageName := l.instances[instance]
		ref, err := alltransports.ParseImageName(imageName)
		if err != nil {
			return nil, fmt.Errorf("creating ImageReference from image %q: %w", imageName, err)
		}
		references = append(references, ref)
	}
	return supplemented.Reference(s, references, multiple, instances), nil
}

// Push saves the manifest list and whichever blobs are needed to a destination location.
func (l *list) Push(ctx context.Context, dest types.ImageReference, options PushOptions) (reference.Canonical, digest.Digest, error) {
	// Load the system signing policy.
	pushPolicy, err := signature.DefaultPolicy(options.SystemContext)
	if err != nil {
		return nil, "", fmt.Errorf("obtaining default signature policy: %w", err)
	}

	// Override the settings for local storage to make sure that we can always read the source "image".
	pushPolicy.Transports[is.Transport.Name()] = storageAllowedPolicyScopes

	policyContext, err := signature.NewPolicyContext(pushPolicy)
	if err != nil {
		return nil, "", fmt.Errorf("creating new signature policy context: %w", err)
	}
	defer func() {
		if err2 := policyContext.Destroy(); err2 != nil {
			logrus.Errorf("Destroying signature policy context: %v", err2)
		}
	}()

	// If we were given a media type that corresponds to a multiple-images
	// type, reset it to a valid corresponding single-image type, since we
	// already expect the image library to infer the list type from the
	// image type that we're telling it to force.
	singleImageManifestType := options.ManifestType
	switch singleImageManifestType {
	case v1.MediaTypeImageIndex:
		singleImageManifestType = v1.MediaTypeImageManifest
	case manifest.DockerV2ListMediaType:
		singleImageManifestType = manifest.DockerV2Schema2MediaType
	}

	// Build a source reference for our list and grab bag full of blobs.
	src, err := l.Reference(options.Store, options.ImageListSelection, options.Instances)
	if err != nil {
		return nil, "", err
	}
	if options.SourceFilter != nil {
		if src, err = options.SourceFilter(src); err != nil {
			return nil, "", err
		}
	}
	compressionVariants, err := prepareAddWithCompression(options.AddCompression)
	if err != nil {
		return nil, "", err
	}
	copyOptions := &cp.Options{
		ImageListSelection:               options.ImageListSelection,
		Instances:                        options.Instances,
		SourceCtx:                        options.SystemContext,
		DestinationCtx:                   options.SystemContext,
		ReportWriter:                     options.ReportWriter,
		RemoveSignatures:                 options.RemoveSignatures,
		Signers:                          options.Signers,
		SignBy:                           options.SignBy,
		SignPassphrase:                   options.SignPassphrase,
		SignBySigstorePrivateKeyFile:     options.SignBySigstorePrivateKeyFile,
		SignSigstorePrivateKeyPassphrase: options.SignSigstorePrivateKeyPassphrase,
		ForceManifestMIMEType:            singleImageManifestType,
		EnsureCompressionVariantsExist:   compressionVariants,
		ForceCompressionFormat:           options.ForceCompressionFormat,
	}

	retryOptions := retry.Options{}
	retryOptions.MaxRetry = defaultMaxRetries
	if options.MaxRetries != nil {
		retryOptions.MaxRetry = int(*options.MaxRetries)
	}
	if options.RetryDelay != nil {
		retryOptions.Delay = *options.RetryDelay
	}

	// Copy whatever we were asked to copy.
	var manifestDigest digest.Digest
	f := func() error {
		opts := copyOptions
		var manifestBytes []byte
		var digest digest.Digest
		var err error
		if manifestBytes, err = cp.Image(ctx, policyContext, dest, src, opts); err == nil {
			if digest, err = manifest.Digest(manifestBytes); err == nil {
				manifestDigest = digest
			}
		}
		return err
	}
	err = retry.IfNecessary(ctx, f, &retryOptions)
	return nil, manifestDigest, err
}

func prepareAddWithCompression(variants []string) ([]cp.OptionCompressionVariant, error) {
	res := []cp.OptionCompressionVariant{}
	for _, name := range variants {
		algo, err := compression.AlgorithmByName(name)
		if err != nil {
			return nil, fmt.Errorf("requested algorithm %s is not supported for replication: %w", name, err)
		}
		res = append(res, cp.OptionCompressionVariant{Algorithm: algo})
	}
	return res, nil
}

func mapToSlice(m map[string]string) []string {
	slice := make([]string, 0, len(m))
	for key, value := range m {
		slice = append(slice, key+"="+value)
	}
	return slice
}

// Add adds information about the specified image to the list, computing the
// image's manifest's digest, retrieving OS and architecture information from
// the image's configuration, and recording the image's reference so that it
// can be found at push-time.  Returns the instanceDigest for the image.  If
// the reference points to an image list, either all instances are added (if
// "all" is true), or the instance which matches "sys" (if "all" is false) will
// be added.
func (l *list) Add(ctx context.Context, sys *types.SystemContext, ref types.ImageReference, all bool) (digest.Digest, error) {
	src, err := ref.NewImageSource(ctx, sys)
	if err != nil {
		return "", fmt.Errorf("setting up to read manifest and configuration from %q: %w", transports.ImageName(ref), err)
	}
	defer src.Close()

	type instanceInfo struct {
		instanceDigest                       *digest.Digest
		OS, Architecture, OSVersion, Variant string
		Features, OSFeatures, Annotations    []string
		Size                                 int64
		ConfigInfo                           types.BlobInfo
		ArtifactType                         string
		URLs                                 []string
	}
	var instanceInfos []instanceInfo
	var manifestDigest digest.Digest

	primaryManifestBytes, primaryManifestType, err := image.UnparsedInstance(src, nil).Manifest(ctx)
	if err != nil {
		return "", fmt.Errorf("reading manifest from %q: %w", transports.ImageName(ref), err)
	}

	if manifest.MIMETypeIsMultiImage(primaryManifestType) {
		lists, err := manifests.FromBlob(primaryManifestBytes)
		if err != nil {
			return "", fmt.Errorf("parsing manifest list in %q: %w", transports.ImageName(ref), err)
		}
		if all {
			for i, instance := range lists.OCIv1().Manifests {
				platform := instance.Platform
				if platform == nil {
					platform = &v1.Platform{}
				}
				instanceDigest := instance.Digest
				instanceInfo := instanceInfo{
					instanceDigest: &instanceDigest,
					OS:             platform.OS,
					Architecture:   platform.Architecture,
					OSVersion:      platform.OSVersion,
					Variant:        platform.Variant,
					Features:       append([]string{}, lists.Docker().Manifests[i].Platform.Features...),
					OSFeatures:     append([]string{}, platform.OSFeatures...),
					Size:           instance.Size,
					ArtifactType:   instance.ArtifactType,
					Annotations:    mapToSlice(instance.Annotations),
					URLs:           instance.URLs,
				}
				instanceInfos = append(instanceInfos, instanceInfo)
			}
		} else {
			list, err := manifest.ListFromBlob(primaryManifestBytes, primaryManifestType)
			if err != nil {
				return "", fmt.Errorf("parsing manifest list in %q: %w", transports.ImageName(ref), err)
			}
			instanceDigest, err := list.ChooseInstance(sys)
			if err != nil {
				return "", fmt.Errorf("selecting image from manifest list in %q: %w", transports.ImageName(ref), err)
			}
			added := false
			for i, instance := range lists.OCIv1().Manifests {
				if instance.Digest != instanceDigest {
					continue
				}
				platform := instance.Platform
				if platform == nil {
					platform = &v1.Platform{}
				}
				instanceInfo := instanceInfo{
					instanceDigest: &instanceDigest,
					OS:             platform.OS,
					Architecture:   platform.Architecture,
					OSVersion:      platform.OSVersion,
					Variant:        platform.Variant,
					Features:       append([]string{}, lists.Docker().Manifests[i].Platform.Features...),
					OSFeatures:     append([]string{}, platform.OSFeatures...),
					Size:           instance.Size,
					ArtifactType:   instance.ArtifactType,
					Annotations:    mapToSlice(instance.Annotations),
					URLs:           instance.URLs,
				}
				instanceInfos = append(instanceInfos, instanceInfo)
				added = true
			}
			if !added {
				instanceInfo := instanceInfo{
					instanceDigest: &instanceDigest,
				}
				instanceInfos = append(instanceInfos, instanceInfo)
			}
		}
	} else {
		instanceInfo := instanceInfo{
			instanceDigest: nil,
		}
		if primaryManifestType == v1.MediaTypeImageManifest {
			if m, err := manifest.OCI1FromManifest(primaryManifestBytes); err == nil {
				instanceInfo.ArtifactType = m.ArtifactType
			}
		}
		instanceInfos = append(instanceInfos, instanceInfo)
	}

	knownConfigTypes := []string{manifest.DockerV2Schema2ConfigMediaType, v1.MediaTypeImageConfig}
	for _, instanceInfo := range instanceInfos {
		unparsedInstance := image.UnparsedInstance(src, instanceInfo.instanceDigest)
		manifestBytes, manifestType, err := unparsedInstance.Manifest(ctx)
		if err != nil {
			return "", fmt.Errorf("reading manifest from %q, instance %q: %w", transports.ImageName(ref), instanceInfo.instanceDigest, err)
		}
		instanceManifest, err := manifest.FromBlob(manifestBytes, manifestType)
		if err != nil {
			return "", fmt.Errorf("parsing manifest from %q, instance %q: %w", transports.ImageName(ref), instanceInfo.instanceDigest, err)
		}
		instanceInfo.ConfigInfo = instanceManifest.ConfigInfo()
		hasPlatformConfig := instanceInfo.ArtifactType == "" && slices.Contains(knownConfigTypes, instanceInfo.ConfigInfo.MediaType)
		needToParsePlatformConfig := (instanceInfo.OS == "" || instanceInfo.Architecture == "")
		if hasPlatformConfig && needToParsePlatformConfig {
			img, err := image.FromUnparsedImage(ctx, sys, unparsedInstance)
			if err != nil {
				return "", fmt.Errorf("reading configuration blob from %q: %w", transports.ImageName(ref), err)
			}
			config, err := img.OCIConfig(ctx)
			if err != nil {
				return "", fmt.Errorf("reading info about config blob from %q: %w", transports.ImageName(ref), err)
			}
			if instanceInfo.OS == "" {
				instanceInfo.OS = config.OS
				instanceInfo.OSVersion = config.OSVersion
				instanceInfo.OSFeatures = slices.Clone(config.OSFeatures)
			}
			if instanceInfo.Architecture == "" {
				instanceInfo.Architecture = config.Architecture
				instanceInfo.Variant = config.Variant
			}
		}
		if instanceInfo.instanceDigest == nil {
			manifestDigest, err = manifest.Digest(manifestBytes)
			if err != nil {
				return "", fmt.Errorf("computing digest of manifest from %q: %w", transports.ImageName(ref), err)
			}
			instanceInfo.instanceDigest = &manifestDigest
			instanceInfo.Size = int64(len(manifestBytes))
		} else if manifestDigest == "" {
			manifestDigest = *instanceInfo.instanceDigest
		}
		err = l.List.AddInstance(*instanceInfo.instanceDigest, instanceInfo.Size, manifestType, instanceInfo.OS, instanceInfo.Architecture, instanceInfo.OSVersion, instanceInfo.OSFeatures, instanceInfo.Variant, instanceInfo.Features, instanceInfo.Annotations)
		if err != nil {
			return "", fmt.Errorf("adding instance with digest %q: %w", *instanceInfo.instanceDigest, err)
		}
		if err := l.List.SetArtifactType(instanceInfo.instanceDigest, instanceInfo.ArtifactType); err != nil {
			return "", fmt.Errorf("setting artifact manifest type for instance with digest %q: %w", *instanceInfo.instanceDigest, err)
		}
		if err = l.List.SetURLs(*instanceInfo.instanceDigest, instanceInfo.URLs); err != nil {
			return "", fmt.Errorf("setting URLs for instance with digest %q: %w", *instanceInfo.instanceDigest, err)
		}
		if _, ok := l.instances[*instanceInfo.instanceDigest]; !ok {
			l.instances[*instanceInfo.instanceDigest] = transports.ImageName(ref)
		}
	}

	return manifestDigest, nil
}

// AddArtifactOptions contains options which control the contents of the
// artifact manifest that AddArtifact will create and add to the image index.

// AddArtifactOptions should provide for all of the ways to construct a manifest outlined in
// https://github.com/opencontainers/image-spec/blob/main/manifest.md#guidelines-for-artifact-usage
//   - no blobs ￫ set ManifestArtifactType
//   - blobs, no configuration ￫ set ManifestArtifactType and possibly LayerMediaType, and provide file names
//   - blobs and configuration ￫ set ManifestArtifactType, possibly LayerMediaType, and ConfigDescriptor, and provide file names
//
// The older style of describing artifacts:
//   - leave ManifestArtifactType blank
//   - specify a zero-length application/vnd.oci.image.config.v1+json config blob
//   - set LayerMediaType to a custom type
//
// When reading data produced elsewhere, note that newer tooling will produce
// manifests with ArtifactType set.  If the manifest's ArtifactType is not set,
// consumers should consult the config descriptor's MediaType.
type AddArtifactOptions struct {
	ManifestArtifactType *string              // overall type of the artifact manifest. default: "application/vnd.unknown.artifact.v1"
	Platform             v1.Platform          // default: add to the index without platform information
	ConfigDescriptor     *v1.Descriptor       // default: a descriptor for an explicitly empty config blob
	ConfigFile           string               // path to config contents, recorded if ConfigDescriptor.Size != 0 and ConfigDescriptor.Data is not set
	LayerMediaType       *string              // default: mime.TypeByExtension() if basename contains ".", else http.DetectContentType()
	Annotations          map[string]string    // optional, default is none
	SubjectReference     types.ImageReference // optional
	ExcludeTitles        bool                 // don't add "org.opencontainers.image.title" annotations set to file base names
}

// AddArtifact creates an artifact manifest describing the specified file or
// files, then adds them to the specified image index.  Returns the
// instanceDigest for the artifact manifest.
// The caller could craft the manifest themselves and use Add() to add it to
// the image index and get the same end-result, but this should save them some
// work.
func (l *list) AddArtifact(ctx context.Context, sys *types.SystemContext, options AddArtifactOptions, files ...string) (digest.Digest, error) {
	// If we were given a subject, build a descriptor for it first, since
	// it might be remote, and anything else we do before looking at it
	// might have to get thrown away if we can't get to it for whatever
	// reason.
	var subject *v1.Descriptor
	if options.SubjectReference != nil {
		subjectSource, err := options.SubjectReference.NewImageSource(ctx, sys)
		if err != nil {
			return "", fmt.Errorf("setting up to read manifest and configuration from subject %q: %w", transports.ImageName(options.SubjectReference), err)
		}
		defer subjectSource.Close()
		subjectManifestBytes, subjectManifestType, err := image.UnparsedInstance(subjectSource, nil).Manifest(ctx)
		if err != nil {
			return "", fmt.Errorf("reading manifest from subject %q: %w", transports.ImageName(options.SubjectReference), err)
		}
		subjectManifestDigest, err := manifest.Digest(subjectManifestBytes)
		if err != nil {
			return "", fmt.Errorf("digesting manifest of subject %q: %w", transports.ImageName(options.SubjectReference), err)
		}
		var subjectArtifactType string
		if !manifest.MIMETypeIsMultiImage(subjectManifestType) {
			var subjectManifest v1.Manifest
			if json.Unmarshal(subjectManifestBytes, &subjectManifest) == nil {
				subjectArtifactType = subjectManifest.ArtifactType
			}
		}
		subject = &v1.Descriptor{
			MediaType:    subjectManifestType,
			ArtifactType: subjectArtifactType,
			Digest:       subjectManifestDigest,
			Size:         int64(len(subjectManifestBytes)),
		}
	}

	// Build up the layers list piece by piece.
	var layers []v1.Descriptor
	fileDigests := make(map[string]digest.Digest)

	if len(files) == 0 {
		// https://github.com/opencontainers/image-spec/blob/main/manifest.md#guidelines-for-artifact-usage
		// says that we should have at least one layer listed, even if it's just a placeholder
		layers = append(layers, v1.DescriptorEmptyJSON)
	}
	for _, file := range files {
		if err := func() error {
			// Open the file so that we can digest it.
			absFile, err := filepath.Abs(file)
			if err != nil {
				return fmt.Errorf("converting %q to an absolute path: %w", file, err)
			}

			f, err := os.Open(absFile)
			if err != nil {
				return fmt.Errorf("reading %q to determine its digest: %w", file, err)
			}
			defer f.Close()

			// Hang on to a copy of the first 512 bytes, but digest the whole thing.
			digester := digest.Canonical.Digester()
			writeCounter := ioutils.NewWriteCounter(digester.Hash())
			var detectableData bytes.Buffer
			_, err = io.CopyN(writeCounter, io.TeeReader(f, &detectableData), 512)
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading %q to determine its digest: %w", file, err)
			}
			if err == nil {
				if _, err := io.Copy(writeCounter, f); err != nil {
					return fmt.Errorf("reading %q to determine its digest: %w", file, err)
				}
			}
			fileDigests[absFile] = digester.Digest()

			// If one wasn't specified, figure out what the MediaType should be.
			title := filepath.Base(absFile)
			layerMediaType := options.LayerMediaType
			if layerMediaType == nil {
				if index := strings.LastIndex(title, "."); index != -1 {
					// File's basename has an extension, try to use a shortcut.
					tmp := mime.TypeByExtension(title[index:])
					if tmp != "" {
						layerMediaType = &tmp
					}
				}
				if layerMediaType == nil {
					// File's basename has no extension or didn't map to a type, look at the contents we saved.
					tmp := http.DetectContentType(detectableData.Bytes())
					layerMediaType = &tmp
				}
				if layerMediaType != nil {
					// Strip off any parameters, since we only want the type name.
					if parsedMediaType, _, err := mime.ParseMediaType(*layerMediaType); err == nil {
						layerMediaType = &parsedMediaType
					}
				}
			}

			// Build the descriptor for the layer.
			descriptor := v1.Descriptor{
				MediaType: *layerMediaType,
				Digest:    fileDigests[absFile],
				Size:      writeCounter.Count,
			}
			// OCI annotations are usually applied at the image manifest as a whole,
			// but tools like oras (https://oras.land/) also apply them to blob
			// descriptors.  AnnotationTitle is used as a suggestion for the name
			// to give to a blob if it's being stored as a file, and we default
			// to adding one based on its original name.
			if !options.ExcludeTitles {
				descriptor.Annotations = map[string]string{
					v1.AnnotationTitle: title,
				}
			}
			layers = append(layers, descriptor)
			return nil
		}(); err != nil {
			return "", err
		}
	}

	// Unless we were told what this is, use the default that ORAS uses.
	artifactType := "application/vnd.unknown.artifact.v1"
	if options.ManifestArtifactType != nil {
		artifactType = *options.ManifestArtifactType
	}

	// Unless we were explicitly told otherwise, default to an empty config blob.
	configDescriptor := internal.DeepCopyDescriptor(&v1.DescriptorEmptyJSON)
	if options.ConfigDescriptor != nil {
		configDescriptor = internal.DeepCopyDescriptor(options.ConfigDescriptor)
	}
	if options.ConfigFile != "" {
		if options.ConfigDescriptor == nil { // i.e., we assigned the default mediatype
			configDescriptor.MediaType = v1.MediaTypeImageConfig
		}
		configDescriptor.Data = nil
		configDescriptor.Digest = "" // to be figured out below
		configDescriptor.Size = -1   // to be figured out below
	}
	configFilePath := ""
	if configDescriptor.Size != 0 {
		if len(configDescriptor.Data) == 0 {
			if options.ConfigFile == "" {
				return "", errors.New("needed config data file, but none was provided")
			}
			filePath, err := filepath.Abs(options.ConfigFile)
			if err != nil {
				return "", fmt.Errorf("recording artifact config data file %q: %w", options.ConfigFile, err)
			}
			digester := digest.Canonical.Digester()
			counter := ioutils.NewWriteCounter(digester.Hash())
			if err := func() error {
				f, err := os.Open(filePath)
				if err != nil {
					return fmt.Errorf("reading artifact config data file %q: %w", options.ConfigFile, err)
				}
				defer f.Close()
				if _, err := io.Copy(counter, f); err != nil {
					return fmt.Errorf("digesting artifact config data file %q: %w", options.ConfigFile, err)
				}
				return nil
			}(); err != nil {
				return "", err
			}
			configDescriptor.Data = nil
			configDescriptor.Size = counter.Count
			configDescriptor.Digest = digester.Digest()
			configFilePath = filePath
		} else {
			decoder := bytes.NewReader(configDescriptor.Data)
			digester := digest.Canonical.Digester()
			counter := ioutils.NewWriteCounter(digester.Hash())
			if _, err := io.Copy(counter, decoder); err != nil {
				return "", fmt.Errorf("digesting inlined artifact config data: %w", err)
			}
			configDescriptor.Size = counter.Count
			configDescriptor.Digest = digester.Digest()
		}
	} else {
		configDescriptor.Data = nil
		configDescriptor.Digest = digest.Canonical.FromString("")
	}

	// Construct the manifest.
	artifactManifest := v1.Manifest{
		Versioned: imgspec.Versioned{
			SchemaVersion: 2,
		},
		MediaType:    v1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       *configDescriptor,
		Layers:       layers,
		Subject:      subject,
	}
	// Add in annotations, more or less exactly as specified.
	artifactManifest.Annotations = maps.Clone(options.Annotations)

	// Encode and save the data we care about.
	artifactManifestBytes, err := json.Marshal(artifactManifest)
	if err != nil {
		return "", fmt.Errorf("marshalling the artifact manifest: %w", err)
	}
	artifactManifestDigest, err := manifest.Digest(artifactManifestBytes)
	if err != nil {
		return "", fmt.Errorf("digesting the artifact manifest: %w", err)
	}
	l.artifacts.Manifests[artifactManifestDigest] = string(artifactManifestBytes)
	l.artifacts.Layers[artifactManifestDigest] = nil
	l.artifacts.Configs[artifactManifestDigest] = artifactManifest.Config.Digest
	if configFilePath != "" {
		l.artifacts.Detached[artifactManifest.Config.Digest] = configFilePath
		l.artifacts.Files[artifactManifestDigest] = append(l.artifacts.Files[artifactManifestDigest], configFilePath)
	} else {
		l.artifacts.Blobs[artifactManifest.Config.Digest] = slices.Clone(artifactManifest.Config.Data)
	}
	for filePath, fileDigest := range fileDigests {
		l.artifacts.Layers[artifactManifestDigest] = append(l.artifacts.Layers[artifactManifestDigest], fileDigest)
		l.artifacts.Detached[fileDigest] = filePath
		l.artifacts.Files[artifactManifestDigest] = append(l.artifacts.Files[artifactManifestDigest], filePath)
	}
	for _, layer := range layers {
		if len(layer.Data) != 0 {
			l.artifacts.Blobs[layer.Digest] = slices.Clone(layer.Data)
			l.artifacts.Layers[artifactManifestDigest] = append(l.artifacts.Layers[artifactManifestDigest], layer.Digest)
		}
	}
	// Add this artifact manifest to the image index.
	if err := l.AddInstance(artifactManifestDigest, int64(len(artifactManifestBytes)), artifactManifest.MediaType, options.Platform.OS, options.Platform.Architecture, options.Platform.OSVersion, options.Platform.OSFeatures, options.Platform.Variant, nil, nil); err != nil {
		return "", fmt.Errorf("adding artifact manifest for %q to image index: %w", files, err)
	}
	// Set the artifact type in the image index entry if we have one, since AddInstance() didn't do that for us.
	if artifactManifest.ArtifactType != "" {
		if err := l.List.SetArtifactType(&artifactManifestDigest, artifactManifest.ArtifactType); err != nil {
			return "", fmt.Errorf("adding artifact manifest for %q to image index: %w", files, err)
		}
	}
	return artifactManifestDigest, nil
}

// Remove filters out any instances in the list which match the specified digest.
func (l *list) Remove(instanceDigest digest.Digest) error {
	err := l.List.Remove(instanceDigest)
	if err == nil {
		delete(l.instances, instanceDigest)
	}
	return err
}

// LockerForImage returns a Locker for a given image record.  It's recommended
// that processes which use LoadFromImage() to load a list from an image and
// then use that list's SaveToImage() method to save a modified version of the
// list to that image record use this lock to avoid accidentally wiping out
// changes that another process is also attempting to make.
func LockerForImage(store storage.Store, image string) (lockfile.Locker, error) { // nolint:staticcheck
	img, err := store.Image(image)
	if err != nil {
		return nil, fmt.Errorf("locating image %q for locating lock: %w", image, err)
	}
	d := digest.NewDigestFromEncoded(digest.Canonical, img.ID)
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("coercing image ID for %q into a digest: %w", image, err)
	}
	return store.GetDigestLock(d)
}
