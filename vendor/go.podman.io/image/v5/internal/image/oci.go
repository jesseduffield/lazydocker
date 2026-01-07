package image

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	ociencspec "github.com/containers/ocicrypt/spec"
	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/iolimits"
	internalManifest "go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/blobinfocache/none"
	"go.podman.io/image/v5/types"
)

type manifestOCI1 struct {
	src        types.ImageSource // May be nil if configBlob is not nil
	configBlob []byte            // If set, corresponds to contents of m.Config.
	m          *manifest.OCI1
}

func manifestOCI1FromManifest(src types.ImageSource, manifestBlob []byte) (genericManifest, error) {
	m, err := manifest.OCI1FromManifest(manifestBlob)
	if err != nil {
		return nil, err
	}
	return &manifestOCI1{
		src: src,
		m:   m,
	}, nil
}

// manifestOCI1FromComponents builds a new manifestOCI1 from the supplied data:
func manifestOCI1FromComponents(config imgspecv1.Descriptor, src types.ImageSource, configBlob []byte, layers []imgspecv1.Descriptor) genericManifest {
	return &manifestOCI1{
		src:        src,
		configBlob: configBlob,
		m:          manifest.OCI1FromComponents(config, layers),
	}
}

func (m *manifestOCI1) serialize() ([]byte, error) {
	return m.m.Serialize()
}

func (m *manifestOCI1) manifestMIMEType() string {
	return imgspecv1.MediaTypeImageManifest
}

// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
// Note that the config object may not exist in the underlying storage in the return value of UpdatedImage! Use ConfigBlob() below.
func (m *manifestOCI1) ConfigInfo() types.BlobInfo {
	return m.m.ConfigInfo()
}

// ConfigBlob returns the blob described by ConfigInfo, iff ConfigInfo().Digest != ""; nil otherwise.
// The result is cached; it is OK to call this however often you need.
func (m *manifestOCI1) ConfigBlob(ctx context.Context) ([]byte, error) {
	if m.configBlob == nil {
		if m.src == nil {
			return nil, errors.New("Internal error: neither src nor configBlob set in manifestOCI1")
		}
		stream, _, err := m.src.GetBlob(ctx, manifest.BlobInfoFromOCI1Descriptor(m.m.Config), none.NoCache)
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		blob, err := iolimits.ReadAtMost(stream, iolimits.MaxConfigBodySize)
		if err != nil {
			return nil, err
		}
		computedDigest := digest.FromBytes(blob)
		if computedDigest != m.m.Config.Digest {
			return nil, fmt.Errorf("Download config.json digest %s does not match expected %s", computedDigest, m.m.Config.Digest)
		}
		m.configBlob = blob
	}
	return m.configBlob, nil
}

// OCIConfig returns the image configuration as per OCI v1 image-spec. Information about
// layers in the resulting configuration isn't guaranteed to be returned to due how
// old image manifests work (docker v2s1 especially).
func (m *manifestOCI1) OCIConfig(ctx context.Context) (*imgspecv1.Image, error) {
	if m.m.Config.MediaType != imgspecv1.MediaTypeImageConfig {
		return nil, internalManifest.NewNonImageArtifactError(&m.m.Manifest)
	}

	cb, err := m.ConfigBlob(ctx)
	if err != nil {
		return nil, err
	}
	configOCI := &imgspecv1.Image{}
	if err := json.Unmarshal(cb, configOCI); err != nil {
		return nil, err
	}
	return configOCI, nil
}

// LayerInfos returns a list of BlobInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (m *manifestOCI1) LayerInfos() []types.BlobInfo {
	return manifestLayerInfosToBlobInfos(m.m.LayerInfos())
}

// EmbeddedDockerReferenceConflicts whether a Docker reference embedded in the manifest, if any, conflicts with destination ref.
// It returns false if the manifest does not embed a Docker reference.
// (This embedding unfortunately happens for Docker schema1, please do not add support for this in any new formats.)
func (m *manifestOCI1) EmbeddedDockerReferenceConflicts(ref reference.Named) bool {
	return false
}

// Inspect returns various information for (skopeo inspect) parsed from the manifest and configuration.
func (m *manifestOCI1) Inspect(ctx context.Context) (*types.ImageInspectInfo, error) {
	getter := func(info types.BlobInfo) ([]byte, error) {
		if info.Digest != m.ConfigInfo().Digest {
			// Shouldn't ever happen
			return nil, errors.New("asked for a different config blob")
		}
		config, err := m.ConfigBlob(ctx)
		if err != nil {
			return nil, err
		}
		return config, nil
	}
	return m.m.Inspect(getter)
}

// UpdatedImageNeedsLayerDiffIDs returns true iff UpdatedImage(options) needs InformationOnly.LayerDiffIDs.
// This is a horribly specific interface, but computing InformationOnly.LayerDiffIDs can be very expensive to compute
// (most importantly it forces us to download the full layers even if they are already present at the destination).
func (m *manifestOCI1) UpdatedImageNeedsLayerDiffIDs(options types.ManifestUpdateOptions) bool {
	return false
}

// UpdatedImage returns a types.Image modified according to options.
// This does not change the state of the original Image object.
// The returned error will be a manifest.ManifestLayerCompressionIncompatibilityError
// if the combination of CompressionOperation and CompressionAlgorithm specified
// in one or more options.LayerInfos items indicates that a layer is compressed using
// an algorithm that is not allowed in OCI.
func (m *manifestOCI1) UpdatedImage(ctx context.Context, options types.ManifestUpdateOptions) (types.Image, error) {
	copy := manifestOCI1{ // NOTE: This is not a deep copy, it still shares slices etc.
		src:        m.src,
		configBlob: m.configBlob,
		m:          manifest.OCI1Clone(m.m),
	}

	converted, err := convertManifestIfRequiredWithUpdate(ctx, options, map[string]manifestConvertFn{
		manifest.DockerV2Schema2MediaType:       copy.convertToManifestSchema2Generic,
		manifest.DockerV2Schema1MediaType:       copy.convertToManifestSchema1,
		manifest.DockerV2Schema1SignedMediaType: copy.convertToManifestSchema1,
	})
	if err != nil {
		return nil, err
	}

	if converted != nil {
		return converted, nil
	}

	// No conversion required, update manifest
	if options.LayerInfos != nil {
		if err := copy.m.UpdateLayerInfos(options.LayerInfos); err != nil {
			return nil, err
		}
	}
	// Ignore options.EmbeddedDockerReference: it may be set when converting from schema1, but we really don't care.

	return memoryImageFromManifest(&copy), nil
}

func schema2DescriptorFromOCI1Descriptor(d imgspecv1.Descriptor) manifest.Schema2Descriptor {
	return manifest.Schema2Descriptor{
		MediaType: d.MediaType,
		Size:      d.Size,
		Digest:    d.Digest,
		URLs:      d.URLs,
	}
}

// convertToManifestSchema2Generic returns a genericManifest implementation converted to manifest.DockerV2Schema2MediaType.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestSchema1 object.
//
// We need this function just because a function returning an implementation of the genericManifest
// interface is not automatically assignable to a function type returning the genericManifest interface
func (m *manifestOCI1) convertToManifestSchema2Generic(ctx context.Context, options *types.ManifestUpdateOptions) (genericManifest, error) {
	return m.convertToManifestSchema2(ctx, options)
}

// layerEditsOfOCIOnlyFeatures checks if options requires some layer edits to be done before converting to a Docker format.
// If not, it returns (nil, nil).
// If decryption is required, it returns a set of edits to provide to OCI1.UpdateLayerInfos,
// and edits *options to not try decryption again.
func (m *manifestOCI1) layerEditsOfOCIOnlyFeatures(options *types.ManifestUpdateOptions) ([]types.BlobInfo, error) {
	if options == nil || options.LayerInfos == nil {
		return nil, nil
	}

	originalInfos := m.LayerInfos()
	if len(originalInfos) != len(options.LayerInfos) {
		return nil, fmt.Errorf("preparing to decrypt before conversion: %d layers vs. %d layer edits", len(originalInfos), len(options.LayerInfos))
	}

	ociOnlyEdits := slices.Clone(originalInfos) // Start with a full copy so that we don't forget to copy anything: use the current data in full unless we intentionally deviate.
	laterEdits := slices.Clone(options.LayerInfos)
	needsOCIOnlyEdits := false
	for i, edit := range options.LayerInfos {
		// Unless determined otherwise, don't do any compression-related MIME type conversions. m.LayerInfos() should not set these edit instructions, but be explicit.
		ociOnlyEdits[i].CompressionOperation = types.PreserveOriginal
		ociOnlyEdits[i].CompressionAlgorithm = nil

		if edit.CryptoOperation == types.Decrypt {
			needsOCIOnlyEdits = true // Encrypted types must be removed before conversion because they can’t be represented in Docker schemas
			ociOnlyEdits[i].CryptoOperation = types.Decrypt
			laterEdits[i].CryptoOperation = types.PreserveOriginalCrypto // Don't try to decrypt in a schema[12] manifest later, that would fail.
		}

		if originalInfos[i].MediaType == imgspecv1.MediaTypeImageLayerZstd ||
			originalInfos[i].MediaType == imgspecv1.MediaTypeImageLayerNonDistributableZstd { //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
			needsOCIOnlyEdits = true // Zstd MIME types must be removed before conversion because they can’t be represented in Docker schemas.
			ociOnlyEdits[i].CompressionOperation = edit.CompressionOperation
			ociOnlyEdits[i].CompressionAlgorithm = edit.CompressionAlgorithm
			laterEdits[i].CompressionOperation = types.PreserveOriginal
			laterEdits[i].CompressionAlgorithm = nil
		}
	}
	if !needsOCIOnlyEdits {
		return nil, nil
	}

	options.LayerInfos = laterEdits
	return ociOnlyEdits, nil
}

// convertToManifestSchema2 returns a genericManifest implementation converted to manifest.DockerV2Schema2MediaType.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestOCI1 object.
func (m *manifestOCI1) convertToManifestSchema2(_ context.Context, options *types.ManifestUpdateOptions) (*manifestSchema2, error) {
	if m.m.Config.MediaType != imgspecv1.MediaTypeImageConfig {
		return nil, internalManifest.NewNonImageArtifactError(&m.m.Manifest)
	}

	// Mostly we first make a format conversion, and _afterwards_ do layer edits. But first we need to do the layer edits
	// which remove OCI-specific features, because trying to convert those layers would fail.
	// So, do the layer updates for decryption, and for conversions from Zstd.
	ociManifest := m.m
	ociOnlyEdits, err := m.layerEditsOfOCIOnlyFeatures(options)
	if err != nil {
		return nil, err
	}
	if ociOnlyEdits != nil {
		ociManifest = manifest.OCI1Clone(ociManifest)
		if err := ociManifest.UpdateLayerInfos(ociOnlyEdits); err != nil {
			return nil, err
		}
	}

	// Create a copy of the descriptor.
	config := schema2DescriptorFromOCI1Descriptor(ociManifest.Config)

	// Above, we have already checked that this manifest refers to an image, not an OCI artifact,
	// so the only difference between OCI and DockerSchema2 is the mediatypes. The
	// media type of the manifest is handled by manifestSchema2FromComponents.
	config.MediaType = manifest.DockerV2Schema2ConfigMediaType

	layers := make([]manifest.Schema2Descriptor, len(ociManifest.Layers))
	for idx := range layers {
		layers[idx] = schema2DescriptorFromOCI1Descriptor(ociManifest.Layers[idx])
		switch layers[idx].MediaType {
		case imgspecv1.MediaTypeImageLayerNonDistributable: //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
			layers[idx].MediaType = manifest.DockerV2Schema2ForeignLayerMediaType
		case imgspecv1.MediaTypeImageLayerNonDistributableGzip: //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
			layers[idx].MediaType = manifest.DockerV2Schema2ForeignLayerMediaTypeGzip
		case imgspecv1.MediaTypeImageLayerNonDistributableZstd: //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
			return nil, fmt.Errorf("Error during manifest conversion: %q: zstd compression is not supported for docker images", layers[idx].MediaType)
		case imgspecv1.MediaTypeImageLayer:
			layers[idx].MediaType = manifest.DockerV2SchemaLayerMediaTypeUncompressed
		case imgspecv1.MediaTypeImageLayerGzip:
			layers[idx].MediaType = manifest.DockerV2Schema2LayerMediaType
		case imgspecv1.MediaTypeImageLayerZstd:
			return nil, fmt.Errorf("Error during manifest conversion: %q: zstd compression is not officially supported for docker images", layers[idx].MediaType)
		case ociencspec.MediaTypeLayerEnc, ociencspec.MediaTypeLayerGzipEnc, ociencspec.MediaTypeLayerZstdEnc,
			ociencspec.MediaTypeLayerNonDistributableEnc, ociencspec.MediaTypeLayerNonDistributableGzipEnc, ociencspec.MediaTypeLayerNonDistributableZstdEnc:
			return nil, fmt.Errorf("during manifest conversion: encrypted layers (%q) are not supported in docker images", layers[idx].MediaType)
		default:
			return nil, fmt.Errorf("Unknown media type during manifest conversion: %q", layers[idx].MediaType)
		}
	}

	// Rather than copying the ConfigBlob now, we just pass m.src to the
	// translated manifest, since the only difference is the mediatype of
	// descriptors there is no change to any blob stored in m.src.
	return manifestSchema2FromComponents(config, m.src, nil, layers), nil
}

// convertToManifestSchema1 returns a genericManifest implementation converted to manifest.DockerV2Schema1{Signed,}MediaType.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestOCI1 object.
func (m *manifestOCI1) convertToManifestSchema1(ctx context.Context, options *types.ManifestUpdateOptions) (genericManifest, error) {
	if m.m.Config.MediaType != imgspecv1.MediaTypeImageConfig {
		return nil, internalManifest.NewNonImageArtifactError(&m.m.Manifest)
	}

	// We can't directly convert images to V1, but we can transitively convert via a V2 image
	m2, err := m.convertToManifestSchema2(ctx, options)
	if err != nil {
		return nil, err
	}

	return m2.convertToManifestSchema1(ctx, options)
}

// SupportsEncryption returns if encryption is supported for the manifest type
func (m *manifestOCI1) SupportsEncryption(context.Context) bool {
	return true
}

// CanChangeLayerCompression returns true if we can compress/decompress layers with mimeType in the current image
// (and the code can handle that).
// NOTE: Even if this returns true, the relevant format might not accept all compression algorithms; the set of accepted
// algorithms depends not on the current format, but possibly on the target of a conversion (if UpdatedImage converts
// to a different manifest format).
func (m *manifestOCI1) CanChangeLayerCompression(mimeType string) bool {
	return m.m.CanChangeLayerCompression(mimeType)
}
