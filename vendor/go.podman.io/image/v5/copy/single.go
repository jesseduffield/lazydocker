package copy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb/v8"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/internal/pkg/platform"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/set"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/compression"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	chunkedToc "go.podman.io/storage/pkg/chunked/toc"
)

// imageCopier tracks state specific to a single image (possibly an item of a manifest list)
type imageCopier struct {
	c                             *copier
	manifestUpdates               *types.ManifestUpdateOptions
	src                           *image.SourcedImage
	manifestConversionPlan        manifestConversionPlan
	diffIDsAreNeeded              bool
	cannotModifyManifestReason    string // The reason the manifest cannot be modified, or an empty string if it can
	canSubstituteBlobs            bool
	compressionFormat             *compressiontypes.Algorithm // Compression algorithm to use, if the user explicitly requested one, or nil.
	compressionLevel              *int
	requireCompressionFormatMatch bool
}

type copySingleImageOptions struct {
	requireCompressionFormatMatch bool
	compressionFormat             *compressiontypes.Algorithm // Compression algorithm to use, if the user explicitly requested one, or nil.
	compressionLevel              *int
}

// copySingleImageResult carries data produced by copySingleImage
type copySingleImageResult struct {
	manifest              []byte
	manifestMIMEType      string
	manifestDigest        digest.Digest
	compressionAlgorithms []compressiontypes.Algorithm
}

// copySingleImage copies a single (non-manifest-list) image unparsedImage, using c.policyContext to validate
// source image admissibility.
func (c *copier) copySingleImage(ctx context.Context, unparsedImage *image.UnparsedImage, targetInstance *digest.Digest, opts copySingleImageOptions) (copySingleImageResult, error) {
	// The caller is handling manifest lists; this could happen only if a manifest list contains a manifest list.
	// Make sure we fail cleanly in such cases.
	multiImage, err := isMultiImage(ctx, unparsedImage)
	if err != nil {
		// FIXME FIXME: How to name a reference for the sub-image?
		return copySingleImageResult{}, fmt.Errorf("determining manifest MIME type for %s: %w", transports.ImageName(unparsedImage.Reference()), err)
	}
	if multiImage {
		return copySingleImageResult{}, fmt.Errorf("Unexpectedly received a manifest list instead of a manifest for a single image")
	}

	// Please keep this policy check BEFORE reading any other information about the image.
	// (The multiImage check above only matches the MIME type, which we have received anyway.
	// Actual parsing of anything should be deferred.)
	if allowed, err := c.policyContext.IsRunningImageAllowed(ctx, unparsedImage); !allowed || err != nil { // Be paranoid and fail if either return value indicates so.
		return copySingleImageResult{}, fmt.Errorf("Source image rejected: %w", err)
	}
	src, err := image.FromUnparsedImage(ctx, c.options.SourceCtx, unparsedImage)
	if err != nil {
		return copySingleImageResult{}, fmt.Errorf("initializing image from source %s: %w", transports.ImageName(c.rawSource.Reference()), err)
	}

	// If the destination is a digested reference, make a note of that, determine what digest value we're
	// expecting, and check that the source manifest matches it.  If the source manifest doesn't, but it's
	// one item from a manifest list that matches it, accept that as a match.
	destIsDigestedReference := false
	if named := c.dest.Reference().DockerReference(); named != nil {
		if digested, ok := named.(reference.Digested); ok {
			destIsDigestedReference = true
			matches, err := manifest.MatchesDigest(src.ManifestBlob, digested.Digest())
			if err != nil {
				return copySingleImageResult{}, fmt.Errorf("computing digest of source image's manifest: %w", err)
			}
			if !matches {
				manifestList, _, err := c.unparsedToplevel.Manifest(ctx)
				if err != nil {
					return copySingleImageResult{}, fmt.Errorf("reading manifest from source image: %w", err)
				}
				matches, err = manifest.MatchesDigest(manifestList, digested.Digest())
				if err != nil {
					return copySingleImageResult{}, fmt.Errorf("computing digest of source image's manifest: %w", err)
				}
				if !matches {
					return copySingleImageResult{}, errors.New("Digest of source image's manifest would not match destination reference")
				}
			}
		}
	}

	if err := prepareImageConfigForDest(ctx, c.options.DestinationCtx, src, c.dest); err != nil {
		return copySingleImageResult{}, err
	}

	sigs, err := c.sourceSignatures(ctx, src,
		"Getting image source signatures",
		"Checking if image destination supports signatures")
	if err != nil {
		return copySingleImageResult{}, err
	}

	// Determine if we're allowed to modify the manifest.
	// If we can, set to the empty string. If we can't, set to the reason why.
	// Compare, and perhaps keep in sync with, the version in copyMultipleImages.
	cannotModifyManifestReason := ""
	if len(sigs) > 0 {
		cannotModifyManifestReason = "Would invalidate signatures"
	}
	if destIsDigestedReference {
		cannotModifyManifestReason = "Destination specifies a digest"
	}
	if c.options.PreserveDigests {
		cannotModifyManifestReason = "Instructed to preserve digests"
	}

	ic := imageCopier{
		c:               c,
		manifestUpdates: &types.ManifestUpdateOptions{InformationOnly: types.ManifestUpdateInformation{Destination: c.dest}},
		src:             src,
		// manifestConversionPlan and diffIDsAreNeeded are computed later
		cannotModifyManifestReason:    cannotModifyManifestReason,
		requireCompressionFormatMatch: opts.requireCompressionFormatMatch,
	}
	if opts.compressionFormat != nil {
		ic.compressionFormat = opts.compressionFormat
		ic.compressionLevel = opts.compressionLevel
	} else if c.options.DestinationCtx != nil {
		// Note that compressionFormat and compressionLevel can be nil.
		ic.compressionFormat = c.options.DestinationCtx.CompressionFormat
		ic.compressionLevel = c.options.DestinationCtx.CompressionLevel
	}
	// HACK: Don’t combine zstd:chunked and encryption.
	// zstd:chunked can only usefully be consumed using range requests of parts of the layer, which would require the encryption
	// to support decrypting arbitrary subsets of the stream. That’s plausible but not supported using the encryption API we have.
	// Also, the chunked metadata is exposed in annotations unencrypted, which reveals the TOC digest = layer identity without
	// encryption. (That can be determined from the unencrypted config anyway, but, still...)
	//
	// Ideally this should query a well-defined property of the compression algorithm (and $somehow determine the right fallback) instead of
	// hard-coding zstd:chunked / zstd.
	if ic.c.options.OciEncryptLayers != nil {
		format := ic.compressionFormat
		if format == nil {
			format = defaultCompressionFormat
		}
		if format.Name() == compressiontypes.ZstdChunkedAlgorithmName {
			if ic.requireCompressionFormatMatch {
				return copySingleImageResult{}, errors.New("explicitly requested to combine zstd:chunked with encryption, which is not beneficial; use plain zstd instead")
			}
			logrus.Warnf("Compression using zstd:chunked is not beneficial for encrypted layers, using plain zstd instead")
			ic.compressionFormat = &compression.Zstd
		}
	}

	// Decide whether we can substitute blobs with semantic equivalents:
	// - Don’t do that if we can’t modify the manifest at all
	// - Ensure _this_ copy sees exactly the intended data when either processing a signed image or signing it.
	//   This may be too conservative, but for now, better safe than sorry, _especially_ on the len(c.signers) != 0 path:
	//   The signature makes the content non-repudiable, so it very much matters that the signature is made over exactly what the user intended.
	//   We do intend the RecordDigestUncompressedPair calls to only work with reliable data, but at least there’s a risk
	//   that the compressed version coming from a third party may be designed to attack some other decompressor implementation,
	//   and we would reuse and sign it.
	ic.canSubstituteBlobs = ic.cannotModifyManifestReason == "" && len(c.signers) == 0

	if err := ic.updateEmbeddedDockerReference(); err != nil {
		return copySingleImageResult{}, err
	}

	destRequiresOciEncryption := (isEncrypted(src) && ic.c.options.OciDecryptConfig == nil) || c.options.OciEncryptLayers != nil

	ic.manifestConversionPlan, err = determineManifestConversion(determineManifestConversionInputs{
		srcMIMEType:                    ic.src.ManifestMIMEType,
		destSupportedManifestMIMETypes: ic.c.dest.SupportedManifestMIMETypes(),
		forceManifestMIMEType:          c.options.ForceManifestMIMEType,
		requestedCompressionFormat:     ic.compressionFormat,
		requiresOCIEncryption:          destRequiresOciEncryption,
		cannotModifyManifestReason:     ic.cannotModifyManifestReason,
	})
	if err != nil {
		return copySingleImageResult{}, err
	}
	// We set up this part of ic.manifestUpdates quite early, not just around the
	// code that calls copyUpdatedConfigAndManifest, so that other parts of the copy code
	// (e.g. the UpdatedImageNeedsLayerDiffIDs check just below) can make decisions based
	// on the expected destination format.
	if ic.manifestConversionPlan.preferredMIMETypeNeedsConversion {
		ic.manifestUpdates.ManifestMIMEType = ic.manifestConversionPlan.preferredMIMEType
	}

	// If src.UpdatedImageNeedsLayerDiffIDs(ic.manifestUpdates) will be true, it needs to be true by the time we get here.
	ic.diffIDsAreNeeded = src.UpdatedImageNeedsLayerDiffIDs(*ic.manifestUpdates)

	// If enabled, fetch and compare the destination's manifest. And as an optimization skip updating the destination iff equal
	if c.options.OptimizeDestinationImageAlreadyExists {
		shouldUpdateSigs := len(sigs) > 0 || len(c.signers) != 0 // TODO: Consider allowing signatures updates only and skipping the image's layers/manifest copy if possible
		noPendingManifestUpdates := ic.noPendingManifestUpdates()

		logrus.Debugf("Checking if we can skip copying: has signatures=%t, OCI encryption=%t, no manifest updates=%t, compression match required for reusing blobs=%t", shouldUpdateSigs, destRequiresOciEncryption, noPendingManifestUpdates, opts.requireCompressionFormatMatch)
		if !shouldUpdateSigs && !destRequiresOciEncryption && noPendingManifestUpdates && !ic.requireCompressionFormatMatch {
			matchedResult, err := ic.compareImageDestinationManifestEqual(ctx, targetInstance)
			if err != nil {
				logrus.Warnf("Failed to compare destination image manifest: %v", err)
				return copySingleImageResult{}, err
			}

			if matchedResult != nil {
				c.Printf("Skipping: image already present at destination\n")
				return *matchedResult, nil
			}
		}
	}

	compressionAlgos, err := ic.copyLayers(ctx)
	if err != nil {
		return copySingleImageResult{}, err
	}

	// With docker/distribution registries we do not know whether the registry accepts schema2 or schema1 only;
	// and at least with the OpenShift registry "acceptschema2" option, there is no way to detect the support
	// without actually trying to upload something and getting a types.ManifestTypeRejectedError.
	// So, try the preferred manifest MIME type with possibly-updated blob digests, media types, and sizes if
	// we're altering how they're compressed.  If the process succeeds, fine…
	manifestBytes, manifestDigest, err := ic.copyUpdatedConfigAndManifest(ctx, targetInstance)
	wipResult := copySingleImageResult{
		manifest:         manifestBytes,
		manifestMIMEType: ic.manifestConversionPlan.preferredMIMEType,
		manifestDigest:   manifestDigest,
	}
	if err != nil {
		logrus.Debugf("Writing manifest using preferred type %s failed: %v", ic.manifestConversionPlan.preferredMIMEType, err)
		// … if it fails, and the failure is either because the manifest is rejected by the registry, or
		// because we failed to create a manifest of the specified type because the specific manifest type
		// doesn't support the type of compression we're trying to use (e.g. docker v2s2 and zstd), we may
		// have other options available that could still succeed.
		var manifestTypeRejectedError types.ManifestTypeRejectedError
		var manifestLayerCompressionIncompatibilityError manifest.ManifestLayerCompressionIncompatibilityError
		isManifestRejected := errors.As(err, &manifestTypeRejectedError)
		isCompressionIncompatible := errors.As(err, &manifestLayerCompressionIncompatibilityError)
		if (!isManifestRejected && !isCompressionIncompatible) || len(ic.manifestConversionPlan.otherMIMETypeCandidates) == 0 {
			// We don’t have other options.
			// In principle the code below would handle this as well, but the resulting  error message is fairly ugly.
			// Don’t bother the user with MIME types if we have no choice.
			return copySingleImageResult{}, err
		}
		// If the original MIME type is acceptable, determineManifestConversion always uses it as ic.manifestConversionPlan.preferredMIMEType.
		// So if we are here, we will definitely be trying to convert the manifest.
		// With ic.cannotModifyManifestReason != "", that would just be a string of repeated failures for the same reason,
		// so let’s bail out early and with a better error message.
		if ic.cannotModifyManifestReason != "" {
			return copySingleImageResult{}, fmt.Errorf("writing manifest failed and we cannot try conversions: %q: %w", cannotModifyManifestReason, err)
		}

		// errs is a list of errors when trying various manifest types. Also serves as an "upload succeeded" flag when set to nil.
		errs := []string{fmt.Sprintf("%s(%v)", ic.manifestConversionPlan.preferredMIMEType, err)}
		for _, manifestMIMEType := range ic.manifestConversionPlan.otherMIMETypeCandidates {
			logrus.Debugf("Trying to use manifest type %s…", manifestMIMEType)
			ic.manifestUpdates.ManifestMIMEType = manifestMIMEType
			attemptedManifest, attemptedManifestDigest, err := ic.copyUpdatedConfigAndManifest(ctx, targetInstance)
			if err != nil {
				logrus.Debugf("Upload of manifest type %s failed: %v", manifestMIMEType, err)
				errs = append(errs, fmt.Sprintf("%s(%v)", manifestMIMEType, err))
				continue
			}

			// We have successfully uploaded a manifest.
			wipResult = copySingleImageResult{
				manifest:         attemptedManifest,
				manifestMIMEType: manifestMIMEType,
				manifestDigest:   attemptedManifestDigest,
			}
			errs = nil // Mark this as a success so that we don't abort below.
			break
		}
		if errs != nil {
			return copySingleImageResult{}, fmt.Errorf("Uploading manifest failed, attempted the following formats: %s", strings.Join(errs, ", "))
		}
	}
	if targetInstance != nil {
		targetInstance = &wipResult.manifestDigest
	}

	newSigs, err := c.createSignatures(ctx, wipResult.manifest, c.options.SignIdentity)
	if err != nil {
		return copySingleImageResult{}, err
	}
	sigs = append(slices.Clone(sigs), newSigs...)

	if len(sigs) > 0 {
		c.Printf("Storing signatures\n")
		if err := c.dest.PutSignaturesWithFormat(ctx, sigs, targetInstance); err != nil {
			return copySingleImageResult{}, fmt.Errorf("writing signatures: %w", err)
		}
	}
	wipResult.compressionAlgorithms = compressionAlgos
	res := wipResult // We are done
	return res, nil
}

// prepareImageConfigForDest enforces dest.MustMatchRuntimeOS and handles dest.NoteOriginalOCIConfig, if necessary.
func prepareImageConfigForDest(ctx context.Context, sys *types.SystemContext, src types.Image, dest private.ImageDestination) error {
	ociConfig, configErr := src.OCIConfig(ctx)
	// Do not fail on configErr here, this might be an artifact
	// and maybe nothing needs this to be a container image and to process the config.

	if dest.MustMatchRuntimeOS() {
		if configErr != nil {
			return fmt.Errorf("parsing image configuration: %w", configErr)
		}
		wantedPlatforms := platform.WantedPlatforms(sys)

		if !slices.ContainsFunc(wantedPlatforms, func(wantedPlatform imgspecv1.Platform) bool {
			// For a transitional period, this might trigger warnings because the Variant
			// field was added to OCI config only recently. If this turns out to be too noisy,
			// revert this check to only look for (OS, Architecture).
			return platform.MatchesPlatform(ociConfig.Platform, wantedPlatform)
		}) {
			options := newOrderedSet()
			for _, p := range wantedPlatforms {
				options.append(fmt.Sprintf("%s+%s+%q", p.OS, p.Architecture, p.Variant))
			}
			logrus.Infof("Image operating system mismatch: image uses OS %q+architecture %q+%q, expecting one of %q",
				ociConfig.OS, ociConfig.Architecture, ociConfig.Variant, strings.Join(options.list, ", "))
		}
	}

	if err := dest.NoteOriginalOCIConfig(ociConfig, configErr); err != nil {
		return err
	}

	return nil
}

// updateEmbeddedDockerReference handles the Docker reference embedded in Docker schema1 manifests.
func (ic *imageCopier) updateEmbeddedDockerReference() error {
	if ic.c.dest.IgnoresEmbeddedDockerReference() {
		return nil // Destination would prefer us not to update the embedded reference.
	}
	destRef := ic.c.dest.Reference().DockerReference()
	if destRef == nil {
		return nil // Destination does not care about Docker references
	}
	if !ic.src.EmbeddedDockerReferenceConflicts(destRef) {
		return nil // No reference embedded in the manifest, or it matches destRef already.
	}

	if ic.cannotModifyManifestReason != "" {
		return fmt.Errorf("Copying a schema1 image with an embedded Docker reference to %s (Docker reference %s) would change the manifest, which we cannot do: %q",
			transports.ImageName(ic.c.dest.Reference()), destRef.String(), ic.cannotModifyManifestReason)
	}
	ic.manifestUpdates.EmbeddedDockerReference = destRef
	return nil
}

func (ic *imageCopier) noPendingManifestUpdates() bool {
	return reflect.DeepEqual(*ic.manifestUpdates, types.ManifestUpdateOptions{InformationOnly: ic.manifestUpdates.InformationOnly})
}

// compareImageDestinationManifestEqual compares the source and destination image manifests (reading the manifest from the
// (possibly remote) destination). If they are equal, it returns a full copySingleImageResult, nil otherwise.
func (ic *imageCopier) compareImageDestinationManifestEqual(ctx context.Context, targetInstance *digest.Digest) (*copySingleImageResult, error) {
	srcManifestDigest, err := manifest.Digest(ic.src.ManifestBlob)
	if err != nil {
		return nil, fmt.Errorf("calculating manifest digest: %w", err)
	}

	destImageSource, err := ic.c.dest.Reference().NewImageSource(ctx, ic.c.options.DestinationCtx)
	if err != nil {
		logrus.Debugf("Unable to create destination image %s source: %v", ic.c.dest.Reference(), err)
		return nil, nil
	}
	defer destImageSource.Close()

	destManifest, destManifestType, err := destImageSource.GetManifest(ctx, targetInstance)
	if err != nil {
		logrus.Debugf("Unable to get destination image %s/%s manifest: %v", destImageSource, targetInstance, err)
		return nil, nil
	}

	destManifestDigest, err := manifest.Digest(destManifest)
	if err != nil {
		return nil, fmt.Errorf("calculating manifest digest: %w", err)
	}

	logrus.Debugf("Comparing source and destination manifest digests: %v vs. %v", srcManifestDigest, destManifestDigest)
	if srcManifestDigest != destManifestDigest {
		return nil, nil
	}

	compressionAlgos := set.New[string]()
	for _, srcInfo := range ic.src.LayerInfos() {
		_, c, err := compressionEditsFromBlobInfo(srcInfo)
		if err != nil {
			return nil, err
		}
		if c != nil {
			compressionAlgos.Add(c.Name())
		}
	}

	algos, err := algorithmsByNames(compressionAlgos.All())
	if err != nil {
		return nil, err
	}

	// Destination and source manifests, types and digests should all be equivalent
	return &copySingleImageResult{
		manifest:              destManifest,
		manifestMIMEType:      destManifestType,
		manifestDigest:        srcManifestDigest,
		compressionAlgorithms: algos,
	}, nil
}

// copyLayers copies layers from ic.src/ic.c.rawSource to dest, using and updating ic.manifestUpdates if necessary and ic.cannotModifyManifestReason == "".
func (ic *imageCopier) copyLayers(ctx context.Context) ([]compressiontypes.Algorithm, error) {
	srcInfos := ic.src.LayerInfos()
	updatedSrcInfos, err := ic.src.LayerInfosForCopy(ctx)
	if err != nil {
		return nil, err
	}
	srcInfosUpdated := false
	if updatedSrcInfos != nil && !reflect.DeepEqual(srcInfos, updatedSrcInfos) {
		if ic.cannotModifyManifestReason != "" {
			return nil, fmt.Errorf("Copying this image would require changing layer representation, which we cannot do: %q", ic.cannotModifyManifestReason)
		}
		srcInfos = updatedSrcInfos
		srcInfosUpdated = true
	}

	type copyLayerData struct {
		destInfo types.BlobInfo
		diffID   digest.Digest
		err      error
	}

	// The manifest is used to extract the information whether a given
	// layer is empty.
	man, err := manifest.FromBlob(ic.src.ManifestBlob, ic.src.ManifestMIMEType)
	if err != nil {
		return nil, err
	}
	manifestLayerInfos := man.LayerInfos()

	// copyGroup is used to determine if all layers are copied
	copyGroup := sync.WaitGroup{}

	data := make([]copyLayerData, len(srcInfos))
	copyLayerHelper := func(index int, srcLayer types.BlobInfo, toEncrypt bool, pool *mpb.Progress, srcRef reference.Named) {
		defer ic.c.concurrentBlobCopiesSemaphore.Release(1)
		defer copyGroup.Done()
		cld := copyLayerData{}
		if !ic.c.options.DownloadForeignLayers && ic.c.dest.AcceptsForeignLayerURLs() && len(srcLayer.URLs) != 0 {
			// DiffIDs are, currently, needed only when converting from schema1.
			// In which case src.LayerInfos will not have URLs because schema1
			// does not support them.
			if ic.diffIDsAreNeeded {
				cld.err = errors.New("getting DiffID for foreign layers is unimplemented")
			} else {
				cld.destInfo = srcLayer
				logrus.Debugf("Skipping foreign layer %q copy to %s", cld.destInfo.Digest, ic.c.dest.Reference().Transport().Name())
			}
		} else {
			cld.destInfo, cld.diffID, cld.err = ic.copyLayer(ctx, srcLayer, toEncrypt, pool, index, srcRef, manifestLayerInfos[index].EmptyLayer)
		}
		data[index] = cld
	}

	// Decide which layers to encrypt
	layersToEncrypt := set.New[int]()
	if ic.c.options.OciEncryptLayers != nil {
		totalLayers := len(srcInfos)
		for _, l := range *ic.c.options.OciEncryptLayers {
			switch {
			case l >= 0 && l < totalLayers:
				layersToEncrypt.Add(l)
			case l < 0 && l+totalLayers >= 0: // Implies (l + totalLayers) < totalLayers
				layersToEncrypt.Add(l + totalLayers) // If l is negative, it is reverse indexed.
			default:
				return nil, fmt.Errorf("when choosing layers to encrypt, layer index %d out of range (%d layers exist)", l, totalLayers)
			}
		}

		if len(*ic.c.options.OciEncryptLayers) == 0 { // “encrypt all layers”
			for i := 0; i < len(srcInfos); i++ {
				layersToEncrypt.Add(i)
			}
		}
	}

	if err := func() error { // A scope for defer
		progressPool := ic.c.newProgressPool()
		defer progressPool.Wait()

		// Ensure we wait for all layers to be copied. progressPool.Wait() must not be called while any of the copyLayerHelpers interact with the progressPool.
		defer copyGroup.Wait()

		for i, srcLayer := range srcInfos {
			if err := ic.c.concurrentBlobCopiesSemaphore.Acquire(ctx, 1); err != nil {
				// This can only fail with ctx.Err(), so no need to blame acquiring the semaphore.
				return fmt.Errorf("copying layer: %w", err)
			}
			copyGroup.Add(1)
			go copyLayerHelper(i, srcLayer, layersToEncrypt.Contains(i), progressPool, ic.c.rawSource.Reference().DockerReference())
		}

		// A call to copyGroup.Wait() is done at this point by the defer above.
		return nil
	}(); err != nil {
		return nil, err
	}

	compressionAlgos := set.New[string]()
	destInfos := make([]types.BlobInfo, len(srcInfos))
	diffIDs := make([]digest.Digest, len(srcInfos))
	for i, cld := range data {
		if cld.err != nil {
			return nil, cld.err
		}
		if cld.destInfo.CompressionAlgorithm != nil {
			compressionAlgos.Add(cld.destInfo.CompressionAlgorithm.Name())
		}
		destInfos[i] = cld.destInfo
		diffIDs[i] = cld.diffID
	}

	// WARNING: If you are adding new reasons to change ic.manifestUpdates, also update the
	// OptimizeDestinationImageAlreadyExists short-circuit conditions
	ic.manifestUpdates.InformationOnly.LayerInfos = destInfos
	if ic.diffIDsAreNeeded {
		ic.manifestUpdates.InformationOnly.LayerDiffIDs = diffIDs
	}
	if srcInfosUpdated || layerDigestsDiffer(srcInfos, destInfos) {
		ic.manifestUpdates.LayerInfos = destInfos
	}
	algos, err := algorithmsByNames(compressionAlgos.All())
	if err != nil {
		return nil, err
	}
	return algos, nil
}

// layerDigestsDiffer returns true iff the digests in a and b differ (ignoring sizes and possible other fields)
func layerDigestsDiffer(a, b []types.BlobInfo) bool {
	return !slices.EqualFunc(a, b, func(a, b types.BlobInfo) bool {
		return a.Digest == b.Digest
	})
}

// copyUpdatedConfigAndManifest updates the image per ic.manifestUpdates, if necessary,
// stores the resulting config and manifest to the destination, and returns the stored manifest
// and its digest.
func (ic *imageCopier) copyUpdatedConfigAndManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, digest.Digest, error) {
	var pendingImage types.Image = ic.src
	if !ic.noPendingManifestUpdates() {
		if ic.cannotModifyManifestReason != "" {
			return nil, "", fmt.Errorf("Internal error: copy needs an updated manifest but that was known to be forbidden: %q", ic.cannotModifyManifestReason)
		}
		if !ic.diffIDsAreNeeded && ic.src.UpdatedImageNeedsLayerDiffIDs(*ic.manifestUpdates) {
			// We have set ic.diffIDsAreNeeded based on the preferred MIME type returned by determineManifestConversion.
			// So, this can only happen if we are trying to upload using one of the other MIME type candidates.
			// Because UpdatedImageNeedsLayerDiffIDs is true only when converting from s1 to s2, this case should only arise
			// when ic.c.dest.SupportedManifestMIMETypes() includes both s1 and s2, the upload using s1 failed, and we are now trying s2.
			// Supposedly s2-only registries do not exist or are extremely rare, so failing with this error message is good enough for now.
			// If handling such registries turns out to be necessary, we could compute ic.diffIDsAreNeeded based on the full list of manifest MIME type candidates.
			return nil, "", fmt.Errorf("Can not convert image to %s, preparing DiffIDs for this case is not supported", ic.manifestUpdates.ManifestMIMEType)
		}
		pi, err := ic.src.UpdatedImage(ctx, *ic.manifestUpdates)
		if err != nil {
			return nil, "", fmt.Errorf("creating an updated image manifest: %w", err)
		}
		pendingImage = pi
	}
	man, _, err := pendingImage.Manifest(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("reading manifest: %w", err)
	}

	if err := ic.copyConfig(ctx, pendingImage); err != nil {
		return nil, "", err
	}

	ic.c.Printf("Writing manifest to image destination\n")
	manifestDigest, err := manifest.Digest(man)
	if err != nil {
		return nil, "", err
	}
	if instanceDigest != nil {
		instanceDigest = &manifestDigest
	}
	if err := ic.c.dest.PutManifest(ctx, man, instanceDigest); err != nil {
		logrus.Debugf("Error %v while writing manifest %q", err, string(man))
		return nil, "", fmt.Errorf("writing manifest: %w", err)
	}
	return man, manifestDigest, nil
}

// copyConfig copies config.json, if any, from src to dest.
func (ic *imageCopier) copyConfig(ctx context.Context, src types.Image) error {
	srcInfo := src.ConfigInfo()
	if srcInfo.Digest != "" {
		if err := ic.c.concurrentBlobCopiesSemaphore.Acquire(ctx, 1); err != nil {
			// This can only fail with ctx.Err(), so no need to blame acquiring the semaphore.
			return fmt.Errorf("copying config: %w", err)
		}
		defer ic.c.concurrentBlobCopiesSemaphore.Release(1)

		destInfo, err := func() (types.BlobInfo, error) { // A scope for defer
			progressPool := ic.c.newProgressPool()
			defer progressPool.Wait()
			bar, err := ic.c.createProgressBar(progressPool, false, srcInfo, "config", "done")
			if err != nil {
				return types.BlobInfo{}, err
			}
			defer bar.Abort(false)
			ic.c.printCopyInfo("config", srcInfo)

			configBlob, err := src.ConfigBlob(ctx)
			if err != nil {
				return types.BlobInfo{}, fmt.Errorf("reading config blob %s: %w", srcInfo.Digest, err)
			}

			destInfo, err := ic.copyBlobFromStream(ctx, bytes.NewReader(configBlob), srcInfo, nil, true, false, bar, -1, false)
			if err != nil {
				return types.BlobInfo{}, err
			}

			bar.mark100PercentComplete()
			return destInfo, nil
		}()
		if err != nil {
			return err
		}
		if destInfo.Digest != srcInfo.Digest {
			return fmt.Errorf("Internal error: copying uncompressed config blob %s changed digest to %s", srcInfo.Digest, destInfo.Digest)
		}
	}
	return nil
}

// diffIDResult contains both a digest value and an error from diffIDComputationGoroutine.
// We could also send the error through the pipeReader, but this more cleanly separates the copying of the layer and the DiffID computation.
type diffIDResult struct {
	digest digest.Digest
	err    error
}

// compressionEditsFromBlobInfo returns a (CompressionOperation, CompressionAlgorithm) value pair suitable
// for types.BlobInfo.
func compressionEditsFromBlobInfo(srcInfo types.BlobInfo) (types.LayerCompression, *compressiontypes.Algorithm, error) {
	// This MIME type → compression mapping belongs in manifest-specific code in our manifest
	// package (but we should preferably replace/change UpdatedImage instead of productizing
	// this workaround).
	switch srcInfo.MediaType {
	case manifest.DockerV2Schema2LayerMediaType, imgspecv1.MediaTypeImageLayerGzip:
		return types.PreserveOriginal, &compression.Gzip, nil
	case imgspecv1.MediaTypeImageLayerZstd:
		tocDigest, err := chunkedToc.GetTOCDigest(srcInfo.Annotations)
		if err != nil {
			return types.PreserveOriginal, nil, err
		}
		if tocDigest != nil {
			return types.PreserveOriginal, &compression.ZstdChunked, nil
		}
		return types.PreserveOriginal, &compression.Zstd, nil
	case manifest.DockerV2SchemaLayerMediaTypeUncompressed, imgspecv1.MediaTypeImageLayer:
		return types.Decompress, nil, nil
	default:
		return types.PreserveOriginal, nil, nil
	}
}

// copyLayer copies a layer with srcInfo (with known Digest and Annotations and possibly known Size) in src to dest, perhaps (de/re/)compressing it,
// and returns a complete blobInfo of the copied layer, and a value for LayerDiffIDs if diffIDIsNeeded
// srcRef can be used as an additional hint to the destination during checking whether a layer can be reused but srcRef can be nil.
func (ic *imageCopier) copyLayer(ctx context.Context, srcInfo types.BlobInfo, toEncrypt bool, pool *mpb.Progress, layerIndex int, srcRef reference.Named, emptyLayer bool) (types.BlobInfo, digest.Digest, error) {
	// If the srcInfo doesn't contain compression information, try to compute it from the
	// MediaType, which was either read from a manifest by way of LayerInfos() or constructed
	// by LayerInfosForCopy(), if it was supplied at all.  If we succeed in copying the blob,
	// the BlobInfo we return will be passed to UpdatedImage() and then to UpdateLayerInfos(),
	// which uses the compression information to compute the updated MediaType values.
	// (Sadly UpdatedImage() is documented to not update MediaTypes from
	//  ManifestUpdateOptions.LayerInfos[].MediaType, so we are doing it indirectly.)
	if srcInfo.CompressionOperation == types.PreserveOriginal && srcInfo.CompressionAlgorithm == nil {
		op, algo, err := compressionEditsFromBlobInfo(srcInfo)
		if err != nil {
			return types.BlobInfo{}, "", err
		}
		srcInfo.CompressionOperation = op
		srcInfo.CompressionAlgorithm = algo
	}

	ic.c.printCopyInfo("blob", srcInfo)

	diffIDIsNeeded := false
	var cachedDiffID digest.Digest = ""
	if ic.diffIDsAreNeeded {
		cachedDiffID = ic.c.blobInfoCache.UncompressedDigest(srcInfo.Digest) // May be ""
		diffIDIsNeeded = cachedDiffID == ""
	}
	// When encrypting to decrypting, only use the simple code path. We might be able to optimize more
	// (e.g. if we know the DiffID of an encrypted compressed layer, it might not be necessary to pull, decrypt and decompress again),
	// but it’s not trivially safe to do such things, so until someone takes the effort to make a comprehensive argument, let’s not.
	encryptingOrDecrypting := toEncrypt || (isOciEncrypted(srcInfo.MediaType) && ic.c.options.OciDecryptConfig != nil)
	canAvoidProcessingCompleteLayer := !diffIDIsNeeded && !encryptingOrDecrypting

	// Don’t read the layer from the source if we already have the blob, and optimizations are acceptable.
	if canAvoidProcessingCompleteLayer {
		canChangeLayerCompression := ic.src.CanChangeLayerCompression(srcInfo.MediaType)
		logrus.Debugf("Checking if we can reuse blob %s: general substitution = %v, compression for MIME type %q = %v",
			srcInfo.Digest, ic.canSubstituteBlobs, srcInfo.MediaType, canChangeLayerCompression)
		canSubstitute := ic.canSubstituteBlobs && canChangeLayerCompression

		var requiredCompression *compressiontypes.Algorithm
		if ic.requireCompressionFormatMatch {
			requiredCompression = ic.compressionFormat
		}

		var tocDigest digest.Digest

		// Check if we have a chunked layer in storage that's based on that blob.  These layers are stored by their TOC digest.
		d, err := chunkedToc.GetTOCDigest(srcInfo.Annotations)
		if err != nil {
			return types.BlobInfo{}, "", err
		}
		if d != nil {
			tocDigest = *d
		}

		reused, reusedBlob, err := ic.c.dest.TryReusingBlobWithOptions(ctx, srcInfo, private.TryReusingBlobOptions{
			Cache:                   ic.c.blobInfoCache,
			CanSubstitute:           canSubstitute,
			EmptyLayer:              emptyLayer,
			LayerIndex:              &layerIndex,
			SrcRef:                  srcRef,
			PossibleManifestFormats: append([]string{ic.manifestConversionPlan.preferredMIMEType}, ic.manifestConversionPlan.otherMIMETypeCandidates...),
			RequiredCompression:     requiredCompression,
			OriginalCompression:     srcInfo.CompressionAlgorithm,
			TOCDigest:               tocDigest,
		})
		if err != nil {
			return types.BlobInfo{}, "", fmt.Errorf("trying to reuse blob %s at destination: %w", srcInfo.Digest, err)
		}
		if reused {
			logrus.Debugf("Skipping blob %s (already present):", srcInfo.Digest)
			if err := func() error { // A scope for defer
				label := "skipped: already exists"
				if reusedBlob.MatchedByTOCDigest {
					label = "skipped: already exists (found by TOC)"
				}
				bar, err := ic.c.createProgressBar(pool, false, types.BlobInfo{Digest: reusedBlob.Digest, Size: 0}, "blob", label)
				if err != nil {
					return err
				}
				defer bar.Abort(false)
				bar.mark100PercentComplete()
				return nil
			}(); err != nil {
				return types.BlobInfo{}, "", err
			}

			// Throw an event that the layer has been skipped
			if ic.c.options.Progress != nil && ic.c.options.ProgressInterval > 0 {
				ic.c.options.Progress <- types.ProgressProperties{
					Event:    types.ProgressEventSkipped,
					Artifact: srcInfo,
				}
			}

			return updatedBlobInfoFromReuse(srcInfo, reusedBlob), cachedDiffID, nil
		}
	}

	// A partial pull is managed by the destination storage, that decides what portions
	// of the source file are not known yet and must be fetched.
	// Attempt a partial only when the source allows to retrieve a blob partially and
	// the destination has support for it.
	if canAvoidProcessingCompleteLayer && ic.c.rawSource.SupportsGetBlobAt() && ic.c.dest.SupportsPutBlobPartial() {
		reused, blobInfo, err := func() (bool, types.BlobInfo, error) { // A scope for defer
			bar, err := ic.c.createProgressBar(pool, true, srcInfo, "blob", "done")
			if err != nil {
				return false, types.BlobInfo{}, err
			}
			hideProgressBar := true
			defer func() { // Note that this is not the same as defer bar.Abort(hideProgressBar); we need hideProgressBar to be evaluated lazily.
				bar.Abort(hideProgressBar)
			}()

			proxy := blobChunkAccessorProxy{
				wrapped: ic.c.rawSource,
				bar:     bar,
			}
			uploadedBlob, err := ic.c.dest.PutBlobPartial(ctx, &proxy, srcInfo, private.PutBlobPartialOptions{
				Cache:      ic.c.blobInfoCache,
				EmptyLayer: emptyLayer,
				LayerIndex: layerIndex,
			})
			if err == nil {
				if srcInfo.Size != -1 {
					refill := srcInfo.Size - bar.Current()
					bar.SetCurrent(srcInfo.Size)
					bar.SetRefill(refill)
				}
				bar.mark100PercentComplete()
				hideProgressBar = false
				logrus.Debugf("Retrieved partial blob %v", srcInfo.Digest)
				return true, updatedBlobInfoFromUpload(srcInfo, uploadedBlob), nil
			}
			// On a "partial content not available" error, ignore it and retrieve the whole layer.
			var perr private.ErrFallbackToOrdinaryLayerDownload
			if errors.As(err, &perr) {
				logrus.Debugf("Failed to retrieve partial blob: %v", err)
				return false, types.BlobInfo{}, nil
			}
			return false, types.BlobInfo{}, err
		}()
		if err != nil {
			return types.BlobInfo{}, "", fmt.Errorf("partial pull of blob %s: %w", srcInfo.Digest, err)
		}
		if reused {
			return blobInfo, cachedDiffID, nil
		}
	}

	// Fallback: copy the layer, computing the diffID if we need to do so
	return func() (types.BlobInfo, digest.Digest, error) { // A scope for defer
		bar, err := ic.c.createProgressBar(pool, false, srcInfo, "blob", "done")
		if err != nil {
			return types.BlobInfo{}, "", err
		}
		defer bar.Abort(false)

		srcStream, srcBlobSize, err := ic.c.rawSource.GetBlob(ctx, srcInfo, ic.c.blobInfoCache)
		if err != nil {
			return types.BlobInfo{}, "", fmt.Errorf("reading blob %s: %w", srcInfo.Digest, err)
		}
		defer srcStream.Close()

		blobInfo, diffIDChan, err := ic.copyLayerFromStream(ctx, srcStream, types.BlobInfo{Digest: srcInfo.Digest, Size: srcBlobSize, MediaType: srcInfo.MediaType, Annotations: srcInfo.Annotations}, diffIDIsNeeded, toEncrypt, bar, layerIndex, emptyLayer)
		if err != nil {
			return types.BlobInfo{}, "", err
		}

		diffID := cachedDiffID
		if diffIDIsNeeded {
			select {
			case <-ctx.Done():
				return types.BlobInfo{}, "", ctx.Err()
			case diffIDResult := <-diffIDChan:
				if diffIDResult.err != nil {
					return types.BlobInfo{}, "", fmt.Errorf("computing layer DiffID: %w", diffIDResult.err)
				}
				logrus.Debugf("Computed DiffID %s for layer %s", diffIDResult.digest, srcInfo.Digest)
				// Don’t record any associations that involve encrypted data. This is a bit crude,
				// some blob substitutions (replacing pulls of encrypted data with local reuse of known decryption outcomes)
				// might be safe, but it’s not trivially obvious, so let’s be conservative for now.
				// This crude approach also means we don’t need to record whether a blob is encrypted
				// in the blob info cache (which would probably be necessary for any more complex logic),
				// and the simplicity is attractive.
				if !encryptingOrDecrypting {
					// This is safe because we have just computed diffIDResult.Digest ourselves, and in the process
					// we have read all of the input blob, so srcInfo.Digest must have been validated by digestingReader.
					ic.c.blobInfoCache.RecordDigestUncompressedPair(srcInfo.Digest, diffIDResult.digest)
				}
				diffID = diffIDResult.digest
			}
		}

		bar.mark100PercentComplete()
		return blobInfo, diffID, nil
	}()
}

// updatedBlobInfoFromReuse returns inputInfo updated with reusedBlob which was created based on inputInfo.
func updatedBlobInfoFromReuse(inputInfo types.BlobInfo, reusedBlob private.ReusedBlob) types.BlobInfo {
	// The transport is only tasked with finding the blob, determining its size if necessary, and returning the right
	// compression format if the blob was substituted.
	// Handling of compression, encryption, and the related MIME types and the like are all the responsibility
	// of the generic code in this package.
	res := types.BlobInfo{
		Digest: reusedBlob.Digest,
		Size:   reusedBlob.Size,
		URLs:   nil, // This _must_ be cleared if Digest changes; clear it in other cases as well, to preserve previous behavior.
		// FIXME: This should remove zstd:chunked annotations IF the original was chunked and the new one isn’t
		// (but those annotations being left with incorrect values should not break pulls).
		Annotations:          maps.Clone(inputInfo.Annotations),
		MediaType:            inputInfo.MediaType, // Mostly irrelevant, MediaType is updated based on Compression*/CryptoOperation.
		CompressionOperation: reusedBlob.CompressionOperation,
		CompressionAlgorithm: reusedBlob.CompressionAlgorithm,
		CryptoOperation:      inputInfo.CryptoOperation, // Expected to be unset anyway.
	}
	// The transport is only expected to fill CompressionOperation and CompressionAlgorithm
	// if the blob was substituted; otherwise, it is optional, and if not set, fill it in based
	// on what we know from the srcInfos we were given.
	if reusedBlob.Digest == inputInfo.Digest {
		if res.CompressionOperation == types.PreserveOriginal {
			res.CompressionOperation = inputInfo.CompressionOperation
		}
		if res.CompressionAlgorithm == nil {
			res.CompressionAlgorithm = inputInfo.CompressionAlgorithm
		}
	}
	if len(reusedBlob.CompressionAnnotations) != 0 {
		if res.Annotations == nil {
			res.Annotations = map[string]string{}
		}
		maps.Copy(res.Annotations, reusedBlob.CompressionAnnotations)
	}
	return res
}

// copyLayerFromStream is an implementation detail of copyLayer; mostly providing a separate “defer” scope.
// it copies a blob with srcInfo (with known Digest and Annotations and possibly known Size) from srcStream to dest,
// perhaps (de/re/)compressing the stream,
// and returns a complete blobInfo of the copied blob and perhaps a <-chan diffIDResult if diffIDIsNeeded, to be read by the caller.
func (ic *imageCopier) copyLayerFromStream(ctx context.Context, srcStream io.Reader, srcInfo types.BlobInfo,
	diffIDIsNeeded bool, toEncrypt bool, bar *progressBar, layerIndex int, emptyLayer bool) (types.BlobInfo, <-chan diffIDResult, error) {
	var getDiffIDRecorder func(compressiontypes.DecompressorFunc) io.Writer // = nil
	var diffIDChan chan diffIDResult

	err := errors.New("Internal error: unexpected panic in copyLayer") // For pipeWriter.CloseWithbelow
	if diffIDIsNeeded {
		diffIDChan = make(chan diffIDResult, 1) // Buffered, so that sending a value after this or our caller has failed and exited does not block.
		pipeReader, pipeWriter := io.Pipe()
		defer func() { // Note that this is not the same as {defer pipeWriter.CloseWithError(err)}; we need err to be evaluated lazily.
			_ = pipeWriter.CloseWithError(err) // CloseWithError(nil) is equivalent to Close(), always returns nil
		}()

		getDiffIDRecorder = func(decompressor compressiontypes.DecompressorFunc) io.Writer {
			// If this fails, e.g. because we have exited and due to pipeWriter.CloseWithError() above further
			// reading from the pipe has failed, we don’t really care.
			// We only read from diffIDChan if the rest of the flow has succeeded, and when we do read from it,
			// the return value includes an error indication, which we do check.
			//
			// If this gets never called, pipeReader will not be used anywhere, but pipeWriter will only be
			// closed above, so we are happy enough with both pipeReader and pipeWriter to just get collected by GC.
			go diffIDComputationGoroutine(diffIDChan, pipeReader, decompressor) // Closes pipeReader
			return pipeWriter
		}
	}

	blobInfo, err := ic.copyBlobFromStream(ctx, srcStream, srcInfo, getDiffIDRecorder, false, toEncrypt, bar, layerIndex, emptyLayer) // Sets err to nil on success
	return blobInfo, diffIDChan, err
	// We need the defer … pipeWriter.CloseWithError() to happen HERE so that the caller can block on reading from diffIDChan
}

// diffIDComputationGoroutine reads all input from layerStream, uncompresses using decompressor if necessary, and sends its digest, and status, if any, to dest.
func diffIDComputationGoroutine(dest chan<- diffIDResult, layerStream io.ReadCloser, decompressor compressiontypes.DecompressorFunc) {
	result := diffIDResult{
		digest: "",
		err:    errors.New("Internal error: unexpected panic in diffIDComputationGoroutine"),
	}
	defer func() { dest <- result }()
	defer layerStream.Close() // We do not care to bother the other end of the pipe with other failures; we send them to dest instead.

	result.digest, result.err = computeDiffID(layerStream, decompressor)
}

// computeDiffID reads all input from layerStream, uncompresses it using decompressor if necessary, and returns its digest.
func computeDiffID(stream io.Reader, decompressor compressiontypes.DecompressorFunc) (digest.Digest, error) {
	if decompressor != nil {
		s, err := decompressor(stream)
		if err != nil {
			return "", err
		}
		defer s.Close()
		stream = s
	}

	return digest.Canonical.FromReader(stream)
}

// algorithmsByNames returns slice of Algorithms from a sequence of Algorithm Names
func algorithmsByNames(names iter.Seq[string]) ([]compressiontypes.Algorithm, error) {
	result := []compressiontypes.Algorithm{}
	for name := range names {
		algo, err := compression.AlgorithmByName(name)
		if err != nil {
			return nil, err
		}
		result = append(result, algo)
	}
	return result, nil
}
