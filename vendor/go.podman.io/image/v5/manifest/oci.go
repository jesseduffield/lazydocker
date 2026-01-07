package manifest

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	ociencspec "github.com/containers/ocicrypt/spec"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/internal/manifest"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

// BlobInfoFromOCI1Descriptor returns a types.BlobInfo based on the input OCI1 descriptor.
func BlobInfoFromOCI1Descriptor(desc imgspecv1.Descriptor) types.BlobInfo {
	return types.BlobInfo{
		Digest:      desc.Digest,
		Size:        desc.Size,
		URLs:        desc.URLs,
		Annotations: desc.Annotations,
		MediaType:   desc.MediaType,
	}
}

// OCI1 is a manifest.Manifest implementation for OCI images.
// The underlying data from imgspecv1.Manifest is also available.
type OCI1 struct {
	imgspecv1.Manifest
}

// SupportedOCI1MediaType checks if the specified string is a supported OCI1
// media type.
//
// Deprecated: blindly rejecting unknown MIME types when the consumer does not
// need to process the input just reduces interoperability (and violates the
// standard) with no benefit, and that this function does not check that the
// media type is appropriate for any specific purpose, so it’s not all that
// useful for validation anyway.
func SupportedOCI1MediaType(m string) error {
	switch m {
	case imgspecv1.MediaTypeDescriptor, imgspecv1.MediaTypeImageConfig,
		imgspecv1.MediaTypeImageLayer, imgspecv1.MediaTypeImageLayerGzip, imgspecv1.MediaTypeImageLayerZstd,
		imgspecv1.MediaTypeImageLayerNonDistributable, imgspecv1.MediaTypeImageLayerNonDistributableGzip, imgspecv1.MediaTypeImageLayerNonDistributableZstd, //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
		imgspecv1.MediaTypeImageManifest,
		imgspecv1.MediaTypeLayoutHeader,
		ociencspec.MediaTypeLayerEnc, ociencspec.MediaTypeLayerGzipEnc:
		return nil
	default:
		return fmt.Errorf("unsupported OCIv1 media type: %q", m)
	}
}

// OCI1FromManifest creates an OCI1 manifest instance from a manifest blob.
func OCI1FromManifest(manifestBlob []byte) (*OCI1, error) {
	oci1 := OCI1{}
	if err := json.Unmarshal(manifestBlob, &oci1); err != nil {
		return nil, err
	}
	if err := manifest.ValidateUnambiguousManifestFormat(manifestBlob, imgspecv1.MediaTypeImageManifest,
		manifest.AllowedFieldConfig|manifest.AllowedFieldLayers); err != nil {
		return nil, err
	}
	return &oci1, nil
}

// OCI1FromComponents creates an OCI1 manifest instance from the supplied data.
func OCI1FromComponents(config imgspecv1.Descriptor, layers []imgspecv1.Descriptor) *OCI1 {
	return &OCI1{
		imgspecv1.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: imgspecv1.MediaTypeImageManifest,
			Config:    config,
			Layers:    layers,
		},
	}
}

// OCI1Clone creates a copy of the supplied OCI1 manifest.
func OCI1Clone(src *OCI1) *OCI1 {
	return &OCI1{
		Manifest: src.Manifest,
	}
}

// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
func (m *OCI1) ConfigInfo() types.BlobInfo {
	return BlobInfoFromOCI1Descriptor(m.Config)
}

// LayerInfos returns a list of LayerInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (m *OCI1) LayerInfos() []LayerInfo {
	blobs := make([]LayerInfo, 0, len(m.Layers))
	for _, layer := range m.Layers {
		blobs = append(blobs, LayerInfo{
			BlobInfo:   BlobInfoFromOCI1Descriptor(layer),
			EmptyLayer: false,
		})
	}
	return blobs
}

var oci1CompressionMIMETypeSets = []compressionMIMETypeSet{
	{
		mtsUncompressed:                    imgspecv1.MediaTypeImageLayerNonDistributable,     //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
		compressiontypes.GzipAlgorithmName: imgspecv1.MediaTypeImageLayerNonDistributableGzip, //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
		compressiontypes.ZstdAlgorithmName: imgspecv1.MediaTypeImageLayerNonDistributableZstd, //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
	},
	{
		mtsUncompressed:                    imgspecv1.MediaTypeImageLayer,
		compressiontypes.GzipAlgorithmName: imgspecv1.MediaTypeImageLayerGzip,
		compressiontypes.ZstdAlgorithmName: imgspecv1.MediaTypeImageLayerZstd,
	},
}

// UpdateLayerInfos replaces the original layers with the specified BlobInfos (size+digest+urls+mediatype), in order (the root layer first, and then successive layered layers)
// The returned error will be a manifest.ManifestLayerCompressionIncompatibilityError if any of the layerInfos includes a combination of CompressionOperation and
// CompressionAlgorithm that isn't supported by OCI.
//
// It’s generally the caller’s responsibility to determine whether a particular edit is acceptable, rather than relying on
// failures of this function, because the layer is typically created _before_ UpdateLayerInfos is called, because UpdateLayerInfos needs
// to know the final digest). See OCI1.CanChangeLayerCompression for some help in determining this; other aspects like compression
// algorithms that might not be supported by a format, or the limited set of MIME types accepted for encryption, are not currently
// handled — that logic should eventually also be provided as OCI1 methods, not hard-coded in callers.
func (m *OCI1) UpdateLayerInfos(layerInfos []types.BlobInfo) error {
	if len(m.Layers) != len(layerInfos) {
		return fmt.Errorf("Error preparing updated manifest: layer count changed from %d to %d", len(m.Layers), len(layerInfos))
	}
	original := m.Layers
	m.Layers = make([]imgspecv1.Descriptor, len(layerInfos))
	for i, info := range layerInfos {
		mimeType := original[i].MediaType
		if info.CryptoOperation == types.Decrypt {
			decMimeType, err := getDecryptedMediaType(mimeType)
			if err != nil {
				return fmt.Errorf("error preparing updated manifest: decryption specified but original mediatype is not encrypted: %q", mimeType)
			}
			mimeType = decMimeType
		}
		mimeType, err := updatedMIMEType(oci1CompressionMIMETypeSets, mimeType, info)
		if err != nil {
			return fmt.Errorf("preparing updated manifest, layer %q: %w", info.Digest, err)
		}
		if info.CryptoOperation == types.Encrypt {
			encMediaType, err := getEncryptedMediaType(mimeType)
			if err != nil {
				return fmt.Errorf("error preparing updated manifest: encryption specified but no counterpart for mediatype: %q", mimeType)
			}
			mimeType = encMediaType
		}

		m.Layers[i].MediaType = mimeType
		m.Layers[i].Digest = info.Digest
		m.Layers[i].Size = info.Size
		m.Layers[i].Annotations = info.Annotations
		m.Layers[i].URLs = info.URLs
	}
	return nil
}

// getEncryptedMediaType will return the mediatype to its encrypted counterpart and return
// an error if the mediatype does not support encryption
func getEncryptedMediaType(mediatype string) (string, error) {
	parts := strings.Split(mediatype, "+")
	if slices.Contains(parts[1:], "encrypted") {
		return "", fmt.Errorf("unsupported mediaType: %q already encrypted", mediatype)
	}
	unsuffixedMediatype := parts[0]
	switch unsuffixedMediatype {
	case DockerV2Schema2LayerMediaType, imgspecv1.MediaTypeImageLayer,
		imgspecv1.MediaTypeImageLayerNonDistributable: //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
		return mediatype + "+encrypted", nil
	}

	return "", fmt.Errorf("unsupported mediaType to encrypt: %q", mediatype)
}

// getDecryptedMediaType will return the mediatype to its encrypted counterpart and return
// an error if the mediatype does not support decryption
func getDecryptedMediaType(mediatype string) (string, error) {
	res, ok := strings.CutSuffix(mediatype, "+encrypted")
	if !ok {
		return "", fmt.Errorf("unsupported mediaType to decrypt: %q", mediatype)
	}

	return res, nil
}

// Serialize returns the manifest in a blob format.
// NOTE: Serialize() does not in general reproduce the original blob if this object was loaded from one, even if no modifications were made!
func (m *OCI1) Serialize() ([]byte, error) {
	return json.Marshal(*m)
}

// Inspect returns various information for (skopeo inspect) parsed from the manifest and configuration.
func (m *OCI1) Inspect(configGetter func(types.BlobInfo) ([]byte, error)) (*types.ImageInspectInfo, error) {
	if m.Config.MediaType != imgspecv1.MediaTypeImageConfig {
		// We could return at least the layers, but that’s already available in a better format via types.Image.LayerInfos.
		// Most software calling this without human intervention is going to expect the values to be realistic and relevant,
		// and is probably better served by failing; we can always re-visit that later if we fail now, but
		// if we started returning some data for OCI artifacts now, we couldn’t start failing in this function later.
		return nil, manifest.NewNonImageArtifactError(&m.Manifest)
	}

	config, err := configGetter(m.ConfigInfo())
	if err != nil {
		return nil, err
	}
	v1 := &imgspecv1.Image{}
	if err := json.Unmarshal(config, v1); err != nil {
		return nil, err
	}
	d1 := &Schema2V1Image{}
	if err := json.Unmarshal(config, d1); err != nil {
		return nil, err
	}
	layerInfos := m.LayerInfos()
	i := &types.ImageInspectInfo{
		Tag:           "",
		Created:       v1.Created,
		DockerVersion: d1.DockerVersion,
		Labels:        v1.Config.Labels,
		Architecture:  v1.Architecture,
		Variant:       v1.Variant,
		Os:            v1.OS,
		Layers:        layerInfosToStrings(layerInfos),
		LayersData:    imgInspectLayersFromLayerInfos(layerInfos),
		Env:           v1.Config.Env,
		Author:        v1.Author,
	}
	return i, nil
}

// ImageID computes an ID which can uniquely identify this image by its contents.
func (m *OCI1) ImageID(diffIDs []digest.Digest) (string, error) {
	// The way m.Config.Digest “uniquely identifies” an image is
	// by containing RootFS.DiffIDs, which identify the layers of the image.
	// For non-image artifacts, the we can’t expect the config to change
	// any time the other layers (semantically) change, so this approach of
	// distinguishing objects only by m.Config.Digest doesn’t work in general.
	//
	// Any caller of this method presumably wants to disambiguate the same
	// images with a different representation, but doesn’t want to disambiguate
	// representations (by using a manifest digest).  So, submitting a non-image
	// artifact to such a caller indicates an expectation mismatch.
	// So, we just fail here instead of inventing some other ID value (e.g.
	// by combining the config and blob layer digests).  That still
	// gives us the option to not fail, and return some value, in the future,
	// without committing to that approach now.
	// (The only known caller of ImageID is storage/storageImageDestination.computeID,
	// which can’t work with non-image artifacts.)
	if m.Config.MediaType != imgspecv1.MediaTypeImageConfig {
		return "", manifest.NewNonImageArtifactError(&m.Manifest)
	}

	if err := m.Config.Digest.Validate(); err != nil {
		return "", err
	}
	return m.Config.Digest.Encoded(), nil
}

// CanChangeLayerCompression returns true if we can compress/decompress layers with mimeType in the current image
// (and the code can handle that).
// NOTE: Even if this returns true, the relevant format might not accept all compression algorithms; the set of accepted
// algorithms depends not on the current format, but possibly on the target of a conversion.
func (m *OCI1) CanChangeLayerCompression(mimeType string) bool {
	if m.Config.MediaType != imgspecv1.MediaTypeImageConfig {
		return false
	}
	return compressionVariantsRecognizeMIMEType(oci1CompressionMIMETypeSets, mimeType)
}
