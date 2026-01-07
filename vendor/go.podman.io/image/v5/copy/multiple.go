package copy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	internalManifest "go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/internal/set"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
)

type instanceCopyKind int

const (
	instanceCopyCopy instanceCopyKind = iota
	instanceCopyClone
)

type instanceCopy struct {
	op           instanceCopyKind
	sourceDigest digest.Digest

	// Fields which can be used by callers when operation
	// is `instanceCopyCopy`
	copyForceCompressionFormat bool

	// Fields which can be used by callers when operation
	// is `instanceCopyClone`
	cloneArtifactType       string
	cloneCompressionVariant OptionCompressionVariant
	clonePlatform           *imgspecv1.Platform
	cloneAnnotations        map[string]string
}

// internal type only to make imgspecv1.Platform comparable
type platformComparable struct {
	architecture string
	os           string
	osVersion    string
	osFeatures   string
	variant      string
}

// Converts imgspecv1.Platform to a comparable format.
func platformV1ToPlatformComparable(platform *imgspecv1.Platform) platformComparable {
	if platform == nil {
		return platformComparable{}
	}
	osFeatures := slices.Clone(platform.OSFeatures)
	sort.Strings(osFeatures)
	return platformComparable{architecture: platform.Architecture,
		os: platform.OS,
		// This is strictly speaking ambiguous, fields of OSFeatures can contain a ','. Probably good enough for now.
		osFeatures: strings.Join(osFeatures, ","),
		osVersion:  platform.OSVersion,
		variant:    platform.Variant,
	}
}

// platformCompressionMap prepares a mapping of platformComparable -> CompressionAlgorithmNames for given digests
func platformCompressionMap(list internalManifest.List, instanceDigests []digest.Digest) (map[platformComparable]*set.Set[string], error) {
	res := make(map[platformComparable]*set.Set[string])
	for _, instanceDigest := range instanceDigests {
		instanceDetails, err := list.Instance(instanceDigest)
		if err != nil {
			return nil, fmt.Errorf("getting details for instance %s: %w", instanceDigest, err)
		}
		platform := platformV1ToPlatformComparable(instanceDetails.ReadOnly.Platform)
		platformSet, ok := res[platform]
		if !ok {
			platformSet = set.New[string]()
			res[platform] = platformSet
		}
		platformSet.AddSeq(slices.Values(instanceDetails.ReadOnly.CompressionAlgorithmNames))
	}
	return res, nil
}

func validateCompressionVariantExists(input []OptionCompressionVariant) error {
	for _, option := range input {
		_, err := compression.AlgorithmByName(option.Algorithm.Name())
		if err != nil {
			return fmt.Errorf("invalid algorithm %q in option.EnsureCompressionVariantsExist: %w", option.Algorithm.Name(), err)
		}
	}
	return nil
}

// prepareInstanceCopies prepares a list of instances which needs to copied to the manifest list.
func prepareInstanceCopies(list internalManifest.List, instanceDigests []digest.Digest, options *Options) ([]instanceCopy, error) {
	res := []instanceCopy{}
	if options.ImageListSelection == CopySpecificImages && len(options.EnsureCompressionVariantsExist) > 0 {
		// List can already contain compressed instance for a compression selected in `EnsureCompressionVariantsExist`
		// It’s unclear what it means when `CopySpecificImages` includes an instance in options.Instances,
		// EnsureCompressionVariantsExist asks for an instance with some compression,
		// an instance with that compression already exists, but is not included in options.Instances.
		// We might define the semantics and implement this in the future.
		return res, fmt.Errorf("EnsureCompressionVariantsExist is not implemented for CopySpecificImages")
	}
	err := validateCompressionVariantExists(options.EnsureCompressionVariantsExist)
	if err != nil {
		return res, err
	}
	compressionsByPlatform, err := platformCompressionMap(list, instanceDigests)
	if err != nil {
		return nil, err
	}
	for i, instanceDigest := range instanceDigests {
		if options.ImageListSelection == CopySpecificImages &&
			!slices.Contains(options.Instances, instanceDigest) {
			logrus.Debugf("Skipping instance %s (%d/%d)", instanceDigest, i+1, len(instanceDigests))
			continue
		}
		instanceDetails, err := list.Instance(instanceDigest)
		if err != nil {
			return res, fmt.Errorf("getting details for instance %s: %w", instanceDigest, err)
		}
		forceCompressionFormat, err := shouldRequireCompressionFormatMatch(options)
		if err != nil {
			return nil, err
		}
		res = append(res, instanceCopy{
			op:                         instanceCopyCopy,
			sourceDigest:               instanceDigest,
			copyForceCompressionFormat: forceCompressionFormat,
		})
		platform := platformV1ToPlatformComparable(instanceDetails.ReadOnly.Platform)
		compressionList := compressionsByPlatform[platform]
		for _, compressionVariant := range options.EnsureCompressionVariantsExist {
			if !compressionList.Contains(compressionVariant.Algorithm.Name()) {
				res = append(res, instanceCopy{
					op:                      instanceCopyClone,
					sourceDigest:            instanceDigest,
					cloneArtifactType:       instanceDetails.ReadOnly.ArtifactType,
					cloneCompressionVariant: compressionVariant,
					clonePlatform:           instanceDetails.ReadOnly.Platform,
					cloneAnnotations:        maps.Clone(instanceDetails.ReadOnly.Annotations),
				})
				// add current compression to the list so that we don’t create duplicate clones
				compressionList.Add(compressionVariant.Algorithm.Name())
			}
		}
	}
	return res, nil
}

// copyMultipleImages copies some or all of an image list's instances, using
// c.policyContext to validate source image admissibility.
func (c *copier) copyMultipleImages(ctx context.Context) (copiedManifest []byte, retErr error) {
	// Parse the list and get a copy of the original value after it's re-encoded.
	manifestList, manifestType, err := c.unparsedToplevel.Manifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading manifest list: %w", err)
	}
	originalList, err := internalManifest.ListFromBlob(manifestList, manifestType)
	if err != nil {
		return nil, fmt.Errorf("parsing manifest list %q: %w", string(manifestList), err)
	}
	updatedList := originalList.CloneInternal()

	sigs, err := c.sourceSignatures(ctx, c.unparsedToplevel,
		"Getting image list signatures",
		"Checking if image list destination supports signatures")
	if err != nil {
		return nil, err
	}

	// If the destination is a digested reference, make a note of that, determine what digest value we're
	// expecting, and check that the source manifest matches it.
	destIsDigestedReference := false
	if named := c.dest.Reference().DockerReference(); named != nil {
		if digested, ok := named.(reference.Digested); ok {
			destIsDigestedReference = true
			matches, err := manifest.MatchesDigest(manifestList, digested.Digest())
			if err != nil {
				return nil, fmt.Errorf("computing digest of source image's manifest: %w", err)
			}
			if !matches {
				return nil, errors.New("Digest of source image's manifest would not match destination reference")
			}
		}
	}

	// Determine if we're allowed to modify the manifest list.
	// If we can, set to the empty string. If we can't, set to the reason why.
	// Compare, and perhaps keep in sync with, the version in copySingleImage.
	cannotModifyManifestListReason := ""
	if len(sigs) > 0 {
		cannotModifyManifestListReason = "Would invalidate signatures"
	}
	if destIsDigestedReference {
		cannotModifyManifestListReason = "Destination specifies a digest"
	}
	if c.options.PreserveDigests {
		cannotModifyManifestListReason = "Instructed to preserve digests"
	}

	// Determine if we'll need to convert the manifest list to a different format.
	forceListMIMEType := c.options.ForceManifestMIMEType
	switch forceListMIMEType {
	case manifest.DockerV2Schema1MediaType, manifest.DockerV2Schema1SignedMediaType, manifest.DockerV2Schema2MediaType:
		forceListMIMEType = manifest.DockerV2ListMediaType
	case imgspecv1.MediaTypeImageManifest:
		forceListMIMEType = imgspecv1.MediaTypeImageIndex
	}
	selectedListType, otherManifestMIMETypeCandidates, err := c.determineListConversion(manifestType, c.dest.SupportedManifestMIMETypes(), forceListMIMEType)
	if err != nil {
		return nil, fmt.Errorf("determining manifest list type to write to destination: %w", err)
	}
	if selectedListType != originalList.MIMEType() {
		if cannotModifyManifestListReason != "" {
			return nil, fmt.Errorf("Manifest list must be converted to type %q to be written to destination, but we cannot modify it: %q", selectedListType, cannotModifyManifestListReason)
		}
	}

	// Copy each image, or just the ones we want to copy, in turn.
	instanceDigests := updatedList.Instances()
	instanceEdits := []internalManifest.ListEdit{}
	instanceCopyList, err := prepareInstanceCopies(updatedList, instanceDigests, c.options)
	if err != nil {
		return nil, fmt.Errorf("preparing instances for copy: %w", err)
	}
	c.Printf("Copying %d images generated from %d images in list\n", len(instanceCopyList), len(instanceDigests))
	for i, instance := range instanceCopyList {
		// Update instances to be edited by their `ListOperation` and
		// populate necessary fields.
		switch instance.op {
		case instanceCopyCopy:
			logrus.Debugf("Copying instance %s (%d/%d)", instance.sourceDigest, i+1, len(instanceCopyList))
			c.Printf("Copying image %s (%d/%d)\n", instance.sourceDigest, i+1, len(instanceCopyList))
			unparsedInstance := image.UnparsedInstance(c.rawSource, &instanceCopyList[i].sourceDigest)
			updated, err := c.copySingleImage(ctx, unparsedInstance, &instanceCopyList[i].sourceDigest, copySingleImageOptions{requireCompressionFormatMatch: instance.copyForceCompressionFormat})
			if err != nil {
				return nil, fmt.Errorf("copying image %d/%d from manifest list: %w", i+1, len(instanceCopyList), err)
			}
			// Record the result of a possible conversion here.
			instanceEdits = append(instanceEdits, internalManifest.ListEdit{
				ListOperation:               internalManifest.ListOpUpdate,
				UpdateOldDigest:             instance.sourceDigest,
				UpdateDigest:                updated.manifestDigest,
				UpdateSize:                  int64(len(updated.manifest)),
				UpdateCompressionAlgorithms: updated.compressionAlgorithms,
				UpdateMediaType:             updated.manifestMIMEType})
		case instanceCopyClone:
			logrus.Debugf("Replicating instance %s (%d/%d)", instance.sourceDigest, i+1, len(instanceCopyList))
			c.Printf("Replicating image %s (%d/%d)\n", instance.sourceDigest, i+1, len(instanceCopyList))
			unparsedInstance := image.UnparsedInstance(c.rawSource, &instanceCopyList[i].sourceDigest)
			updated, err := c.copySingleImage(ctx, unparsedInstance, &instanceCopyList[i].sourceDigest, copySingleImageOptions{
				requireCompressionFormatMatch: true,
				compressionFormat:             &instance.cloneCompressionVariant.Algorithm,
				compressionLevel:              instance.cloneCompressionVariant.Level})
			if err != nil {
				return nil, fmt.Errorf("replicating image %d/%d from manifest list: %w", i+1, len(instanceCopyList), err)
			}
			// Record the result of a possible conversion here.
			instanceEdits = append(instanceEdits, internalManifest.ListEdit{
				ListOperation:            internalManifest.ListOpAdd,
				AddDigest:                updated.manifestDigest,
				AddSize:                  int64(len(updated.manifest)),
				AddMediaType:             updated.manifestMIMEType,
				AddArtifactType:          instance.cloneArtifactType,
				AddPlatform:              instance.clonePlatform,
				AddAnnotations:           instance.cloneAnnotations,
				AddCompressionAlgorithms: updated.compressionAlgorithms,
			})
		default:
			return nil, fmt.Errorf("copying image: invalid copy operation %d", instance.op)
		}
	}

	// Now reset the digest/size/types of the manifests in the list to account for any conversions that we made.
	if err = updatedList.EditInstances(instanceEdits); err != nil {
		return nil, fmt.Errorf("updating manifest list: %w", err)
	}

	// Iterate through supported list types, preferred format first.
	c.Printf("Writing manifest list to image destination\n")
	var errs []string
	for _, thisListType := range append([]string{selectedListType}, otherManifestMIMETypeCandidates...) {
		var attemptedList internalManifest.ListPublic = updatedList

		logrus.Debugf("Trying to use manifest list type %s…", thisListType)

		// Perform the list conversion, if we need one.
		if thisListType != updatedList.MIMEType() {
			attemptedList, err = updatedList.ConvertToMIMEType(thisListType)
			if err != nil {
				return nil, fmt.Errorf("converting manifest list to list with MIME type %q: %w", thisListType, err)
			}
		}

		// Check if the updates or a type conversion meaningfully changed the list of images
		// by serializing them both so that we can compare them.
		attemptedManifestList, err := attemptedList.Serialize()
		if err != nil {
			return nil, fmt.Errorf("encoding updated manifest list (%q: %#v): %w", updatedList.MIMEType(), updatedList.Instances(), err)
		}
		originalManifestList, err := originalList.Serialize()
		if err != nil {
			return nil, fmt.Errorf("encoding original manifest list for comparison (%q: %#v): %w", originalList.MIMEType(), originalList.Instances(), err)
		}

		// If we can't just use the original value, but we have to change it, flag an error.
		if !bytes.Equal(attemptedManifestList, originalManifestList) {
			if cannotModifyManifestListReason != "" {
				return nil, fmt.Errorf("Manifest list must be converted to type %q to be written to destination, but we cannot modify it: %q", thisListType, cannotModifyManifestListReason)
			}
			logrus.Debugf("Manifest list has been updated")
		} else {
			// We can just use the original value, so use it instead of the one we just rebuilt, so that we don't change the digest.
			attemptedManifestList = manifestList
		}

		// Save the manifest list.
		err = c.dest.PutManifest(ctx, attemptedManifestList, nil)
		if err != nil {
			logrus.Debugf("Upload of manifest list type %s failed: %v", thisListType, err)
			errs = append(errs, fmt.Sprintf("%s(%v)", thisListType, err))
			continue
		}
		errs = nil
		manifestList = attemptedManifestList
		break
	}
	if errs != nil {
		return nil, fmt.Errorf("Uploading manifest list failed, attempted the following formats: %s", strings.Join(errs, ", "))
	}

	// Sign the manifest list.
	newSigs, err := c.createSignatures(ctx, manifestList, c.options.SignIdentity)
	if err != nil {
		return nil, err
	}
	sigs = append(slices.Clone(sigs), newSigs...)

	c.Printf("Storing list signatures\n")
	if err := c.dest.PutSignaturesWithFormat(ctx, sigs, nil); err != nil {
		return nil, fmt.Errorf("writing signatures: %w", err)
	}

	return manifestList, nil
}
