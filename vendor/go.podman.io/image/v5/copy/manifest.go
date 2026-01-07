package copy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	internalManifest "go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/internal/set"
	"go.podman.io/image/v5/manifest"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

// preferredManifestMIMETypes lists manifest MIME types in order of our preference, if we can't use the original manifest and need to convert.
// Prefer v2s2 to v2s1 because v2s2 does not need to be changed when uploading to a different location.
// Include v2s1 signed but not v2s1 unsigned, because docker/distribution requires a signature even if the unsigned MIME type is used.
var preferredManifestMIMETypes = []string{manifest.DockerV2Schema2MediaType, manifest.DockerV2Schema1SignedMediaType}

// allManifestMIMETypes lists all possible manifest MIME types.
var allManifestMIMETypes = []string{v1.MediaTypeImageManifest, manifest.DockerV2Schema2MediaType, manifest.DockerV2Schema1SignedMediaType, manifest.DockerV2Schema1MediaType}

// orderedSet is a list of strings (MIME types or platform descriptors in our case), with each string appearing at most once.
type orderedSet struct {
	list     []string
	included *set.Set[string]
}

// newOrderedSet creates a correctly initialized orderedSet.
// [Sometimes it would be really nice if Golang had constructors…]
func newOrderedSet() *orderedSet {
	return &orderedSet{
		list:     []string{},
		included: set.New[string](),
	}
}

// append adds s to the end of os, only if it is not included already.
func (os *orderedSet) append(s string) {
	if !os.included.Contains(s) {
		os.list = append(os.list, s)
		os.included.Add(s)
	}
}

// determineManifestConversionInputs contains the inputs for determineManifestConversion.
type determineManifestConversionInputs struct {
	srcMIMEType string // MIME type of the input manifest

	destSupportedManifestMIMETypes []string // MIME types supported by the destination, per types.ImageDestination.SupportedManifestMIMETypes()

	forceManifestMIMEType      string                      // User’s choice of forced manifest MIME type
	requestedCompressionFormat *compressiontypes.Algorithm // Compression algorithm to use, if the user _explictily_ requested one.
	requiresOCIEncryption      bool                        // Restrict to manifest formats that can support OCI encryption
	cannotModifyManifestReason string                      // The reason the manifest cannot be modified, or an empty string if it can
}

// manifestConversionPlan contains the decisions made by determineManifestConversion.
type manifestConversionPlan struct {
	// The preferred manifest MIME type (whether we are converting to it or using it unmodified).
	// We compute this only to show it in error messages; without having to add this context
	// in an error message, we would be happy enough to know only that no conversion is needed.
	preferredMIMEType                string
	preferredMIMETypeNeedsConversion bool     // True if using preferredMIMEType requires a conversion step.
	otherMIMETypeCandidates          []string // Other possible alternatives, in order
}

// determineManifestConversion returns a plan for what formats, and possibly conversions, to use based on in.
func determineManifestConversion(in determineManifestConversionInputs) (manifestConversionPlan, error) {
	srcType := in.srcMIMEType
	normalizedSrcType := manifest.NormalizedMIMEType(srcType)
	if srcType != normalizedSrcType {
		logrus.Debugf("Source manifest MIME type %q, treating it as %q", srcType, normalizedSrcType)
		srcType = normalizedSrcType
	}

	destSupportedManifestMIMETypes := in.destSupportedManifestMIMETypes
	if in.forceManifestMIMEType != "" {
		destSupportedManifestMIMETypes = []string{in.forceManifestMIMEType}
	}
	if len(destSupportedManifestMIMETypes) == 0 {
		destSupportedManifestMIMETypes = allManifestMIMETypes
	}

	restrictiveCompressionRequired := in.requestedCompressionFormat != nil && !internalManifest.CompressionAlgorithmIsUniversallySupported(*in.requestedCompressionFormat)
	supportedByDest := set.New[string]()
	for _, t := range destSupportedManifestMIMETypes {
		if in.requiresOCIEncryption && !manifest.MIMETypeSupportsEncryption(t) {
			continue
		}
		if restrictiveCompressionRequired && !internalManifest.MIMETypeSupportsCompressionAlgorithm(t, *in.requestedCompressionFormat) {
			continue
		}
		supportedByDest.Add(t)
	}
	if supportedByDest.Empty() {
		if len(destSupportedManifestMIMETypes) == 0 { // Coverage: This should never happen, empty values were replaced by allManifestMIMETypes
			return manifestConversionPlan{}, errors.New("internal error: destSupportedManifestMIMETypes is empty")
		}
		// We know, and have verified, that destSupportedManifestMIMETypes is not empty, so some filtering of supported MIME types must have been involved.

		// destSupportedManifestMIMETypes has three possible origins:
		if in.forceManifestMIMEType != "" { // 1. forceManifestType specified
			switch {
			case in.requiresOCIEncryption && restrictiveCompressionRequired:
				return manifestConversionPlan{}, fmt.Errorf("compression using %s, and encryption, required together with format %s, which does not support both",
					in.requestedCompressionFormat.Name(), in.forceManifestMIMEType)
			case in.requiresOCIEncryption:
				return manifestConversionPlan{}, fmt.Errorf("encryption required together with format %s, which does not support encryption",
					in.forceManifestMIMEType)
			case restrictiveCompressionRequired:
				return manifestConversionPlan{}, fmt.Errorf("compression using %s required together with format %s, which does not support it",
					in.requestedCompressionFormat.Name(), in.forceManifestMIMEType)
			default:
				return manifestConversionPlan{}, errors.New("internal error: forceManifestMIMEType was rejected for an unknown reason")
			}
		}
		if len(in.destSupportedManifestMIMETypes) == 0 { // 2. destination accepts anything and we have chosen allManifestTypes
			if !restrictiveCompressionRequired {
				// Coverage: This should never happen.
				// If we have not rejected for encryption reasons, we must have rejected due to encryption, but
				// allManifestTypes includes OCI, which supports encryption.
				return manifestConversionPlan{}, errors.New("internal error: in.destSupportedManifestMIMETypes is empty but supportedByDest is empty as well")
			}
			// This can legitimately happen when the user asks for completely unsupported formats like Bzip2 or Xz.
			return manifestConversionPlan{}, fmt.Errorf("compression using %s required, but none of the known manifest formats support it", in.requestedCompressionFormat.Name())
		}
		// 3. destination accepts a restricted list of mime types
		destMIMEList := strings.Join(destSupportedManifestMIMETypes, ", ")
		switch {
		case in.requiresOCIEncryption && restrictiveCompressionRequired:
			return manifestConversionPlan{}, fmt.Errorf("compression using %s, and encryption, required but the destination only supports MIME types [%s], none of which support both",
				in.requestedCompressionFormat.Name(), destMIMEList)
		case in.requiresOCIEncryption:
			return manifestConversionPlan{}, fmt.Errorf("encryption required but the destination only supports MIME types [%s], none of which support encryption",
				destMIMEList)
		case restrictiveCompressionRequired:
			return manifestConversionPlan{}, fmt.Errorf("compression using %s required but the destination only supports MIME types [%s], none of which support it",
				in.requestedCompressionFormat.Name(), destMIMEList)
		default: // Coverage: This should never happen, we only filter for in.requiresOCIEncryption || restrictiveCompressionRequired
			return manifestConversionPlan{}, errors.New("internal error: supportedByDest is empty but destSupportedManifestMIMETypes is not, and we are neither encrypting nor requiring a restrictive compression algorithm")
		}
	}

	// destSupportedManifestMIMETypes is a static guess; a particular registry may still only support a subset of the types.
	// So, build a list of types to try in order of decreasing preference.
	// FIXME? This treats manifest.DockerV2Schema1SignedMediaType and manifest.DockerV2Schema1MediaType as distinct,
	// although we are not really making any conversion, and it is very unlikely that a destination would support one but not the other.
	// In practice, schema1 is probably the lowest common denominator, so we would expect to try the first one of the MIME types
	// and never attempt the other one.
	prioritizedTypes := newOrderedSet()

	// First of all, prefer to keep the original manifest unmodified.
	if supportedByDest.Contains(srcType) {
		prioritizedTypes.append(srcType)
	}
	if in.cannotModifyManifestReason != "" {
		// We could also drop this check and have the caller
		// make the choice; it is already doing that to an extent, to improve error
		// messages.  But it is nice to hide the “if we can't modify, do no conversion”
		// special case in here; the caller can then worry (or not) only about a good UI.
		logrus.Debugf("We can't modify the manifest, hoping for the best...")
		return manifestConversionPlan{ // Take our chances - FIXME? Or should we fail without trying?
			preferredMIMEType:       srcType,
			otherMIMETypeCandidates: []string{},
		}, nil
	}

	// Then use our list of preferred types.
	for _, t := range preferredManifestMIMETypes {
		if supportedByDest.Contains(t) {
			prioritizedTypes.append(t)
		}
	}

	// Finally, try anything else the destination supports.
	for _, t := range destSupportedManifestMIMETypes {
		if supportedByDest.Contains(t) {
			prioritizedTypes.append(t)
		}
	}

	logrus.Debugf("Manifest has MIME type %s, ordered candidate list [%s]", srcType, strings.Join(prioritizedTypes.list, ", "))
	if len(prioritizedTypes.list) == 0 { // Coverage: destSupportedManifestMIMETypes and supportedByDest, which is a subset, is not empty (or we would have exited above), so this should never happen.
		return manifestConversionPlan{}, errors.New("Internal error: no candidate MIME types")
	}
	res := manifestConversionPlan{
		preferredMIMEType:       prioritizedTypes.list[0],
		otherMIMETypeCandidates: prioritizedTypes.list[1:],
	}
	res.preferredMIMETypeNeedsConversion = res.preferredMIMEType != srcType
	if !res.preferredMIMETypeNeedsConversion {
		logrus.Debugf("... will first try using the original manifest unmodified")
	}
	return res, nil
}

// isMultiImage returns true if img is a list of images
func isMultiImage(ctx context.Context, img types.UnparsedImage) (bool, error) {
	_, mt, err := img.Manifest(ctx)
	if err != nil {
		return false, err
	}
	return manifest.MIMETypeIsMultiImage(mt), nil
}

// determineListConversion takes the current MIME type of a list of manifests,
// the list of MIME types supported for a given destination, and a possible
// forced value, and returns the MIME type to which we should convert the list
// of manifests (regardless of whether we are converting to it or using it
// unmodified) and a slice of other list types which might be supported by the
// destination.
func (c *copier) determineListConversion(currentListMIMEType string, destSupportedMIMETypes []string, forcedListMIMEType string) (string, []string, error) {
	// If there's no list of supported types, then anything we support is expected to be supported.
	if len(destSupportedMIMETypes) == 0 {
		destSupportedMIMETypes = manifest.SupportedListMIMETypes
	}
	// If we're forcing it, replace the list of supported types with the forced value.
	if forcedListMIMEType != "" {
		destSupportedMIMETypes = []string{forcedListMIMEType}
	}

	prioritizedTypes := newOrderedSet()
	// The first priority is the current type, if it's in the list, since that lets us avoid a
	// conversion that isn't strictly necessary.
	if slices.Contains(destSupportedMIMETypes, currentListMIMEType) {
		prioritizedTypes.append(currentListMIMEType)
	}
	// Pick out the other list types that we support.
	for _, t := range destSupportedMIMETypes {
		if manifest.MIMETypeIsMultiImage(t) {
			prioritizedTypes.append(t)
		}
	}

	logrus.Debugf("Manifest list has MIME type %q, ordered candidate list [%s]", currentListMIMEType, strings.Join(destSupportedMIMETypes, ", "))
	if len(prioritizedTypes.list) == 0 {
		return "", nil, fmt.Errorf("destination does not support any supported manifest list types (%v)", manifest.SupportedListMIMETypes)
	}
	selectedType := prioritizedTypes.list[0]
	otherSupportedTypes := prioritizedTypes.list[1:]
	if selectedType != currentListMIMEType {
		logrus.Debugf("... will convert to %s first, and then try %v", selectedType, otherSupportedTypes)
	} else {
		logrus.Debugf("... will use the original manifest list type, and then try %v", otherSupportedTypes)
	}
	// Done.
	return selectedType, otherSupportedTypes, nil
}
