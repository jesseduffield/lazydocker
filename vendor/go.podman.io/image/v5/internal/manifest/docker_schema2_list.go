package manifest

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	platform "go.podman.io/image/v5/internal/pkg/platform"
	compression "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

// Schema2PlatformSpec describes the platform which a particular manifest is
// specialized for.
// This is publicly visible as c/image/manifest.Schema2PlatformSpec.
type Schema2PlatformSpec struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
	Variant      string   `json:"variant,omitempty"`
	Features     []string `json:"features,omitempty"` // removed in OCI
}

// Schema2ManifestDescriptor references a platform-specific manifest.
// This is publicly visible as c/image/manifest.Schema2ManifestDescriptor.
type Schema2ManifestDescriptor struct {
	Schema2Descriptor
	Platform Schema2PlatformSpec `json:"platform"`
}

// Schema2ListPublic is a list of platform-specific manifests.
// This is publicly visible as c/image/manifest.Schema2List.
// Internal users should usually use Schema2List instead.
type Schema2ListPublic struct {
	SchemaVersion int                         `json:"schemaVersion"`
	MediaType     string                      `json:"mediaType"`
	Manifests     []Schema2ManifestDescriptor `json:"manifests"`
}

// MIMEType returns the MIME type of this particular manifest list.
func (list *Schema2ListPublic) MIMEType() string {
	return list.MediaType
}

// Instances returns a slice of digests of the manifests that this list knows of.
func (list *Schema2ListPublic) Instances() []digest.Digest {
	results := make([]digest.Digest, len(list.Manifests))
	for i, m := range list.Manifests {
		results[i] = m.Digest
	}
	return results
}

// Instance returns the ListUpdate of a particular instance in the list.
func (list *Schema2ListPublic) Instance(instanceDigest digest.Digest) (ListUpdate, error) {
	for _, manifest := range list.Manifests {
		if manifest.Digest == instanceDigest {
			ret := ListUpdate{
				Digest:    manifest.Digest,
				Size:      manifest.Size,
				MediaType: manifest.MediaType,
			}
			ret.ReadOnly.CompressionAlgorithmNames = []string{compression.GzipAlgorithmName}
			platform := ociPlatformFromSchema2PlatformSpec(manifest.Platform)
			ret.ReadOnly.Platform = &platform
			return ret, nil
		}
	}
	return ListUpdate{}, fmt.Errorf("unable to find instance %s passed to Schema2List.Instances", instanceDigest)
}

// UpdateInstances updates the sizes, digests, and media types of the manifests
// which the list catalogs.
func (list *Schema2ListPublic) UpdateInstances(updates []ListUpdate) error {
	editInstances := []ListEdit{}
	for i, instance := range updates {
		editInstances = append(editInstances, ListEdit{
			UpdateOldDigest: list.Manifests[i].Digest,
			UpdateDigest:    instance.Digest,
			UpdateSize:      instance.Size,
			UpdateMediaType: instance.MediaType,
			ListOperation:   ListOpUpdate})
	}
	return list.editInstances(editInstances)
}

func (list *Schema2ListPublic) editInstances(editInstances []ListEdit) error {
	addedEntries := []Schema2ManifestDescriptor{}
	for i, editInstance := range editInstances {
		switch editInstance.ListOperation {
		case ListOpUpdate:
			if err := editInstance.UpdateOldDigest.Validate(); err != nil {
				return fmt.Errorf("Schema2List.EditInstances: Attempting to update %s which is an invalid digest: %w", editInstance.UpdateOldDigest, err)
			}
			if err := editInstance.UpdateDigest.Validate(); err != nil {
				return fmt.Errorf("Schema2List.EditInstances: Modified digest %s is an invalid digest: %w", editInstance.UpdateDigest, err)
			}
			targetIndex := slices.IndexFunc(list.Manifests, func(m Schema2ManifestDescriptor) bool {
				return m.Digest == editInstance.UpdateOldDigest
			})
			if targetIndex == -1 {
				return fmt.Errorf("Schema2List.EditInstances: digest %s not found", editInstance.UpdateOldDigest)
			}
			list.Manifests[targetIndex].Digest = editInstance.UpdateDigest
			if editInstance.UpdateSize < 0 {
				return fmt.Errorf("update %d of %d passed to Schema2List.UpdateInstances had an invalid size (%d)", i+1, len(editInstances), editInstance.UpdateSize)
			}
			list.Manifests[targetIndex].Size = editInstance.UpdateSize
			if editInstance.UpdateMediaType == "" {
				return fmt.Errorf("update %d of %d passed to Schema2List.UpdateInstances had no media type (was %q)", i+1, len(editInstances), list.Manifests[i].MediaType)
			}
			list.Manifests[targetIndex].MediaType = editInstance.UpdateMediaType
		case ListOpAdd:
			if editInstance.AddPlatform == nil {
				// Should we create a struct with empty fields instead?
				// Right now ListOpAdd is only called when an instance with the same platform value
				// already exists in the manifest, so this should not be reached in practice.
				return fmt.Errorf("adding a schema2 list instance with no platform specified is not supported")
			}
			addedEntries = append(addedEntries, Schema2ManifestDescriptor{
				Schema2Descriptor{
					Digest:    editInstance.AddDigest,
					Size:      editInstance.AddSize,
					MediaType: editInstance.AddMediaType,
				},
				schema2PlatformSpecFromOCIPlatform(*editInstance.AddPlatform),
			})
		default:
			return fmt.Errorf("internal error: invalid operation: %d", editInstance.ListOperation)
		}
	}
	if len(addedEntries) != 0 {
		// slices.Clone() here to ensure a private backing array;
		// an external caller could have manually created Schema2ListPublic with a slice with extra capacity.
		list.Manifests = append(slices.Clone(list.Manifests), addedEntries...)
	}
	return nil
}

func (list *Schema2List) EditInstances(editInstances []ListEdit) error {
	return list.editInstances(editInstances)
}

func (list *Schema2ListPublic) ChooseInstanceByCompression(ctx *types.SystemContext, preferGzip types.OptionalBool) (digest.Digest, error) {
	// ChooseInstanceByCompression is same as ChooseInstance for schema2 manifest list.
	return list.ChooseInstance(ctx)
}

// ChooseInstance parses blob as a schema2 manifest list, and returns the digest
// of the image which is appropriate for the current environment.
func (list *Schema2ListPublic) ChooseInstance(ctx *types.SystemContext) (digest.Digest, error) {
	wantedPlatforms := platform.WantedPlatforms(ctx)
	for _, wantedPlatform := range wantedPlatforms {
		for _, d := range list.Manifests {
			imagePlatform := ociPlatformFromSchema2PlatformSpec(d.Platform)
			if platform.MatchesPlatform(imagePlatform, wantedPlatform) {
				return d.Digest, nil
			}
		}
	}
	return "", fmt.Errorf("no image found in manifest list for architecture %q, variant %q, OS %q", wantedPlatforms[0].Architecture, wantedPlatforms[0].Variant, wantedPlatforms[0].OS)
}

// Serialize returns the list in a blob format.
// NOTE: Serialize() does not in general reproduce the original blob if this object was loaded from one, even if no modifications were made!
func (list *Schema2ListPublic) Serialize() ([]byte, error) {
	buf, err := json.Marshal(list)
	if err != nil {
		return nil, fmt.Errorf("marshaling Schema2List %#v: %w", list, err)
	}
	return buf, nil
}

// Schema2ListPublicFromComponents creates a Schema2 manifest list instance from the
// supplied data.
// This is publicly visible as c/image/manifest.Schema2ListFromComponents.
func Schema2ListPublicFromComponents(components []Schema2ManifestDescriptor) *Schema2ListPublic {
	list := Schema2ListPublic{
		SchemaVersion: 2,
		MediaType:     DockerV2ListMediaType,
		Manifests:     make([]Schema2ManifestDescriptor, len(components)),
	}
	for i, component := range components {
		m := Schema2ManifestDescriptor{
			Schema2Descriptor{
				MediaType: component.MediaType,
				Size:      component.Size,
				Digest:    component.Digest,
				URLs:      slices.Clone(component.URLs),
			},
			Schema2PlatformSpec{
				Architecture: component.Platform.Architecture,
				OS:           component.Platform.OS,
				OSVersion:    component.Platform.OSVersion,
				OSFeatures:   slices.Clone(component.Platform.OSFeatures),
				Variant:      component.Platform.Variant,
				Features:     slices.Clone(component.Platform.Features),
			},
		}
		list.Manifests[i] = m
	}
	return &list
}

// Schema2ListPublicClone creates a deep copy of the passed-in list.
// This is publicly visible as c/image/manifest.Schema2ListClone.
func Schema2ListPublicClone(list *Schema2ListPublic) *Schema2ListPublic {
	return Schema2ListPublicFromComponents(list.Manifests)
}

// ToOCI1Index returns the list encoded as an OCI1 index.
func (list *Schema2ListPublic) ToOCI1Index() (*OCI1IndexPublic, error) {
	components := make([]imgspecv1.Descriptor, 0, len(list.Manifests))
	for _, manifest := range list.Manifests {
		platform := ociPlatformFromSchema2PlatformSpec(manifest.Platform)
		components = append(components, imgspecv1.Descriptor{
			MediaType: manifest.MediaType,
			Size:      manifest.Size,
			Digest:    manifest.Digest,
			URLs:      slices.Clone(manifest.URLs),
			Platform:  &platform,
		})
	}
	oci := OCI1IndexPublicFromComponents(components, nil)
	return oci, nil
}

// ToSchema2List returns the list encoded as a Schema2 list.
func (list *Schema2ListPublic) ToSchema2List() (*Schema2ListPublic, error) {
	return Schema2ListPublicClone(list), nil
}

// Schema2ListPublicFromManifest creates a Schema2 manifest list instance from marshalled
// JSON, presumably generated by encoding a Schema2 manifest list.
// This is publicly visible as c/image/manifest.Schema2ListFromManifest.
func Schema2ListPublicFromManifest(manifest []byte) (*Schema2ListPublic, error) {
	list := Schema2ListPublic{
		Manifests: []Schema2ManifestDescriptor{},
	}
	if err := json.Unmarshal(manifest, &list); err != nil {
		return nil, fmt.Errorf("unmarshaling Schema2List %q: %w", string(manifest), err)
	}
	if err := ValidateUnambiguousManifestFormat(manifest, DockerV2ListMediaType,
		AllowedFieldManifests); err != nil {
		return nil, err
	}
	return &list, nil
}

// Clone returns a deep copy of this list and its contents.
func (list *Schema2ListPublic) Clone() ListPublic {
	return Schema2ListPublicClone(list)
}

// ConvertToMIMEType converts the passed-in manifest list to a manifest
// list of the specified type.
func (list *Schema2ListPublic) ConvertToMIMEType(manifestMIMEType string) (ListPublic, error) {
	switch normalized := NormalizedMIMEType(manifestMIMEType); normalized {
	case DockerV2ListMediaType:
		return list.Clone(), nil
	case imgspecv1.MediaTypeImageIndex:
		return list.ToOCI1Index()
	case DockerV2Schema1MediaType, DockerV2Schema1SignedMediaType, imgspecv1.MediaTypeImageManifest, DockerV2Schema2MediaType:
		return nil, fmt.Errorf("Can not convert manifest list to MIME type %q, which is not a list type", manifestMIMEType)
	default:
		// Note that this may not be reachable, NormalizedMIMEType has a default for unknown values.
		return nil, fmt.Errorf("Unimplemented manifest list MIME type %s", manifestMIMEType)
	}
}

// Schema2List is a list of platform-specific manifests.
type Schema2List struct {
	Schema2ListPublic
}

func schema2ListFromPublic(public *Schema2ListPublic) *Schema2List {
	return &Schema2List{*public}
}

func (list *Schema2List) CloneInternal() List {
	return schema2ListFromPublic(Schema2ListPublicClone(&list.Schema2ListPublic))
}

func (list *Schema2List) Clone() ListPublic {
	return list.CloneInternal()
}

// Schema2ListFromManifest creates a Schema2 manifest list instance from marshalled
// JSON, presumably generated by encoding a Schema2 manifest list.
func Schema2ListFromManifest(manifest []byte) (*Schema2List, error) {
	public, err := Schema2ListPublicFromManifest(manifest)
	if err != nil {
		return nil, err
	}
	return schema2ListFromPublic(public), nil
}

// ociPlatformFromSchema2PlatformSpec converts a schema2 platform p to the OCI struccture.
func ociPlatformFromSchema2PlatformSpec(p Schema2PlatformSpec) imgspecv1.Platform {
	return imgspecv1.Platform{
		Architecture: p.Architecture,
		OS:           p.OS,
		OSVersion:    p.OSVersion,
		OSFeatures:   slices.Clone(p.OSFeatures),
		Variant:      p.Variant,
		// Features is not supported in OCI, and discarded.
	}
}
