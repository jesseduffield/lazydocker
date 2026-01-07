package image

import (
	"context"
	"fmt"

	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/types"
)

type manifestSchema1 struct {
	m *manifest.Schema1
}

func manifestSchema1FromManifest(manifestBlob []byte) (genericManifest, error) {
	m, err := manifest.Schema1FromManifest(manifestBlob)
	if err != nil {
		return nil, err
	}
	return &manifestSchema1{m: m}, nil
}

// manifestSchema1FromComponents builds a new manifestSchema1 from the supplied data.
func manifestSchema1FromComponents(ref reference.Named, fsLayers []manifest.Schema1FSLayers, history []manifest.Schema1History, architecture string) (genericManifest, error) {
	m, err := manifest.Schema1FromComponents(ref, fsLayers, history, architecture)
	if err != nil {
		return nil, err
	}
	return &manifestSchema1{m: m}, nil
}

func (m *manifestSchema1) serialize() ([]byte, error) {
	return m.m.Serialize()
}

func (m *manifestSchema1) manifestMIMEType() string {
	return manifest.DockerV2Schema1SignedMediaType
}

// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
// Note that the config object may not exist in the underlying storage in the return value of UpdatedImage! Use ConfigBlob() below.
func (m *manifestSchema1) ConfigInfo() types.BlobInfo {
	return m.m.ConfigInfo()
}

// ConfigBlob returns the blob described by ConfigInfo, iff ConfigInfo().Digest != ""; nil otherwise.
// The result is cached; it is OK to call this however often you need.
func (m *manifestSchema1) ConfigBlob(context.Context) ([]byte, error) {
	return nil, nil
}

// OCIConfig returns the image configuration as per OCI v1 image-spec. Information about
// layers in the resulting configuration isn't guaranteed to be returned to due how
// old image manifests work (docker v2s1 especially).
func (m *manifestSchema1) OCIConfig(ctx context.Context) (*imgspecv1.Image, error) {
	v2s2, err := m.convertToManifestSchema2(ctx, &types.ManifestUpdateOptions{})
	if err != nil {
		return nil, err
	}
	return v2s2.OCIConfig(ctx)
}

// LayerInfos returns a list of BlobInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (m *manifestSchema1) LayerInfos() []types.BlobInfo {
	return manifestLayerInfosToBlobInfos(m.m.LayerInfos())
}

// EmbeddedDockerReferenceConflicts whether a Docker reference embedded in the manifest, if any, conflicts with destination ref.
// It returns false if the manifest does not embed a Docker reference.
// (This embedding unfortunately happens for Docker schema1, please do not add support for this in any new formats.)
func (m *manifestSchema1) EmbeddedDockerReferenceConflicts(ref reference.Named) bool {
	// This is a bit convoluted: We can’t just have a "get embedded docker reference" method
	// and have the “does it conflict” logic in the generic copy code, because the manifest does not actually
	// embed a full docker/distribution reference, but only the repo name and tag (without the host name).
	// So we would have to provide a “return repo without host name, and tag” getter for the generic code,
	// which would be very awkward.  Instead, we do the matching here in schema1-specific code, and all the
	// generic copy code needs to know about is reference.Named and that a manifest may need updating
	// for some destinations.
	name := reference.Path(ref)
	var tag string
	if tagged, isTagged := ref.(reference.NamedTagged); isTagged {
		tag = tagged.Tag()
	} else {
		tag = ""
	}
	return m.m.Name != name || m.m.Tag != tag
}

// Inspect returns various information for (skopeo inspect) parsed from the manifest and configuration.
func (m *manifestSchema1) Inspect(context.Context) (*types.ImageInspectInfo, error) {
	return m.m.Inspect(nil)
}

// UpdatedImageNeedsLayerDiffIDs returns true iff UpdatedImage(options) needs InformationOnly.LayerDiffIDs.
// This is a horribly specific interface, but computing InformationOnly.LayerDiffIDs can be very expensive to compute
// (most importantly it forces us to download the full layers even if they are already present at the destination).
func (m *manifestSchema1) UpdatedImageNeedsLayerDiffIDs(options types.ManifestUpdateOptions) bool {
	return (options.ManifestMIMEType == manifest.DockerV2Schema2MediaType || options.ManifestMIMEType == imgspecv1.MediaTypeImageManifest)
}

// UpdatedImage returns a types.Image modified according to options.
// This does not change the state of the original Image object.
func (m *manifestSchema1) UpdatedImage(ctx context.Context, options types.ManifestUpdateOptions) (types.Image, error) {
	copy := manifestSchema1{m: manifest.Schema1Clone(m.m)}

	// We have 2 MIME types for schema 1, which are basically equivalent (even the un-"Signed" MIME type will be rejected if there isn’t a signature; so,
	// handle conversions between them by doing nothing.
	if options.ManifestMIMEType != manifest.DockerV2Schema1MediaType && options.ManifestMIMEType != manifest.DockerV2Schema1SignedMediaType {
		converted, err := convertManifestIfRequiredWithUpdate(ctx, options, map[string]manifestConvertFn{
			imgspecv1.MediaTypeImageManifest:  copy.convertToManifestOCI1,
			manifest.DockerV2Schema2MediaType: copy.convertToManifestSchema2Generic,
		})
		if err != nil {
			return nil, err
		}

		if converted != nil {
			return converted, nil
		}
	}

	// No conversion required, update manifest
	if options.LayerInfos != nil {
		if err := copy.m.UpdateLayerInfos(options.LayerInfos); err != nil {
			return nil, err
		}
	}
	if options.EmbeddedDockerReference != nil {
		copy.m.Name = reference.Path(options.EmbeddedDockerReference)
		if tagged, isTagged := options.EmbeddedDockerReference.(reference.NamedTagged); isTagged {
			copy.m.Tag = tagged.Tag()
		} else {
			copy.m.Tag = ""
		}
	}

	return memoryImageFromManifest(&copy), nil
}

// convertToManifestSchema2Generic returns a genericManifest implementation converted to manifest.DockerV2Schema2MediaType.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestSchema1 object.
//
// We need this function just because a function returning an implementation of the genericManifest
// interface is not automatically assignable to a function type returning the genericManifest interface
func (m *manifestSchema1) convertToManifestSchema2Generic(ctx context.Context, options *types.ManifestUpdateOptions) (genericManifest, error) {
	return m.convertToManifestSchema2(ctx, options)
}

// convertToManifestSchema2 returns a genericManifest implementation converted to manifest.DockerV2Schema2MediaType.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestSchema1 object.
//
// Based on github.com/docker/docker/distribution/pull_v2.go
func (m *manifestSchema1) convertToManifestSchema2(_ context.Context, options *types.ManifestUpdateOptions) (*manifestSchema2, error) {
	uploadedLayerInfos := options.InformationOnly.LayerInfos
	layerDiffIDs := options.InformationOnly.LayerDiffIDs

	if len(m.m.ExtractedV1Compatibility) == 0 {
		// What would this even mean?! Anyhow, the rest of the code depends on FSLayers[0] and ExtractedV1Compatibility[0] existing.
		return nil, fmt.Errorf("Cannot convert an image with 0 history entries to %s", manifest.DockerV2Schema2MediaType)
	}
	if len(m.m.ExtractedV1Compatibility) != len(m.m.FSLayers) {
		return nil, fmt.Errorf("Inconsistent schema 1 manifest: %d history entries, %d fsLayers entries", len(m.m.ExtractedV1Compatibility), len(m.m.FSLayers))
	}
	if uploadedLayerInfos != nil && len(uploadedLayerInfos) != len(m.m.FSLayers) {
		return nil, fmt.Errorf("Internal error: uploaded %d blobs, but schema1 manifest has %d fsLayers", len(uploadedLayerInfos), len(m.m.FSLayers))
	}
	if layerDiffIDs != nil && len(layerDiffIDs) != len(m.m.FSLayers) {
		return nil, fmt.Errorf("Internal error: collected %d DiffID values, but schema1 manifest has %d fsLayers", len(layerDiffIDs), len(m.m.FSLayers))
	}

	var convertedLayerUpdates []types.BlobInfo // Only used if options.LayerInfos != nil
	if options.LayerInfos != nil {
		if len(options.LayerInfos) != len(m.m.FSLayers) {
			return nil, fmt.Errorf("Error converting image: layer edits for %d layers vs %d existing layers",
				len(options.LayerInfos), len(m.m.FSLayers))
		}
		convertedLayerUpdates = []types.BlobInfo{}
	}

	// Build a list of the diffIDs for the non-empty layers.
	diffIDs := []digest.Digest{}
	var layers []manifest.Schema2Descriptor
	for v1Index := len(m.m.ExtractedV1Compatibility) - 1; v1Index >= 0; v1Index-- {
		v2Index := (len(m.m.ExtractedV1Compatibility) - 1) - v1Index

		if !m.m.ExtractedV1Compatibility[v1Index].ThrowAway {
			var size int64
			if uploadedLayerInfos != nil {
				size = uploadedLayerInfos[v2Index].Size
			}
			var d digest.Digest
			if layerDiffIDs != nil {
				d = layerDiffIDs[v2Index]
			}
			layers = append(layers, manifest.Schema2Descriptor{
				MediaType: manifest.DockerV2Schema2LayerMediaType,
				Size:      size,
				Digest:    m.m.FSLayers[v1Index].BlobSum,
			})
			if options.LayerInfos != nil {
				convertedLayerUpdates = append(convertedLayerUpdates, options.LayerInfos[v2Index])
			}
			diffIDs = append(diffIDs, d)
		}
	}
	configJSON, err := m.m.ToSchema2Config(diffIDs)
	if err != nil {
		return nil, err
	}
	configDescriptor := manifest.Schema2Descriptor{
		MediaType: manifest.DockerV2Schema2ConfigMediaType,
		Size:      int64(len(configJSON)),
		Digest:    digest.FromBytes(configJSON),
	}

	if options.LayerInfos != nil {
		options.LayerInfos = convertedLayerUpdates
	}
	return manifestSchema2FromComponents(configDescriptor, nil, configJSON, layers), nil
}

// convertToManifestOCI1 returns a genericManifest implementation converted to imgspecv1.MediaTypeImageManifest.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestSchema1 object.
func (m *manifestSchema1) convertToManifestOCI1(ctx context.Context, options *types.ManifestUpdateOptions) (genericManifest, error) {
	// We can't directly convert to OCI, but we can transitively convert via a Docker V2.2 Distribution manifest
	m2, err := m.convertToManifestSchema2(ctx, options)
	if err != nil {
		return nil, err
	}

	return m2.convertToManifestOCI1(ctx, options)
}

// SupportsEncryption returns if encryption is supported for the manifest type
func (m *manifestSchema1) SupportsEncryption(context.Context) bool {
	return false
}

// CanChangeLayerCompression returns true if we can compress/decompress layers with mimeType in the current image
// (and the code can handle that).
// NOTE: Even if this returns true, the relevant format might not accept all compression algorithms; the set of accepted
// algorithms depends not on the current format, but possibly on the target of a conversion (if UpdatedImage converts
// to a different manifest format).
func (m *manifestSchema1) CanChangeLayerCompression(mimeType string) bool {
	return true // There are no MIME types in the manifest, so we must assume a valid image.
}
