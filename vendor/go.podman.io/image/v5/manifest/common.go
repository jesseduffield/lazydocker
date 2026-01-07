package manifest

import (
	"fmt"

	"github.com/sirupsen/logrus"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

// layerInfosToStrings converts a list of layer infos, presumably obtained from a Manifest.LayerInfos()
// method call, into a format suitable for inclusion in a types.ImageInspectInfo structure.
func layerInfosToStrings(infos []LayerInfo) []string {
	layers := make([]string, len(infos))
	for i, info := range infos {
		layers[i] = info.Digest.String()
	}
	return layers
}

// compressionMIMETypeSet describes a set of MIME type “variants” that represent differently-compressed
// versions of “the same kind of content”.
// The map key is the return value of compressiontypes.Algorithm.Name(), or mtsUncompressed;
// the map value is a MIME type, or mtsUnsupportedMIMEType to mean "recognized but unsupported".
type compressionMIMETypeSet map[string]string

const mtsUncompressed = ""        // A key in compressionMIMETypeSet for the uncompressed variant
const mtsUnsupportedMIMEType = "" // A value in compressionMIMETypeSet that means “recognized but unsupported”

// findCompressionMIMETypeSet returns a pointer to a compressionMIMETypeSet in variantTable that contains a value of mimeType, or nil if not found
func findCompressionMIMETypeSet(variantTable []compressionMIMETypeSet, mimeType string) compressionMIMETypeSet {
	for _, variants := range variantTable {
		for _, mt := range variants {
			if mt == mimeType {
				return variants
			}
		}
	}
	return nil
}

// compressionVariantMIMEType returns a variant of mimeType for the specified algorithm (which may be nil
// to mean "no compression"), based on variantTable.
// The returned error will be a ManifestLayerCompressionIncompatibilityError if mimeType has variants
// that differ only in what type of compression is applied, but it can't be combined with this
// algorithm to produce an updated MIME type that complies with the standard that defines mimeType.
// If the compression algorithm is unrecognized, or mimeType is not known to have variants that
// differ from it only in what type of compression has been applied, the returned error will not be
// a ManifestLayerCompressionIncompatibilityError.
func compressionVariantMIMEType(variantTable []compressionMIMETypeSet, mimeType string, algorithm *compressiontypes.Algorithm) (string, error) {
	if mimeType == mtsUnsupportedMIMEType { // Prevent matching against the {algo:mtsUnsupportedMIMEType} entries
		return "", fmt.Errorf("cannot update unknown MIME type")
	}
	variants := findCompressionMIMETypeSet(variantTable, mimeType)
	if variants != nil {
		name := mtsUncompressed
		if algorithm != nil {
			name = algorithm.BaseVariantName()
		}
		if res, ok := variants[name]; ok {
			if res != mtsUnsupportedMIMEType {
				return res, nil
			}
			if name != mtsUncompressed {
				return "", ManifestLayerCompressionIncompatibilityError{fmt.Sprintf("%s compression is not supported for type %q", name, mimeType)}
			}
			return "", ManifestLayerCompressionIncompatibilityError{fmt.Sprintf("uncompressed variant is not supported for type %q", mimeType)}
		}
		if name != mtsUncompressed {
			return "", ManifestLayerCompressionIncompatibilityError{fmt.Sprintf("unknown compressed with algorithm %s variant for type %q", name, mimeType)}
		}
		// We can't very well say “the idea of no compression is unknown”
		return "", ManifestLayerCompressionIncompatibilityError{fmt.Sprintf("uncompressed variant is not supported for type %q", mimeType)}
	}
	if algorithm != nil {
		return "", fmt.Errorf("unsupported MIME type for compression: %q", mimeType)
	}
	return "", fmt.Errorf("unsupported MIME type for decompression: %q", mimeType)
}

// updatedMIMEType returns the result of applying edits in updated (MediaType, CompressionOperation) to
// mimeType, based on variantTable.  It may use updated.Digest for error messages.
// The returned error will be a ManifestLayerCompressionIncompatibilityError if mimeType has variants
// that differ only in what type of compression is applied, but applying updated.CompressionOperation
// and updated.CompressionAlgorithm to it won't produce an updated MIME type that complies with the
// standard that defines mimeType.
func updatedMIMEType(variantTable []compressionMIMETypeSet, mimeType string, updated types.BlobInfo) (string, error) {
	// Note that manifests in containers-storage might be reporting the
	// wrong media type since the original manifests are stored while layers
	// are decompressed in storage.  Hence, we need to consider the case
	// that an already {de}compressed layer should be {de}compressed;
	// compressionVariantMIMEType does that by not caring whether the original is
	// {de}compressed.
	switch updated.CompressionOperation {
	case types.PreserveOriginal:
		// Force a change to the media type if we're being told to use a particular compressor,
		// since it might be different from the one associated with the media type.  Otherwise,
		// try to keep the original media type.
		if updated.CompressionAlgorithm != nil {
			return compressionVariantMIMEType(variantTable, mimeType, updated.CompressionAlgorithm)
		}
		// Keep the original media type.
		return mimeType, nil

	case types.Decompress:
		return compressionVariantMIMEType(variantTable, mimeType, nil)

	case types.Compress:
		if updated.CompressionAlgorithm == nil {
			logrus.Debugf("Error preparing updated manifest: blob %q was compressed but does not specify by which algorithm: falling back to use the original blob", updated.Digest)
			return mimeType, nil
		}
		return compressionVariantMIMEType(variantTable, mimeType, updated.CompressionAlgorithm)

	default:
		return "", fmt.Errorf("unknown compression operation (%d)", updated.CompressionOperation)
	}
}

// ManifestLayerCompressionIncompatibilityError indicates that a specified compression algorithm
// could not be applied to a layer MIME type.  A caller that receives this should either retry
// the call with a different compression algorithm, or attempt to use a different manifest type.
type ManifestLayerCompressionIncompatibilityError struct {
	text string
}

func (m ManifestLayerCompressionIncompatibilityError) Error() string {
	return m.text
}

// compressionVariantsRecognizeMIMEType returns true if variantTable contains data about compressing/decompressing layers with mimeType
// Note that the caller still needs to worry about a specific algorithm not being supported.
func compressionVariantsRecognizeMIMEType(variantTable []compressionMIMETypeSet, mimeType string) bool {
	if mimeType == mtsUnsupportedMIMEType { // Prevent matching against the {algo:mtsUnsupportedMIMEType} entries
		return false
	}
	variants := findCompressionMIMETypeSet(variantTable, mimeType)
	return variants != nil // Alternatively, this could be len(variants) > 1, but really the caller should ask about a specific algorithm.
}

// imgInspectLayersFromLayerInfos converts a list of layer infos, presumably obtained from a Manifest.LayerInfos()
// method call, into a format suitable for inclusion in a types.ImageInspectInfo structure.
func imgInspectLayersFromLayerInfos(infos []LayerInfo) []types.ImageInspectLayer {
	layers := make([]types.ImageInspectLayer, len(infos))
	for i, info := range infos {
		layers[i].MIMEType = info.MediaType
		layers[i].Digest = info.Digest
		layers[i].Size = info.Size
		layers[i].Annotations = info.Annotations
	}
	return layers
}
