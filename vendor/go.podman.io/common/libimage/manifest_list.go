//go:build !remote

package libimage

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	structcopier "github.com/jinzhu/copier"
	"github.com/opencontainers/go-digest"
	imgspec "github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage/define"
	"go.podman.io/common/libimage/manifests"
	manifesterrors "go.podman.io/common/pkg/manifests"
	"go.podman.io/common/pkg/supplemented"
	imageCopy "go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

// NOTE: the abstractions and APIs here are a first step to further merge
// `libimage/manifests` into `libimage`.

// ErrNotAManifestList indicates that an image was found in the local
// containers storage but it is not a manifest list as requested.
var ErrNotAManifestList = errors.New("image is not a manifest list")

// ManifestList represents a manifest list (Docker) or an image index (OCI) in
// the local containers storage.
type ManifestList struct {
	// NOTE: the *List* suffix is intentional as the term "manifest" is
	// used ambiguously across the ecosystem.  It may refer to the (JSON)
	// manifest of an ordinary image OR to a manifest *list* (Docker) or to
	// image index (OCI).
	// It's a bit more work when typing but without ambiguity.

	// The underlying image in the containers storage.
	image *Image

	// The underlying manifest list.
	list manifests.List
}

// ID returns the ID of the manifest list.
func (m *ManifestList) ID() string {
	return m.image.ID()
}

// CreateManifestList creates a new empty manifest list with the specified
// name.
func (r *Runtime) CreateManifestList(name string) (*ManifestList, error) {
	normalized, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}

	list := manifests.Create()
	listID, err := list.SaveToImage(r.store, "", []string{normalized.String()}, manifest.DockerV2ListMediaType)
	if err != nil {
		return nil, err
	}

	mList, err := r.LookupManifestList(listID)
	if err != nil {
		return nil, err
	}

	return mList, nil
}

// LookupManifestList looks up a manifest list with the specified name in the
// containers storage.
func (r *Runtime) LookupManifestList(name string) (*ManifestList, error) {
	image, list, err := r.lookupManifestList(name)
	if err != nil {
		return nil, err
	}
	return &ManifestList{image: image, list: list}, nil
}

func (r *Runtime) lookupManifestList(name string) (*Image, manifests.List, error) {
	lookupOptions := &LookupImageOptions{
		lookupManifest: true,
	}
	image, _, err := r.LookupImage(name, lookupOptions)
	if err != nil {
		return nil, nil, err
	}
	if err := image.reload(); err != nil {
		return nil, nil, err
	}
	list, err := image.getManifestList()
	if err != nil {
		return nil, nil, err
	}
	return image, list, nil
}

// ConvertToManifestList converts the image into a manifest list if it is not
// already also a list.  An error is returned if the conversion fails.
func (i *Image) ConvertToManifestList(ctx context.Context) (*ManifestList, error) {
	// If we don't need to do anything, don't do anything.
	if list, err := i.ToManifestList(); err == nil || !errors.Is(err, ErrNotAManifestList) {
		return list, err
	}

	// Determine which type we prefer for the new manifest list or image index.
	_, imageManifestType, err := i.Manifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the image's manifest: %w", err)
	}
	var preferredListType string
	switch imageManifestType {
	case manifest.DockerV2Schema2MediaType,
		manifest.DockerV2Schema1SignedMediaType,
		manifest.DockerV2Schema1MediaType,
		manifest.DockerV2ListMediaType:
		preferredListType = manifest.DockerV2ListMediaType
	case imgspecv1.MediaTypeImageManifest, imgspecv1.MediaTypeImageIndex:
		preferredListType = imgspecv1.MediaTypeImageIndex
	default:
		preferredListType = ""
	}

	// Create a list and add the image's manifest to it.  Use OCI format
	// for now.  If we need to convert it to Docker format, we'll do that
	// while copying it.
	list := manifests.Create()
	if _, err := list.Add(ctx, &i.runtime.systemContext, i.storageReference, false); err != nil {
		return nil, fmt.Errorf("generating new image index: %w", err)
	}
	listBytes, err := list.Serialize(imgspecv1.MediaTypeImageIndex)
	if err != nil {
		return nil, fmt.Errorf("serializing image index: %w", err)
	}
	listDigest, err := manifest.Digest(listBytes)
	if err != nil {
		return nil, fmt.Errorf("digesting image index: %w", err)
	}

	// Build an OCI layout containing the image index as the only item.
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("serializing initial list: %w", err)
	}
	defer os.RemoveAll(tmp)

	// Drop our image index in there.
	if err := os.Mkdir(filepath.Join(tmp, imgspecv1.ImageBlobsDir), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for blobs: %w", err)
	}
	if err := os.Mkdir(filepath.Join(tmp, imgspecv1.ImageBlobsDir, listDigest.Algorithm().String()), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory for %s blobs: %w", listDigest.Algorithm().String(), err)
	}
	listFile := filepath.Join(tmp, imgspecv1.ImageBlobsDir, listDigest.Algorithm().String(), listDigest.Encoded())
	if err := os.WriteFile(listFile, listBytes, 0o644); err != nil {
		return nil, fmt.Errorf("writing image index for OCI layout: %w", err)
	}

	// Build the index for the layout.
	index := imgspecv1.Index{
		Versioned: imgspec.Versioned{
			SchemaVersion: 2,
		},
		MediaType: imgspecv1.MediaTypeImageIndex,
		Manifests: []imgspecv1.Descriptor{{
			MediaType: imgspecv1.MediaTypeImageIndex,
			Digest:    listDigest,
			Size:      int64(len(listBytes)),
		}},
	}
	indexBytes, err := json.Marshal(&index)
	if err != nil {
		return nil, fmt.Errorf("encoding image index for OCI layout: %w", err)
	}

	// Write the index for the layout.
	indexFile := filepath.Join(tmp, imgspecv1.ImageIndexFile)
	if err := os.WriteFile(indexFile, indexBytes, 0o644); err != nil {
		return nil, fmt.Errorf("writing top-level index for OCI layout: %w", err)
	}

	// Write the "why yes, this is an OCI layout" file.
	layoutFile := filepath.Join(tmp, imgspecv1.ImageLayoutFile)
	layoutBytes, err := json.Marshal(imgspecv1.ImageLayout{Version: imgspecv1.ImageLayoutVersion})
	if err != nil {
		return nil, fmt.Errorf("encoding image layout structure for OCI layout: %w", err)
	}
	if err := os.WriteFile(layoutFile, layoutBytes, 0o644); err != nil {
		return nil, fmt.Errorf("writing oci-layout file: %w", err)
	}

	// Build an OCI layout reference to use as a source.
	tmpRef, err := layout.NewReference(tmp, "")
	if err != nil {
		return nil, fmt.Errorf("creating reference to directory: %w", err)
	}
	bundle := supplemented.Reference(tmpRef, []types.ImageReference{i.storageReference}, imageCopy.CopySystemImage, nil)

	// Build a policy that ensures we don't prevent ourselves from reading
	// this reference.
	signaturePolicy, err := signature.DefaultPolicy(&i.runtime.systemContext)
	if err != nil {
		return nil, fmt.Errorf("obtaining default signature policy: %w", err)
	}
	acceptAnything := signature.PolicyTransportScopes{
		"": []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()},
	}
	signaturePolicy.Transports[i.storageReference.Transport().Name()] = acceptAnything
	signaturePolicy.Transports[tmpRef.Transport().Name()] = acceptAnything
	policyContext, err := signature.NewPolicyContext(signaturePolicy)
	if err != nil {
		return nil, fmt.Errorf("creating new signature policy context: %w", err)
	}
	defer func() {
		if err2 := policyContext.Destroy(); err2 != nil {
			logrus.Errorf("Destroying signature policy context: %v", err2)
		}
	}()

	// Copy from the OCI layout into the same image record, so that it gets
	// both its own manifest and the image index.
	copyOptions := imageCopy.Options{
		ForceManifestMIMEType: imageManifestType,
	}
	if _, err := imageCopy.Image(ctx, policyContext, i.storageReference, bundle, &copyOptions); err != nil {
		return nil, fmt.Errorf("writing updates to image: %w", err)
	}

	// Now explicitly write the list's manifest to the image as its "main"
	// manifest.
	if _, err := list.SaveToImage(i.runtime.store, i.ID(), i.storageImage.Names, preferredListType); err != nil {
		return nil, fmt.Errorf("saving image index: %w", err)
	}

	// Reload the record.
	if err = i.reload(); err != nil {
		return nil, fmt.Errorf("reloading image record: %w", err)
	}
	mList, err := i.runtime.LookupManifestList(i.storageImage.ID)
	if err != nil {
		return nil, fmt.Errorf("looking up new manifest list: %w", err)
	}

	return mList, nil
}

// ToManifestList converts the image into a manifest list.  An error is thrown
// if the image is not a manifest list.
func (i *Image) ToManifestList() (*ManifestList, error) {
	list, err := i.getManifestList()
	if err != nil {
		return nil, err
	}
	return &ManifestList{image: i, list: list}, nil
}

// LookupInstance looks up an instance of the manifest list matching the
// specified platform.  The local machine's platform is used if left empty.
func (m *ManifestList) LookupInstance(ctx context.Context, architecture, os, variant string) (*Image, error) {
	sys := m.image.runtime.systemContextCopy()
	if architecture != "" {
		sys.ArchitectureChoice = architecture
	}
	if os != "" {
		sys.OSChoice = os
	}
	if architecture != "" {
		sys.VariantChoice = variant
	}

	// Now look at the *manifest* and select a matching instance.
	rawManifest, manifestType, err := m.image.Manifest(ctx)
	if err != nil {
		return nil, err
	}
	list, err := manifest.ListFromBlob(rawManifest, manifestType)
	if err != nil {
		return nil, err
	}
	instanceDigest, err := list.ChooseInstance(sys)
	if err != nil {
		return nil, err
	}

	allImages, err := m.image.runtime.ListImages(ctx, nil)
	if err != nil {
		return nil, err
	}

	for _, image := range allImages {
		if slices.Contains(image.Digests(), instanceDigest) || instanceDigest == image.Digest() {
			return image, nil
		}
	}

	return nil, fmt.Errorf("could not find image instance %s of manifest list %s in local containers storage: %w", instanceDigest, m.ID(), storage.ErrImageUnknown)
}

// Saves the specified manifest list and reloads it from storage with the new ID.
func (m *ManifestList) saveAndReload() error {
	newID, err := m.list.SaveToImage(m.image.runtime.store, m.image.ID(), nil, "")
	if err != nil {
		return err
	}
	return m.reloadID(newID)
}

// Reload the image and list instances from storage.
func (m *ManifestList) reload() error {
	listID := m.ID()
	return m.reloadID(listID)
}

func (m *ManifestList) reloadID(listID string) error {
	image, list, err := m.image.runtime.lookupManifestList(listID)
	if err != nil {
		return err
	}
	m.image = image
	m.list = list
	return nil
}

// getManifestList is a helper to obtain a manifest list.
func (i *Image) getManifestList() (manifests.List, error) {
	_, list, err := manifests.LoadFromImage(i.runtime.store, i.ID())
	if errors.Is(err, manifesterrors.ErrManifestTypeNotSupported) {
		err = fmt.Errorf("%s: %w", err.Error(), ErrNotAManifestList)
	}
	return list, err
}

// IsManifestList returns true if the image is a manifest list (Docker) or an
// image index (OCI).  This information may be critical to make certain
// execution paths more robust (e.g., suppress certain errors).
func (i *Image) IsManifestList(ctx context.Context) (bool, error) {
	// FIXME: due to `ImageDigestBigDataKey` we'll always check the
	// _last-written_ manifest which is causing issues for multi-arch image
	// pulls.
	//
	// See https://github.com/containers/common/pull/1505#discussion_r1242677279.
	ref, err := i.StorageReference()
	if err != nil {
		return false, err
	}
	imgSrc, err := ref.NewImageSource(ctx, i.runtime.systemContextCopy())
	if err != nil {
		return false, err
	}
	defer imgSrc.Close()
	_, manifestType, err := image.UnparsedInstance(imgSrc, nil).Manifest(ctx)
	if err != nil {
		return false, err
	}
	return manifest.MIMETypeIsMultiImage(manifestType), nil
}

// Inspect returns a dockerized version of the manifest list.
func (m *ManifestList) Inspect() (*define.ManifestListData, error) {
	inspectList := define.ManifestListData{}
	// Copy the fields from the Docker-format version of the list.
	dockerFormat := m.list.Docker()
	err := structcopier.Copy(&inspectList, &dockerFormat)
	if err != nil {
		return &inspectList, err
	}
	// Get OCI-specific fields from the OCIv1-format version of the list
	// and copy them to the inspect data.
	ociFormat := m.list.OCIv1()
	inspectList.ArtifactType = ociFormat.ArtifactType
	inspectList.Annotations = ociFormat.Annotations
	for i, manifest := range ociFormat.Manifests {
		inspectList.Manifests[i].Annotations = manifest.Annotations
		inspectList.Manifests[i].ArtifactType = manifest.ArtifactType
		inspectList.Manifests[i].URLs = slices.Clone(manifest.URLs)
		inspectList.Manifests[i].Data = manifest.Data
		inspectList.Manifests[i].Files, err = m.list.Files(manifest.Digest)
		if err != nil {
			return &inspectList, err
		}
	}
	if ociFormat.Subject != nil {
		platform := ociFormat.Subject.Platform
		if platform == nil {
			platform = &imgspecv1.Platform{}
		}
		osFeatures := slices.Clone(platform.OSFeatures)
		inspectList.Subject = &define.ManifestListDescriptor{
			Platform: manifest.Schema2PlatformSpec{
				OS:           platform.OS,
				Architecture: platform.Architecture,
				OSVersion:    platform.OSVersion,
				Variant:      platform.Variant,
				OSFeatures:   osFeatures,
			},
			Schema2Descriptor: manifest.Schema2Descriptor{
				MediaType: ociFormat.Subject.MediaType,
				Digest:    ociFormat.Subject.Digest,
				Size:      ociFormat.Subject.Size,
				URLs:      ociFormat.Subject.URLs,
			},
			Annotations:  ociFormat.Subject.Annotations,
			ArtifactType: ociFormat.Subject.ArtifactType,
			Data:         ociFormat.Subject.Data,
		}
	}
	// Set MediaType to mirror the value we'd use when saving the list
	// using defaults, instead of forcing it to one or the other by
	// using the value from one version or the other that we explicitly
	// requested above.
	serialized, err := m.list.Serialize("")
	if err != nil {
		return &inspectList, err
	}
	var typed struct {
		MediaType string `json:"mediaType,omitempty"`
	}
	if err := json.Unmarshal(serialized, &typed); err != nil {
		return &inspectList, err
	}
	if typed.MediaType != "" {
		inspectList.MediaType = typed.MediaType
	}
	return &inspectList, nil
}

// ManifestListAddOptions for adding an image or artifact to a manifest list.
type ManifestListAddOptions struct {
	// Add all images to the list if the to-be-added image itself is a
	// manifest list.
	All bool `json:"all"`
	// containers-auth.json(5) file to use when authenticating against
	// container registries.
	AuthFilePath string
	// Path to the certificates directory.
	CertDirPath string
	// Allow contacting registries over HTTP, or HTTPS with failed TLS
	// verification. Note that this does not affect other TLS connections.
	InsecureSkipTLSVerify types.OptionalBool
	// Username to use when authenticating at a container registry.
	Username string
	// Password to use when authenticating at a container registry.
	Password string
}

func (m *ManifestList) parseNameToExtantReference(ctx context.Context, sys *types.SystemContext, name string, manifestList bool, what string) (types.ImageReference, error) {
	ref, err := alltransports.ParseImageName(name)
	if err != nil {
		withDocker := fmt.Sprintf("%s://%s", docker.Transport.Name(), name)
		ref, err = alltransports.ParseImageName(withDocker)
		if err == nil {
			var src types.ImageSource
			src, err = ref.NewImageSource(ctx, sys)
			if err == nil {
				src.Close()
			}
		}
		if err != nil {
			image, _, lookupErr := m.image.runtime.LookupImage(name, &LookupImageOptions{ManifestList: manifestList})
			if lookupErr != nil {
				return nil, fmt.Errorf("locating %s: %q: %w; %q: %w", what, withDocker, err, name, lookupErr)
			}
			ref, err = image.storageReference, nil
		}
	}
	return ref, err
}

// Add adds one or more manifests to the manifest list and returns the digest
// of the added instance.
func (m *ManifestList) Add(ctx context.Context, name string, options *ManifestListAddOptions) (digest.Digest, error) {
	if options == nil {
		options = &ManifestListAddOptions{}
	}

	// Now massage in the copy-related options into the system context.
	systemContext := m.image.runtime.systemContextCopy()
	if options.AuthFilePath != "" {
		systemContext.AuthFilePath = options.AuthFilePath
	}
	if options.CertDirPath != "" {
		systemContext.DockerCertPath = options.CertDirPath
	}
	if options.InsecureSkipTLSVerify != types.OptionalBoolUndefined {
		systemContext.DockerInsecureSkipTLSVerify = options.InsecureSkipTLSVerify
		systemContext.OCIInsecureSkipTLSVerify = options.InsecureSkipTLSVerify == types.OptionalBoolTrue
		systemContext.DockerDaemonInsecureSkipTLSVerify = options.InsecureSkipTLSVerify == types.OptionalBoolTrue
	}
	if options.Username != "" {
		systemContext.DockerAuthConfig = &types.DockerAuthConfig{
			Username: options.Username,
			Password: options.Password,
		}
	}

	ref, err := m.parseNameToExtantReference(ctx, systemContext, name, false, "image to add to manifest list")
	if err != nil {
		return "", err
	}

	locker, err := manifests.LockerForImage(m.image.runtime.store, m.ID())
	if err != nil {
		return "", err
	}
	locker.Lock()
	defer locker.Unlock()
	// Make sure to reload the image from the containers storage to fetch
	// the latest data (e.g., new or delete digests).
	if err := m.reload(); err != nil {
		return "", err
	}
	newDigest, err := m.list.Add(ctx, systemContext, ref, options.All)
	if err != nil {
		return "", err
	}

	// Write the changes to disk.
	if err := m.saveAndReload(); err != nil {
		return "", err
	}
	return newDigest, nil
}

// ManifestListAddArtifactOptions used for creating an artifact manifest for one or more
// files and adding the artifact manifest to a manifest list.
type ManifestListAddArtifactOptions struct {
	// The artifactType to set in the artifact manifest.
	Type *string `json:"artifact_type"`
	// The mediaType to set in the config.MediaType field in the artifact manifest.
	ConfigType string `json:"artifact_config_type"`
	// Content to point to from the config field in the artifact manifest.
	Config string `json:"artifact_config"`
	// The mediaType to set in the layer descriptors in the artifact manifest.
	LayerType string `json:"artifact_layer_type"`
	// Whether or not to suppress the org.opencontainers.image.title annotation in layer descriptors.
	ExcludeTitles bool `json:"exclude_layer_titles"`
	// Annotations to set in the artifact manifest.
	Annotations map[string]string `json:"annotations"`
	// Subject to set in the artifact manifest.
	Subject string `json:"subject"`
}

// AddArtifact adds one or more manifests to the manifest list and returns the digest
// of the added instance.
func (m *ManifestList) AddArtifact(ctx context.Context, options *ManifestListAddArtifactOptions, files ...string) (digest.Digest, error) {
	if options == nil {
		options = &ManifestListAddArtifactOptions{}
	}
	opts := manifests.AddArtifactOptions{
		ManifestArtifactType: options.Type,
		Annotations:          maps.Clone(options.Annotations),
		ExcludeTitles:        options.ExcludeTitles,
	}
	if options.ConfigType != "" {
		opts.ConfigDescriptor = &imgspecv1.Descriptor{
			MediaType: options.ConfigType,
			Digest:    imgspecv1.DescriptorEmptyJSON.Digest,
			Size:      imgspecv1.DescriptorEmptyJSON.Size,
			Data:      slices.Clone(imgspecv1.DescriptorEmptyJSON.Data),
		}
	}
	if options.Config != "" {
		if opts.ConfigDescriptor == nil {
			opts.ConfigDescriptor = &imgspecv1.Descriptor{
				MediaType: imgspecv1.MediaTypeImageConfig,
			}
		}
		opts.ConfigDescriptor.Digest = digest.FromString(options.Config)
		opts.ConfigDescriptor.Size = int64(len(options.Config))
		opts.ConfigDescriptor.Data = slices.Clone([]byte(options.Config))
	}
	if opts.ConfigDescriptor == nil {
		empty := imgspecv1.DescriptorEmptyJSON
		opts.ConfigDescriptor = &empty
	}
	if options.LayerType != "" {
		opts.LayerMediaType = &options.LayerType
	}
	if options.Subject != "" {
		ref, err := m.parseNameToExtantReference(ctx, nil, options.Subject, true, "subject for artifact manifest")
		if err != nil {
			return "", err
		}
		opts.SubjectReference = ref
	}

	// Lock the image record where this list lives.
	locker, err := manifests.LockerForImage(m.image.runtime.store, m.ID())
	if err != nil {
		return "", err
	}
	locker.Lock()
	defer locker.Unlock()

	systemContext := m.image.runtime.systemContextCopy()

	// Make sure to reload the image from the containers storage to fetch
	// the latest data (e.g., new or delete digests).
	if err := m.reload(); err != nil {
		return "", err
	}
	newDigest, err := m.list.AddArtifact(ctx, systemContext, opts, files...)
	if err != nil {
		return "", err
	}

	// Write the changes to disk.
	if err := m.saveAndReload(); err != nil {
		return "", err
	}
	return newDigest, nil
}

// ManifestListAnnotateOptions used for annotating a manifest list.
type ManifestListAnnotateOptions struct {
	// Add the specified annotations to the added image.  Empty values are ignored.
	Annotations map[string]string
	// Add the specified architecture to the added image.  Empty values are ignored.
	Architecture string
	// Add the specified features to the added image.  Empty values are ignored.
	Features []string
	// Add the specified OS to the added image.  Empty values are ignored.
	OS string
	// Add the specified OS features to the added image.  Empty values are ignored.
	OSFeatures []string
	// Add the specified OS version to the added image.  Empty values are ignored.
	OSVersion string
	// Add the specified variant to the added image.  Empty values are ignored unless Architecture is set to a non-empty value.
	Variant string
	// Add the specified annotations to the index itself.  Empty values are ignored.
	IndexAnnotations map[string]string
	// Set the subject to which the index refers.  Empty values are ignored.
	Subject string
}

// AnnotateInstance annotates an image instance specified by `d` in the manifest list.
func (m *ManifestList) AnnotateInstance(d digest.Digest, options *ManifestListAnnotateOptions) error {
	ctx := context.Background()

	if options == nil {
		return nil
	}

	locker, err := manifests.LockerForImage(m.image.runtime.store, m.ID())
	if err != nil {
		return err
	}
	locker.Lock()
	defer locker.Unlock()
	// Make sure to reload the image from the containers storage to fetch
	// the latest data (e.g., new or delete digests).
	if err := m.reload(); err != nil {
		return err
	}

	if len(options.OS) > 0 {
		if err := m.list.SetOS(d, options.OS); err != nil {
			return err
		}
	}
	if len(options.OSVersion) > 0 {
		if err := m.list.SetOSVersion(d, options.OSVersion); err != nil {
			return err
		}
	}
	if len(options.Features) > 0 {
		if err := m.list.SetFeatures(d, options.Features); err != nil {
			return err
		}
	}
	if len(options.OSFeatures) > 0 {
		if err := m.list.SetOSFeatures(d, options.OSFeatures); err != nil {
			return err
		}
	}
	if len(options.Architecture) > 0 {
		if err := m.list.SetArchitecture(d, options.Architecture); err != nil {
			return err
		}
	}
	if len(options.Architecture) != 0 || len(options.Variant) > 0 {
		if err := m.list.SetVariant(d, options.Variant); err != nil {
			return err
		}
	}
	if len(options.Annotations) > 0 {
		if err := m.list.SetAnnotations(&d, options.Annotations); err != nil {
			return err
		}
	}
	if len(options.IndexAnnotations) > 0 {
		if err := m.list.SetAnnotations(nil, options.IndexAnnotations); err != nil {
			return err
		}
	}
	if options.Subject != "" {
		ref, err := m.parseNameToExtantReference(ctx, nil, options.Subject, true, "subject for image index")
		if err != nil {
			return err
		}
		src, err := ref.NewImageSource(ctx, &m.image.runtime.systemContext)
		if err != nil {
			return err
		}
		defer src.Close()
		subjectManifestBytes, subjectManifestType, err := image.UnparsedInstance(src, nil).Manifest(ctx)
		if err != nil {
			return err
		}
		subjectManifestDigest, err := manifest.Digest(subjectManifestBytes)
		if err != nil {
			return err
		}
		var subjectArtifactType string
		if !manifest.MIMETypeIsMultiImage(subjectManifestType) {
			var subjectManifest imgspecv1.Manifest
			if json.Unmarshal(subjectManifestBytes, &subjectManifest) == nil {
				subjectArtifactType = subjectManifest.ArtifactType
			}
		}
		descriptor := &imgspecv1.Descriptor{
			MediaType:    subjectManifestType,
			ArtifactType: subjectArtifactType,
			Digest:       subjectManifestDigest,
			Size:         int64(len(subjectManifestBytes)),
		}
		if err := m.list.SetSubject(descriptor); err != nil {
			return err
		}
	}

	// Write the changes to disk.
	return m.saveAndReload()
}

// RemoveInstance removes the instance specified by `d` from the manifest list.
// Returns the new ID of the image.
func (m *ManifestList) RemoveInstance(d digest.Digest) error {
	locker, err := manifests.LockerForImage(m.image.runtime.store, m.ID())
	if err != nil {
		return err
	}
	locker.Lock()
	defer locker.Unlock()
	// Make sure to reload the image from the containers storage to fetch
	// the latest data (e.g., new or delete digests).
	if err := m.reload(); err != nil {
		return err
	}

	if err := m.list.Remove(d); err != nil {
		return err
	}

	// Write the changes to disk.
	return m.saveAndReload()
}

// ManifestListPushOptions allow for customizing pushing a manifest list.
type ManifestListPushOptions struct {
	CopyOptions

	// For tweaking the list selection.
	ImageListSelection imageCopy.ImageListSelection
	// Use when selecting only specific imags.
	Instances []digest.Digest
	// Add existing instances with requested compression algorithms to manifest list
	AddCompression []string
}

// Push pushes a manifest to the specified destination.
func (m *ManifestList) Push(ctx context.Context, destination string, options *ManifestListPushOptions) (digest.Digest, error) {
	if options == nil {
		options = &ManifestListPushOptions{}
	}

	dest, err := alltransports.ParseImageName(destination)
	if err != nil {
		oldErr := err
		dest, err = alltransports.ParseImageName("docker://" + destination)
		if err != nil {
			return "", oldErr
		}
	}

	if m.image.runtime.eventChannel != nil {
		defer m.image.runtime.writeEvent(&Event{ID: m.ID(), Name: destination, Time: time.Now(), Type: EventTypeImagePush})
	}

	// NOTE: we're using the logic in copier to create a proper
	// types.SystemContext. This prevents us from having an error prone
	// code duplicate here.
	copier, err := m.image.runtime.newCopier(&options.CopyOptions)
	if err != nil {
		return "", err
	}
	defer copier.Close()

	pushOptions := manifests.PushOptions{
		AddCompression:                   options.AddCompression,
		Store:                            m.image.runtime.store,
		SystemContext:                    copier.systemContext,
		ImageListSelection:               options.ImageListSelection,
		Instances:                        options.Instances,
		ReportWriter:                     options.Writer,
		Signers:                          options.Signers,
		SignBy:                           options.SignBy,
		SignPassphrase:                   options.SignPassphrase,
		SignBySigstorePrivateKeyFile:     options.SignBySigstorePrivateKeyFile,
		SignSigstorePrivateKeyPassphrase: options.SignSigstorePrivateKeyPassphrase,
		RemoveSignatures:                 options.RemoveSignatures,
		ManifestType:                     options.ManifestMIMEType,
		MaxRetries:                       options.MaxRetries,
		RetryDelay:                       options.RetryDelay,
		ForceCompressionFormat:           options.ForceCompressionFormat,
	}

	_, d, err := m.list.Push(ctx, dest, pushOptions)
	return d, err
}
