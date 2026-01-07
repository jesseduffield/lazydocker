package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/iolimits"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/blobinfocache/none"
	"go.podman.io/image/v5/types"
)

// GzippedEmptyLayer is a gzip-compressed version of an empty tar file (1024 NULL bytes)
// This comes from github.com/docker/distribution/manifest/schema1/config_builder.go; there is
// a non-zero embedded timestamp; we could zero that, but that would just waste storage space
// in registries, so let’s use the same values.
//
// This is publicly visible as c/image/image.GzippedEmptyLayer.
var GzippedEmptyLayer = []byte{
	31, 139, 8, 0, 0, 9, 110, 136, 0, 255, 98, 24, 5, 163, 96, 20, 140, 88,
	0, 8, 0, 0, 255, 255, 46, 175, 181, 239, 0, 4, 0, 0,
}

// GzippedEmptyLayerDigest is a digest of GzippedEmptyLayer
//
// This is publicly visible as c/image/image.GzippedEmptyLayerDigest.
const GzippedEmptyLayerDigest = digest.Digest("sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4")

type manifestSchema2 struct {
	src        types.ImageSource // May be nil if configBlob is not nil
	configBlob []byte            // If set, corresponds to contents of ConfigDescriptor.
	m          *manifest.Schema2
}

func manifestSchema2FromManifest(src types.ImageSource, manifestBlob []byte) (genericManifest, error) {
	m, err := manifest.Schema2FromManifest(manifestBlob)
	if err != nil {
		return nil, err
	}
	return &manifestSchema2{
		src: src,
		m:   m,
	}, nil
}

// manifestSchema2FromComponents builds a new manifestSchema2 from the supplied data:
func manifestSchema2FromComponents(config manifest.Schema2Descriptor, src types.ImageSource, configBlob []byte, layers []manifest.Schema2Descriptor) *manifestSchema2 {
	return &manifestSchema2{
		src:        src,
		configBlob: configBlob,
		m:          manifest.Schema2FromComponents(config, layers),
	}
}

func (m *manifestSchema2) serialize() ([]byte, error) {
	return m.m.Serialize()
}

func (m *manifestSchema2) manifestMIMEType() string {
	return m.m.MediaType
}

// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
// Note that the config object may not exist in the underlying storage in the return value of UpdatedImage! Use ConfigBlob() below.
func (m *manifestSchema2) ConfigInfo() types.BlobInfo {
	return m.m.ConfigInfo()
}

// OCIConfig returns the image configuration as per OCI v1 image-spec. Information about
// layers in the resulting configuration isn't guaranteed to be returned to due how
// old image manifests work (docker v2s1 especially).
func (m *manifestSchema2) OCIConfig(ctx context.Context) (*imgspecv1.Image, error) {
	configBlob, err := m.ConfigBlob(ctx)
	if err != nil {
		return nil, err
	}
	// docker v2s2 and OCI v1 are mostly compatible but v2s2 contains more fields
	// than OCI v1. This unmarshal makes sure we drop docker v2s2
	// fields that aren't needed in OCI v1.
	configOCI := &imgspecv1.Image{}
	if err := json.Unmarshal(configBlob, configOCI); err != nil {
		return nil, err
	}
	return configOCI, nil
}

// ConfigBlob returns the blob described by ConfigInfo, iff ConfigInfo().Digest != ""; nil otherwise.
// The result is cached; it is OK to call this however often you need.
func (m *manifestSchema2) ConfigBlob(ctx context.Context) ([]byte, error) {
	if m.configBlob == nil {
		if m.src == nil {
			return nil, fmt.Errorf("Internal error: neither src nor configBlob set in manifestSchema2")
		}
		stream, _, err := m.src.GetBlob(ctx, manifest.BlobInfoFromSchema2Descriptor(m.m.ConfigDescriptor), none.NoCache)
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		blob, err := iolimits.ReadAtMost(stream, iolimits.MaxConfigBodySize)
		if err != nil {
			return nil, err
		}
		computedDigest := digest.FromBytes(blob)
		if computedDigest != m.m.ConfigDescriptor.Digest {
			return nil, fmt.Errorf("Download config.json digest %s does not match expected %s", computedDigest, m.m.ConfigDescriptor.Digest)
		}
		m.configBlob = blob
	}
	return m.configBlob, nil
}

// LayerInfos returns a list of BlobInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (m *manifestSchema2) LayerInfos() []types.BlobInfo {
	return manifestLayerInfosToBlobInfos(m.m.LayerInfos())
}

// EmbeddedDockerReferenceConflicts whether a Docker reference embedded in the manifest, if any, conflicts with destination ref.
// It returns false if the manifest does not embed a Docker reference.
// (This embedding unfortunately happens for Docker schema1, please do not add support for this in any new formats.)
func (m *manifestSchema2) EmbeddedDockerReferenceConflicts(ref reference.Named) bool {
	return false
}

// Inspect returns various information for (skopeo inspect) parsed from the manifest and configuration.
func (m *manifestSchema2) Inspect(ctx context.Context) (*types.ImageInspectInfo, error) {
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
func (m *manifestSchema2) UpdatedImageNeedsLayerDiffIDs(options types.ManifestUpdateOptions) bool {
	return false
}

// UpdatedImage returns a types.Image modified according to options.
// This does not change the state of the original Image object.
// The returned error will be a manifest.ManifestLayerCompressionIncompatibilityError
// if the CompressionOperation and CompressionAlgorithm specified in one or more
// options.LayerInfos items is anything other than gzip.
func (m *manifestSchema2) UpdatedImage(ctx context.Context, options types.ManifestUpdateOptions) (types.Image, error) {
	copy := manifestSchema2{ // NOTE: This is not a deep copy, it still shares slices etc.
		src:        m.src,
		configBlob: m.configBlob,
		m:          manifest.Schema2Clone(m.m),
	}

	converted, err := convertManifestIfRequiredWithUpdate(ctx, options, map[string]manifestConvertFn{
		manifest.DockerV2Schema1MediaType:       copy.convertToManifestSchema1,
		manifest.DockerV2Schema1SignedMediaType: copy.convertToManifestSchema1,
		imgspecv1.MediaTypeImageManifest:        copy.convertToManifestOCI1,
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
	// Ignore options.EmbeddedDockerReference: it may be set when converting from schema1 to schema2, but we really don't care.

	return memoryImageFromManifest(&copy), nil
}

func oci1DescriptorFromSchema2Descriptor(d manifest.Schema2Descriptor) imgspecv1.Descriptor {
	return imgspecv1.Descriptor{
		MediaType: d.MediaType,
		Size:      d.Size,
		Digest:    d.Digest,
		URLs:      d.URLs,
	}
}

// convertToManifestOCI1 returns a genericManifest implementation converted to imgspecv1.MediaTypeImageManifest.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestSchema2 object.
func (m *manifestSchema2) convertToManifestOCI1(ctx context.Context, _ *types.ManifestUpdateOptions) (genericManifest, error) {
	configOCI, err := m.OCIConfig(ctx)
	if err != nil {
		return nil, err
	}
	configOCIBytes, err := json.Marshal(configOCI)
	if err != nil {
		return nil, err
	}

	config := imgspecv1.Descriptor{
		MediaType: imgspecv1.MediaTypeImageConfig,
		Size:      int64(len(configOCIBytes)),
		Digest:    digest.FromBytes(configOCIBytes),
	}

	layers := make([]imgspecv1.Descriptor, len(m.m.LayersDescriptors))
	for idx := range layers {
		layers[idx] = oci1DescriptorFromSchema2Descriptor(m.m.LayersDescriptors[idx])
		switch m.m.LayersDescriptors[idx].MediaType {
		case manifest.DockerV2Schema2ForeignLayerMediaType:
			layers[idx].MediaType = imgspecv1.MediaTypeImageLayerNonDistributable //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
		case manifest.DockerV2Schema2ForeignLayerMediaTypeGzip:
			layers[idx].MediaType = imgspecv1.MediaTypeImageLayerNonDistributableGzip //nolint:staticcheck // NonDistributable layers are deprecated, but we want to continue to support manipulating pre-existing images.
		case manifest.DockerV2SchemaLayerMediaTypeUncompressed:
			layers[idx].MediaType = imgspecv1.MediaTypeImageLayer
		case manifest.DockerV2Schema2LayerMediaType:
			layers[idx].MediaType = imgspecv1.MediaTypeImageLayerGzip
		case manifest.DockerV2SchemaLayerMediaTypeZstd:
			layers[idx].MediaType = imgspecv1.MediaTypeImageLayerZstd
		default:
			return nil, fmt.Errorf("Unknown media type during manifest conversion: %q", m.m.LayersDescriptors[idx].MediaType)
		}
	}

	return manifestOCI1FromComponents(config, m.src, configOCIBytes, layers), nil
}

// convertToManifestSchema1 returns a genericManifest implementation converted to manifest.DockerV2Schema1{Signed,}MediaType.
// It may use options.InformationOnly and also adjust *options to be appropriate for editing the returned
// value.
// This does not change the state of the original manifestSchema2 object.
//
// Based on docker/distribution/manifest/schema1/config_builder.go
func (m *manifestSchema2) convertToManifestSchema1(ctx context.Context, options *types.ManifestUpdateOptions) (genericManifest, error) {
	dest := options.InformationOnly.Destination

	var convertedLayerUpdates []types.BlobInfo // Only used if options.LayerInfos != nil
	if options.LayerInfos != nil {
		if len(options.LayerInfos) != len(m.m.LayersDescriptors) {
			return nil, fmt.Errorf("Error converting image: layer edits for %d layers vs %d existing layers",
				len(options.LayerInfos), len(m.m.LayersDescriptors))
		}
		convertedLayerUpdates = []types.BlobInfo{}
	}

	configBytes, err := m.ConfigBlob(ctx)
	if err != nil {
		return nil, err
	}
	imageConfig := &manifest.Schema2Image{}
	if err := json.Unmarshal(configBytes, imageConfig); err != nil {
		return nil, err
	}

	// Build fsLayers and History, discarding all configs. We will patch the top-level config in later.
	fsLayers := make([]manifest.Schema1FSLayers, len(imageConfig.History))
	history := make([]manifest.Schema1History, len(imageConfig.History))
	nonemptyLayerIndex := 0
	var parentV1ID string // Set in the loop
	v1ID := ""
	haveGzippedEmptyLayer := false
	if len(imageConfig.History) == 0 {
		// What would this even mean?! Anyhow, the rest of the code depends on fsLayers[0] and history[0] existing.
		return nil, fmt.Errorf("Cannot convert an image with 0 history entries to %s", manifest.DockerV2Schema1SignedMediaType)
	}
	for v2Index, historyEntry := range imageConfig.History {
		parentV1ID = v1ID
		v1Index := len(imageConfig.History) - 1 - v2Index

		var blobDigest digest.Digest
		if historyEntry.EmptyLayer {
			emptyLayerBlobInfo := types.BlobInfo{Digest: GzippedEmptyLayerDigest, Size: int64(len(GzippedEmptyLayer))}

			if !haveGzippedEmptyLayer {
				logrus.Debugf("Uploading empty layer during conversion to schema 1")
				// Ideally we should update the relevant BlobInfoCache about this layer, but that would require passing it down here,
				// and anyway this blob is so small that it’s easier to just copy it than to worry about figuring out another location where to get it.
				info, err := dest.PutBlob(ctx, bytes.NewReader(GzippedEmptyLayer), emptyLayerBlobInfo, none.NoCache, false)
				if err != nil {
					return nil, fmt.Errorf("uploading empty layer: %w", err)
				}
				if info.Digest != emptyLayerBlobInfo.Digest {
					return nil, fmt.Errorf("Internal error: Uploaded empty layer has digest %#v instead of %s", info.Digest, emptyLayerBlobInfo.Digest)
				}
				haveGzippedEmptyLayer = true
			}
			if options.LayerInfos != nil {
				convertedLayerUpdates = append(convertedLayerUpdates, emptyLayerBlobInfo)
			}
			blobDigest = emptyLayerBlobInfo.Digest
		} else {
			if nonemptyLayerIndex >= len(m.m.LayersDescriptors) {
				return nil, fmt.Errorf("Invalid image configuration, needs more than the %d distributed layers", len(m.m.LayersDescriptors))
			}
			if options.LayerInfos != nil {
				convertedLayerUpdates = append(convertedLayerUpdates, options.LayerInfos[nonemptyLayerIndex])
			}
			blobDigest = m.m.LayersDescriptors[nonemptyLayerIndex].Digest
			nonemptyLayerIndex++
		}

		// AFAICT pull ignores these ID values, at least nowadays, so we could use anything unique, including a simple counter. Use what Docker uses for cargo-cult consistency.
		v, err := v1IDFromBlobDigestAndComponents(blobDigest, parentV1ID)
		if err != nil {
			return nil, err
		}
		v1ID = v

		fakeImage := manifest.Schema1V1Compatibility{
			ID:        v1ID,
			Parent:    parentV1ID,
			Comment:   historyEntry.Comment,
			Created:   historyEntry.Created,
			Author:    historyEntry.Author,
			ThrowAway: historyEntry.EmptyLayer,
		}
		fakeImage.ContainerConfig.Cmd = []string{historyEntry.CreatedBy}
		v1CompatibilityBytes, err := json.Marshal(&fakeImage)
		if err != nil {
			return nil, fmt.Errorf("Internal error: Error creating v1compatibility for %#v", fakeImage)
		}

		fsLayers[v1Index] = manifest.Schema1FSLayers{BlobSum: blobDigest}
		history[v1Index] = manifest.Schema1History{V1Compatibility: string(v1CompatibilityBytes)}
		// Note that parentV1ID of the top layer is preserved when exiting this loop
	}

	// Now patch in real configuration for the top layer (v1Index == 0)
	v1ID, err = v1IDFromBlobDigestAndComponents(fsLayers[0].BlobSum, parentV1ID, string(configBytes)) // See above WRT v1ID value generation and cargo-cult consistency.
	if err != nil {
		return nil, err
	}
	v1Config, err := v1ConfigFromConfigJSON(configBytes, v1ID, parentV1ID, imageConfig.History[len(imageConfig.History)-1].EmptyLayer)
	if err != nil {
		return nil, err
	}
	history[0].V1Compatibility = string(v1Config)

	if options.LayerInfos != nil {
		options.LayerInfos = convertedLayerUpdates
	}
	m1, err := manifestSchema1FromComponents(dest.Reference().DockerReference(), fsLayers, history, imageConfig.Architecture)
	if err != nil {
		return nil, err // This should never happen, we should have created all the components correctly.
	}
	return m1, nil
}

func v1IDFromBlobDigestAndComponents(blobDigest digest.Digest, others ...string) (string, error) {
	if err := blobDigest.Validate(); err != nil {
		return "", err
	}
	parts := append([]string{blobDigest.Encoded()}, others...)
	v1IDHash := sha256.Sum256([]byte(strings.Join(parts, " ")))
	return hex.EncodeToString(v1IDHash[:]), nil
}

func v1ConfigFromConfigJSON(configJSON []byte, v1ID, parentV1ID string, throwaway bool) ([]byte, error) {
	// Preserve everything we don't specifically know about.
	// (This must be a *json.RawMessage, even though *[]byte is fairly redundant, because only *RawMessage implements json.Marshaler.)
	rawContents := map[string]*json.RawMessage{}
	if err := json.Unmarshal(configJSON, &rawContents); err != nil { // We have already unmarshaled it before, using a more detailed schema?!
		return nil, err
	}
	delete(rawContents, "rootfs")
	delete(rawContents, "history")

	updates := map[string]any{"id": v1ID}
	if parentV1ID != "" {
		updates["parent"] = parentV1ID
	}
	if throwaway {
		updates["throwaway"] = throwaway
	}
	for field, value := range updates {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		rawContents[field] = (*json.RawMessage)(&encoded)
	}
	return json.Marshal(rawContents)
}

// SupportsEncryption returns if encryption is supported for the manifest type
func (m *manifestSchema2) SupportsEncryption(context.Context) bool {
	return false
}

// CanChangeLayerCompression returns true if we can compress/decompress layers with mimeType in the current image
// (and the code can handle that).
// NOTE: Even if this returns true, the relevant format might not accept all compression algorithms; the set of accepted
// algorithms depends not on the current format, but possibly on the target of a conversion (if UpdatedImage converts
// to a different manifest format).
func (m *manifestSchema2) CanChangeLayerCompression(mimeType string) bool {
	return m.m.CanChangeLayerCompression(mimeType)
}
