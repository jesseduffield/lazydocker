package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"runtime"
	"slices"

	"github.com/opencontainers/go-digest"
	imgspec "github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	platform "go.podman.io/image/v5/internal/pkg/platform"
	compression "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

const (
	// OCI1InstanceAnnotationCompressionZSTD is an annotation name that can be placed on a manifest descriptor in an OCI index.
	// The value of the annotation must be the string "true".
	// If this annotation is present on a manifest, consuming that image instance requires support for Zstd compression.
	// That also suggests that this instance benefits from
	// Zstd compression, so it can be preferred by compatible consumers over instances that
	// use gzip, depending on their local policy.
	OCI1InstanceAnnotationCompressionZSTD      = "io.github.containers.compression.zstd"
	OCI1InstanceAnnotationCompressionZSTDValue = "true"
)

// OCI1IndexPublic is just an alias for the OCI index type, but one which we can
// provide methods for.
// This is publicly visible as c/image/manifest.OCI1Index
// Internal users should usually use OCI1Index instead.
type OCI1IndexPublic struct {
	imgspecv1.Index
}

// MIMEType returns the MIME type of this particular manifest index.
func (index *OCI1IndexPublic) MIMEType() string {
	return imgspecv1.MediaTypeImageIndex
}

// Instances returns a slice of digests of the manifests that this index knows of.
func (index *OCI1IndexPublic) Instances() []digest.Digest {
	results := make([]digest.Digest, len(index.Manifests))
	for i, m := range index.Manifests {
		results[i] = m.Digest
	}
	return results
}

// Instance returns the ListUpdate of a particular instance in the index.
func (index *OCI1IndexPublic) Instance(instanceDigest digest.Digest) (ListUpdate, error) {
	for _, manifest := range index.Manifests {
		if manifest.Digest == instanceDigest {
			ret := ListUpdate{
				Digest:    manifest.Digest,
				Size:      manifest.Size,
				MediaType: manifest.MediaType,
			}
			ret.ReadOnly.Platform = manifest.Platform
			ret.ReadOnly.Annotations = manifest.Annotations
			ret.ReadOnly.CompressionAlgorithmNames = annotationsToCompressionAlgorithmNames(manifest.Annotations)
			ret.ReadOnly.ArtifactType = manifest.ArtifactType
			return ret, nil
		}
	}
	return ListUpdate{}, fmt.Errorf("unable to find instance %s in OCI1Index", instanceDigest)
}

// UpdateInstances updates the sizes, digests, and media types of the manifests
// which the list catalogs.
func (index *OCI1IndexPublic) UpdateInstances(updates []ListUpdate) error {
	editInstances := []ListEdit{}
	for i, instance := range updates {
		editInstances = append(editInstances, ListEdit{
			UpdateOldDigest: index.Manifests[i].Digest,
			UpdateDigest:    instance.Digest,
			UpdateSize:      instance.Size,
			UpdateMediaType: instance.MediaType,
			ListOperation:   ListOpUpdate})
	}
	return index.editInstances(editInstances)
}

func annotationsToCompressionAlgorithmNames(annotations map[string]string) []string {
	result := make([]string, 0, 1)
	if annotations[OCI1InstanceAnnotationCompressionZSTD] == OCI1InstanceAnnotationCompressionZSTDValue {
		result = append(result, compression.ZstdAlgorithmName)
	}
	// No compression was detected, hence assume instance has default compression `Gzip`
	if len(result) == 0 {
		result = append(result, compression.GzipAlgorithmName)
	}
	return result
}

func addCompressionAnnotations(compressionAlgorithms []compression.Algorithm, annotationsMap *map[string]string) {
	// TODO: This should also delete the algorithm if map already contains an algorithm and compressionAlgorithm
	// list has a different algorithm. To do that, we would need to modify the callers to always provide a reliable
	// and full compressionAlghorithms list.
	if *annotationsMap == nil && len(compressionAlgorithms) > 0 {
		*annotationsMap = map[string]string{}
	}
	for _, algo := range compressionAlgorithms {
		switch algo.BaseVariantName() {
		case compression.ZstdAlgorithmName:
			(*annotationsMap)[OCI1InstanceAnnotationCompressionZSTD] = OCI1InstanceAnnotationCompressionZSTDValue
		default:
			continue
		}
	}
}

func (index *OCI1IndexPublic) editInstances(editInstances []ListEdit) error {
	addedEntries := []imgspecv1.Descriptor{}
	updatedAnnotations := false
	for i, editInstance := range editInstances {
		switch editInstance.ListOperation {
		case ListOpUpdate:
			if err := editInstance.UpdateOldDigest.Validate(); err != nil {
				return fmt.Errorf("OCI1Index.EditInstances: Attempting to update %s which is an invalid digest: %w", editInstance.UpdateOldDigest, err)
			}
			if err := editInstance.UpdateDigest.Validate(); err != nil {
				return fmt.Errorf("OCI1Index.EditInstances: Modified digest %s is an invalid digest: %w", editInstance.UpdateDigest, err)
			}
			targetIndex := slices.IndexFunc(index.Manifests, func(m imgspecv1.Descriptor) bool {
				return m.Digest == editInstance.UpdateOldDigest
			})
			if targetIndex == -1 {
				return fmt.Errorf("OCI1Index.EditInstances: digest %s not found", editInstance.UpdateOldDigest)
			}
			index.Manifests[targetIndex].Digest = editInstance.UpdateDigest
			if editInstance.UpdateSize < 0 {
				return fmt.Errorf("update %d of %d passed to OCI1Index.UpdateInstances had an invalid size (%d)", i+1, len(editInstances), editInstance.UpdateSize)
			}
			index.Manifests[targetIndex].Size = editInstance.UpdateSize
			if editInstance.UpdateMediaType == "" {
				return fmt.Errorf("update %d of %d passed to OCI1Index.UpdateInstances had no media type (was %q)", i+1, len(editInstances), index.Manifests[i].MediaType)
			}
			index.Manifests[targetIndex].MediaType = editInstance.UpdateMediaType
			if editInstance.UpdateAnnotations != nil {
				updatedAnnotations = true
				if editInstance.UpdateAffectAnnotations {
					index.Manifests[targetIndex].Annotations = maps.Clone(editInstance.UpdateAnnotations)
				} else {
					if index.Manifests[targetIndex].Annotations == nil {
						index.Manifests[targetIndex].Annotations = map[string]string{}
					}
					maps.Copy(index.Manifests[targetIndex].Annotations, editInstance.UpdateAnnotations)
				}
			}
			addCompressionAnnotations(editInstance.UpdateCompressionAlgorithms, &index.Manifests[targetIndex].Annotations)
		case ListOpAdd:
			annotations := map[string]string{}
			if editInstance.AddAnnotations != nil {
				annotations = maps.Clone(editInstance.AddAnnotations)
			}
			addCompressionAnnotations(editInstance.AddCompressionAlgorithms, &annotations)
			addedEntries = append(addedEntries, imgspecv1.Descriptor{
				MediaType:    editInstance.AddMediaType,
				ArtifactType: editInstance.AddArtifactType,
				Size:         editInstance.AddSize,
				Digest:       editInstance.AddDigest,
				Platform:     editInstance.AddPlatform,
				Annotations:  annotations,
			})
		default:
			return fmt.Errorf("internal error: invalid operation: %d", editInstance.ListOperation)
		}
	}
	if len(addedEntries) != 0 {
		// slices.Clone() here to ensure the slice uses a private backing array;
		// an external caller could have manually created OCI1IndexPublic with a slice with extra capacity.
		index.Manifests = append(slices.Clone(index.Manifests), addedEntries...)
	}
	if len(addedEntries) != 0 || updatedAnnotations {
		slices.SortStableFunc(index.Manifests, func(a, b imgspecv1.Descriptor) int {
			// FIXME? With Go 1.21 and cmp.Compare available, turn instanceIsZstd into an integer score that can be compared, and generalizes
			// into more algorithms?
			aZstd := instanceIsZstd(a)
			bZstd := instanceIsZstd(b)
			switch {
			case aZstd == bZstd:
				return 0
			case !aZstd: // Implies bZstd
				return -1
			default: // aZstd && !bZstd
				return 1
			}
		})
	}
	return nil
}

func (index *OCI1Index) EditInstances(editInstances []ListEdit) error {
	return index.editInstances(editInstances)
}

// instanceIsZstd returns true if instance is a zstd instance otherwise false.
func instanceIsZstd(manifest imgspecv1.Descriptor) bool {
	if value, ok := manifest.Annotations[OCI1InstanceAnnotationCompressionZSTD]; ok && value == "true" {
		return true
	}
	return false
}

type instanceCandidate struct {
	platformIndex    int           // Index of the candidate in platform.WantedPlatforms: lower numbers are preferred; or math.maxInt if the candidate doesn’t have a platform
	isZstd           bool          // tells if particular instance if zstd instance
	manifestPosition int           // A zero-based index of the instance in the manifest list
	digest           digest.Digest // Instance digest
}

func (ic instanceCandidate) isPreferredOver(other *instanceCandidate, preferGzip types.OptionalBool) bool {
	switch {
	case ic.platformIndex != other.platformIndex:
		return ic.platformIndex < other.platformIndex
	case ic.isZstd != other.isZstd:
		if preferGzip != types.OptionalBoolTrue {
			return ic.isZstd
		} else {
			return !ic.isZstd
		}
	case ic.manifestPosition != other.manifestPosition:
		return ic.manifestPosition < other.manifestPosition
	}
	panic("internal error: invalid comparison between two candidates") // This should not be reachable because in all calls we make, the two candidates differ at least in manifestPosition.
}

// chooseInstance is a private equivalent to ChooseInstanceByCompression,
// shared by ChooseInstance and ChooseInstanceByCompression.
func (index *OCI1IndexPublic) chooseInstance(ctx *types.SystemContext, preferGzip types.OptionalBool) (digest.Digest, error) {
	wantedPlatforms := platform.WantedPlatforms(ctx)
	var bestMatch *instanceCandidate
	bestMatch = nil
	for manifestIndex, d := range index.Manifests {
		candidate := instanceCandidate{platformIndex: math.MaxInt, manifestPosition: manifestIndex, isZstd: instanceIsZstd(d), digest: d.Digest}
		if d.Platform != nil {
			imagePlatform := ociPlatformClone(*d.Platform)
			platformIndex := slices.IndexFunc(wantedPlatforms, func(wantedPlatform imgspecv1.Platform) bool {
				return platform.MatchesPlatform(imagePlatform, wantedPlatform)
			})
			if platformIndex == -1 {
				continue
			}
			candidate.platformIndex = platformIndex
		}
		if bestMatch == nil || candidate.isPreferredOver(bestMatch, preferGzip) {
			bestMatch = &candidate
		}
	}
	if bestMatch != nil {
		return bestMatch.digest, nil
	}
	return "", fmt.Errorf("no image found in image index for architecture %q, variant %q, OS %q", wantedPlatforms[0].Architecture, wantedPlatforms[0].Variant, wantedPlatforms[0].OS)
}

func (index *OCI1Index) ChooseInstanceByCompression(ctx *types.SystemContext, preferGzip types.OptionalBool) (digest.Digest, error) {
	return index.chooseInstance(ctx, preferGzip)
}

// ChooseInstance parses blob as an oci v1 manifest index, and returns the digest
// of the image which is appropriate for the current environment.
func (index *OCI1IndexPublic) ChooseInstance(ctx *types.SystemContext) (digest.Digest, error) {
	return index.chooseInstance(ctx, types.OptionalBoolFalse)
}

// Serialize returns the index in a blob format.
// NOTE: Serialize() does not in general reproduce the original blob if this object was loaded from one, even if no modifications were made!
func (index *OCI1IndexPublic) Serialize() ([]byte, error) {
	buf, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("marshaling OCI1Index %#v: %w", index, err)
	}
	return buf, nil
}

// OCI1IndexPublicFromComponents creates an OCI1 image index instance from the
// supplied data.
// This is publicly visible as c/image/manifest.OCI1IndexFromComponents.
func OCI1IndexPublicFromComponents(components []imgspecv1.Descriptor, annotations map[string]string) *OCI1IndexPublic {
	index := OCI1IndexPublic{
		imgspecv1.Index{
			Versioned:   imgspec.Versioned{SchemaVersion: 2},
			MediaType:   imgspecv1.MediaTypeImageIndex,
			Manifests:   make([]imgspecv1.Descriptor, len(components)),
			Annotations: maps.Clone(annotations),
		},
	}
	for i, component := range components {
		index.Manifests[i] = oci1DescriptorClone(component)
	}
	return &index
}

func oci1DescriptorClone(d imgspecv1.Descriptor) imgspecv1.Descriptor {
	var platform *imgspecv1.Platform
	if d.Platform != nil {
		platformCopy := ociPlatformClone(*d.Platform)
		platform = &platformCopy
	}
	return imgspecv1.Descriptor{
		MediaType:    d.MediaType,
		Digest:       d.Digest,
		Size:         d.Size,
		URLs:         slices.Clone(d.URLs),
		Annotations:  maps.Clone(d.Annotations),
		Data:         bytes.Clone(d.Data),
		Platform:     platform,
		ArtifactType: d.ArtifactType,
	}
}

// OCI1IndexPublicClone creates a deep copy of the passed-in index.
// This is publicly visible as c/image/manifest.OCI1IndexClone.
func OCI1IndexPublicClone(index *OCI1IndexPublic) *OCI1IndexPublic {
	var subject *imgspecv1.Descriptor
	if index.Subject != nil {
		s := oci1DescriptorClone(*index.Subject)
		subject = &s
	}
	manifests := make([]imgspecv1.Descriptor, len(index.Manifests))
	for i, m := range index.Manifests {
		manifests[i] = oci1DescriptorClone(m)
	}
	return &OCI1IndexPublic{
		Index: imgspecv1.Index{
			Versioned:    index.Versioned,
			MediaType:    index.MediaType,
			ArtifactType: index.ArtifactType,
			Manifests:    manifests,
			Subject:      subject,
			Annotations:  maps.Clone(index.Annotations),
		},
	}
}

// ToOCI1Index returns the index encoded as an OCI1 index.
func (index *OCI1IndexPublic) ToOCI1Index() (*OCI1IndexPublic, error) {
	return OCI1IndexPublicClone(index), nil
}

// ToSchema2List returns the index encoded as a Schema2 list.
func (index *OCI1IndexPublic) ToSchema2List() (*Schema2ListPublic, error) {
	components := make([]Schema2ManifestDescriptor, 0, len(index.Manifests))
	for _, manifest := range index.Manifests {
		platform := manifest.Platform
		if platform == nil {
			platform = &imgspecv1.Platform{
				OS:           runtime.GOOS,
				Architecture: runtime.GOARCH,
			}
		}
		components = append(components, Schema2ManifestDescriptor{
			Schema2Descriptor{
				MediaType: manifest.MediaType,
				Size:      manifest.Size,
				Digest:    manifest.Digest,
				URLs:      slices.Clone(manifest.URLs),
			},
			schema2PlatformSpecFromOCIPlatform(*platform),
		})
	}
	s2 := Schema2ListPublicFromComponents(components)
	return s2, nil
}

// OCI1IndexPublicFromManifest creates an OCI1 manifest index instance from marshalled
// JSON, presumably generated by encoding a OCI1 manifest index.
// This is publicly visible as c/image/manifest.OCI1IndexFromManifest.
func OCI1IndexPublicFromManifest(manifest []byte) (*OCI1IndexPublic, error) {
	index := OCI1IndexPublic{
		Index: imgspecv1.Index{
			Versioned:   imgspec.Versioned{SchemaVersion: 2},
			MediaType:   imgspecv1.MediaTypeImageIndex,
			Manifests:   []imgspecv1.Descriptor{},
			Annotations: make(map[string]string),
		},
	}
	if err := json.Unmarshal(manifest, &index); err != nil {
		return nil, fmt.Errorf("unmarshaling OCI1Index %q: %w", string(manifest), err)
	}
	if err := ValidateUnambiguousManifestFormat(manifest, imgspecv1.MediaTypeImageIndex,
		AllowedFieldManifests); err != nil {
		return nil, err
	}
	return &index, nil
}

// Clone returns a deep copy of this list and its contents.
func (index *OCI1IndexPublic) Clone() ListPublic {
	return OCI1IndexPublicClone(index)
}

// ConvertToMIMEType converts the passed-in image index to a manifest list of
// the specified type.
func (index *OCI1IndexPublic) ConvertToMIMEType(manifestMIMEType string) (ListPublic, error) {
	switch normalized := NormalizedMIMEType(manifestMIMEType); normalized {
	case DockerV2ListMediaType:
		return index.ToSchema2List()
	case imgspecv1.MediaTypeImageIndex:
		return index.Clone(), nil
	case DockerV2Schema1MediaType, DockerV2Schema1SignedMediaType, imgspecv1.MediaTypeImageManifest, DockerV2Schema2MediaType:
		return nil, fmt.Errorf("Can not convert image index to MIME type %q, which is not a list type", manifestMIMEType)
	default:
		// Note that this may not be reachable, NormalizedMIMEType has a default for unknown values.
		return nil, fmt.Errorf("Unimplemented manifest MIME type %s", manifestMIMEType)
	}
}

type OCI1Index struct {
	OCI1IndexPublic
}

func oci1IndexFromPublic(public *OCI1IndexPublic) *OCI1Index {
	return &OCI1Index{*public}
}

func (index *OCI1Index) CloneInternal() List {
	return oci1IndexFromPublic(OCI1IndexPublicClone(&index.OCI1IndexPublic))
}

func (index *OCI1Index) Clone() ListPublic {
	return index.CloneInternal()
}

// OCI1IndexFromManifest creates a OCI1 manifest list instance from marshalled
// JSON, presumably generated by encoding a OCI1 manifest list.
func OCI1IndexFromManifest(manifest []byte) (*OCI1Index, error) {
	public, err := OCI1IndexPublicFromManifest(manifest)
	if err != nil {
		return nil, err
	}
	return oci1IndexFromPublic(public), nil
}

// ociPlatformClone returns an independent copy of p.
func ociPlatformClone(p imgspecv1.Platform) imgspecv1.Platform {
	// The only practical way in Go to give read-only access to an array is to copy it.
	// The only practical way in Go to copy a deep structure is to either do it manually field by field,
	// or to use reflection (incl. a round-trip through JSON, which uses reflection).
	//
	// The combination of the two is just sad, and leads to code like this, which will
	// need to be updated with every new Platform field.
	return imgspecv1.Platform{
		Architecture: p.Architecture,
		OS:           p.OS,
		OSVersion:    p.OSVersion,
		OSFeatures:   slices.Clone(p.OSFeatures),
		Variant:      p.Variant,
	}
}

// schema2PlatformSpecFromOCIPlatform converts an OCI platform p to the schema2 structure.
func schema2PlatformSpecFromOCIPlatform(p imgspecv1.Platform) Schema2PlatformSpec {
	return Schema2PlatformSpec{
		Architecture: p.Architecture,
		OS:           p.OS,
		OSVersion:    p.OSVersion,
		OSFeatures:   slices.Clone(p.OSFeatures),
		Variant:      p.Variant,
		Features:     nil,
	}
}
