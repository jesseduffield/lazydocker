package supplemented

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	multierror "github.com/hashicorp/go-multierror"
	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	cp "go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

// supplementedImageReference groups multiple references together.
type supplementedImageReference struct {
	types.ImageReference
	references []types.ImageReference
	multiple   cp.ImageListSelection
	instances  []digest.Digest
}

// supplementedImageSource represents an image, plus all of the blobs of other images.
type supplementedImageSource struct {
	types.ImageSource
	reference                 types.ImageReference
	manifest                  []byte                              // The manifest list or image index.
	manifestType              string                              // The MIME type of the manifest list or image index.
	sourceDefaultInstances    map[types.ImageSource]digest.Digest // The default manifest instances of open ImageSource objects.
	sourceInstancesByInstance map[digest.Digest]types.ImageSource // A map from manifest instance digests to open ImageSource objects.
	instancesByBlobDigest     map[digest.Digest]digest.Digest     // A map from blob digests to manifest instance digests.
}

// Reference groups one reference and some number of additional references
// together as a group.  The first reference's default instance will be treated
// as the default instance of the resulting reference, with the other
// references' instances made available as instances for their respective
// digests.
func Reference(ref types.ImageReference, supplemental []types.ImageReference, multiple cp.ImageListSelection, instances []digest.Digest) types.ImageReference {
	if len(instances) > 0 {
		i := make([]digest.Digest, len(instances))
		copy(i, instances)
		instances = i
	}
	return &supplementedImageReference{
		ImageReference: ref,
		references:     append([]types.ImageReference{}, supplemental...),
		multiple:       multiple,
		instances:      instances,
	}
}

// NewImage returns a new higher-level view of the image.
func (s *supplementedImageReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	src, err := s.NewImageSource(ctx, sys)
	if err != nil {
		return nil, fmt.Errorf("building a new Image using an ImageSource: %w", err)
	}
	return image.FromSource(ctx, sys, src)
}

// NewImageSource opens the referenced images, scans their manifests for
// instances, and builds mappings from each blob mentioned in them to their
// instances.
func (s *supplementedImageReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (iss types.ImageSource, err error) {
	sources := make(map[digest.Digest]types.ImageSource)
	defaultInstances := make(map[types.ImageSource]digest.Digest)
	instances := make(map[digest.Digest]digest.Digest)
	var sis *supplementedImageSource

	// Open the default instance for reading.
	top, err := s.ImageReference.NewImageSource(ctx, sys)
	if err != nil {
		return nil, fmt.Errorf("opening %q as image source: %w", transports.ImageName(s.ImageReference), err)
	}

	defer func() {
		if err != nil {
			if iss != nil {
				// The composite source has been created.  Use its Close method.
				if err2 := iss.Close(); err2 != nil {
					logrus.Errorf("Opening image: %v", err2)
				}
			} else if top != nil {
				// The composite source has not been created, but the top was already opened.  Close it.
				if err2 := top.Close(); err2 != nil {
					logrus.Errorf("Closing image: %v", err2)
				}
			}
		}
	}()

	var addSingle, addMulti func(manifestBytes []byte, manifestType string, src types.ImageSource) error
	type manifestToRead struct {
		src      types.ImageSource
		instance *digest.Digest
	}
	manifestsToRead := list.New()

	addSingle = func(manifestBytes []byte, manifestType string, src types.ImageSource) error {
		// Mark this instance as being associated with this ImageSource.
		manifestDigest, err := manifest.Digest(manifestBytes)
		if err != nil {
			return fmt.Errorf("computing digest over manifest %q: %w", string(manifestBytes), err)
		}
		sources[manifestDigest] = src

		// Parse the manifest as a single image.
		man, err := manifest.FromBlob(manifestBytes, manifestType)
		if err != nil {
			return fmt.Errorf("parsing manifest %q: %w", string(manifestBytes), err)
		}

		// Log the config blob's digest and the blobs of its layers as associated with this manifest.
		config := man.ConfigInfo()
		if config.Digest != "" {
			instances[config.Digest] = manifestDigest
			logrus.Debugf("blob %q belongs to %q", config.Digest, manifestDigest)
		}

		layers := man.LayerInfos()
		for _, layer := range layers {
			instances[layer.Digest] = manifestDigest
			logrus.Debugf("layer %q belongs to %q", layer.Digest, manifestDigest)
		}

		return nil
	}

	addMulti = func(manifestBytes []byte, manifestType string, src types.ImageSource) error {
		// Mark this instance as being associated with this ImageSource.
		manifestDigest, err := manifest.Digest(manifestBytes)
		if err != nil {
			return fmt.Errorf("computing manifest digest: %w", err)
		}
		sources[manifestDigest] = src

		// Parse the manifest as a list of images and artifacts.
		list, err := manifest.ListFromBlob(manifestBytes, manifestType)
		if err != nil {
			return fmt.Errorf("parsing manifest blob %q as a %q: %w", string(manifestBytes), manifestType, err)
		}

		// Figure out which of its instances we want to look at.
		var chaseInstances []digest.Digest
		switch s.multiple {
		case cp.CopySystemImage:
			instance, err := list.ChooseInstance(sys)
			if err != nil {
				return fmt.Errorf("selecting appropriate instance from list: %w", err)
			}
			chaseInstances = []digest.Digest{instance}
		case cp.CopySpecificImages:
			for _, instance := range list.Instances() {
				if slices.Contains(s.instances, instance) {
					chaseInstances = append(chaseInstances, instance)
				}
			}
		case cp.CopyAllImages:
			chaseInstances = list.Instances()
		}

		// Queue these manifest instances for reading from this
		// ImageSource later, if we don't stumble across them somewhere
		// else first.
		for _, instanceIterator := range chaseInstances {
			instance := instanceIterator
			next := &manifestToRead{
				src:      src,
				instance: &instance,
			}
			if src == top {
				// Prefer any other source.
				manifestsToRead.PushBack(next)
			} else {
				// Prefer this source over the first ("main") one.
				manifestsToRead.PushFront(next)
			}
		}
		return nil
	}

	visitedReferences := make(map[types.ImageReference]struct{})
	for i, ref := range append([]types.ImageReference{s.ImageReference}, s.references...) {
		if _, visited := visitedReferences[ref]; visited {
			continue
		}
		visitedReferences[ref] = struct{}{}

		// Open this image for reading.
		var src types.ImageSource
		if ref == s.ImageReference {
			src = top
		} else {
			src, err = ref.NewImageSource(ctx, sys)
			if err != nil {
				return nil, fmt.Errorf("opening %q as image source: %w", transports.ImageName(ref), err)
			}
		}

		// Read the default manifest for the image.
		manifestBytes, manifestType, err := image.UnparsedInstance(src, nil).Manifest(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading default manifest from image %q: %w", transports.ImageName(ref), err)
		}

		// If this is the first image, mark it as our starting point.
		if i == 0 {
			sources[""] = src

			sis = &supplementedImageSource{
				ImageSource:               top,
				reference:                 s,
				manifest:                  manifestBytes,
				manifestType:              manifestType,
				sourceDefaultInstances:    defaultInstances,
				sourceInstancesByInstance: sources,
				instancesByBlobDigest:     instances,
			}
			iss = sis
		}

		// Record the digest of the ImageSource's default instance's manifest.
		manifestDigest, err := manifest.Digest(manifestBytes)
		if err != nil {
			return nil, fmt.Errorf("computing digest of manifest from image %q: %w", transports.ImageName(ref), err)
		}
		sis.sourceDefaultInstances[src] = manifestDigest

		// If the ImageSource's default manifest is a list, parse each of its instances.
		if manifest.MIMETypeIsMultiImage(manifestType) {
			if err = addMulti(manifestBytes, manifestType, src); err != nil {
				return nil, fmt.Errorf("adding multi-image %q: %w", transports.ImageName(ref), err)
			}
		} else {
			if err = addSingle(manifestBytes, manifestType, src); err != nil {
				return nil, fmt.Errorf("adding single image %q: %w", transports.ImageName(ref), err)
			}
		}
	}

	// Parse the rest of the instances.
	for manifestsToRead.Front() != nil {
		front := manifestsToRead.Front()
		value := front.Value
		manifestToRead, ok := value.(*manifestToRead)
		if !ok {
			panic("bug: wrong type looking for *manifestToRead in list?")
		}
		manifestsToRead.Remove(front)

		// If we already read this manifest, no need to read it again.
		if _, alreadyRead := sources[*manifestToRead.instance]; alreadyRead {
			continue
		}

		// Read the instance's manifest.
		manifestBytes, manifestType, err := image.UnparsedInstance(manifestToRead.src, manifestToRead.instance).Manifest(ctx)
		if err != nil {
			// if errors.Is(err, storage.ErrImageUnknown) || errors.Is(err, os.ErrNotExist) {
			// Trust that we either don't need it, or that it's in another reference.
			// continue
			// }
			return nil, fmt.Errorf("reading manifest for instance %q: %w", manifestToRead.instance, err)
		}

		if manifest.MIMETypeIsMultiImage(manifestType) {
			// Add the list's contents.
			if err = addMulti(manifestBytes, manifestType, manifestToRead.src); err != nil {
				return nil, fmt.Errorf("adding single image instance %q: %w", manifestToRead.instance, err)
			}
		} else {
			// Add the single image's contents.
			if err = addSingle(manifestBytes, manifestType, manifestToRead.src); err != nil {
				return nil, fmt.Errorf("adding single image instance %q: %w", manifestToRead.instance, err)
			}
		}
	}

	return iss, nil
}

func (s *supplementedImageReference) DeleteImage(_ context.Context, _ *types.SystemContext) error {
	return errors.New("deletion of images not implemented")
}

func (s *supplementedImageSource) Close() error {
	var returnErr *multierror.Error
	closed := make(map[types.ImageSource]struct{})
	for _, sourceInstance := range s.sourceInstancesByInstance {
		if _, closed := closed[sourceInstance]; closed {
			continue
		}
		if err := sourceInstance.Close(); err != nil {
			returnErr = multierror.Append(returnErr, err)
		}
		closed[sourceInstance] = struct{}{}
	}
	if returnErr == nil {
		return nil
	}
	return returnErr.ErrorOrNil()
}

func (s *supplementedImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	requestInstanceDigest := instanceDigest
	if instanceDigest == nil {
		return s.manifest, s.manifestType, nil
	}
	if sourceInstance, ok := s.sourceInstancesByInstance[*instanceDigest]; ok {
		if *instanceDigest == s.sourceDefaultInstances[sourceInstance] {
			requestInstanceDigest = nil
		}
		return sourceInstance.GetManifest(ctx, requestInstanceDigest)
	}
	return nil, "", fmt.Errorf("getting manifest for digest %q: %w", *instanceDigest, ErrDigestNotFound)
}

func (s *supplementedImageSource) GetBlob(ctx context.Context, blob types.BlobInfo, bic types.BlobInfoCache) (io.ReadCloser, int64, error) {
	sourceInstance, ok := s.instancesByBlobDigest[blob.Digest]
	if !ok {
		return nil, -1, fmt.Errorf("blob %q in known instances: %w", blob.Digest, ErrBlobNotFound)
	}
	src, ok := s.sourceInstancesByInstance[sourceInstance]
	if !ok {
		return nil, -1, fmt.Errorf("getting image source for instance %q: %w", sourceInstance, ErrDigestNotFound)
	}
	return src.GetBlob(ctx, blob, bic)
}

func (s *supplementedImageSource) HasThreadSafeGetBlob() bool {
	checked := make(map[types.ImageSource]struct{})
	for _, sourceInstance := range s.sourceInstancesByInstance {
		if _, checked := checked[sourceInstance]; checked {
			continue
		}
		if !sourceInstance.HasThreadSafeGetBlob() {
			return false
		}
		checked[sourceInstance] = struct{}{}
	}
	return true
}

func (s *supplementedImageSource) GetSignatures(ctx context.Context, instanceDigest *digest.Digest) ([][]byte, error) {
	var (
		src    types.ImageSource
		digest digest.Digest
	)
	requestInstanceDigest := instanceDigest
	if instanceDigest == nil {
		if sourceInstance, ok := s.sourceInstancesByInstance[""]; ok {
			src = sourceInstance
		}
	} else {
		digest = *instanceDigest
		if sourceInstance, ok := s.sourceInstancesByInstance[*instanceDigest]; ok {
			src = sourceInstance
		}
		if *instanceDigest == s.sourceDefaultInstances[src] {
			requestInstanceDigest = nil
		}
	}
	if src != nil {
		return src.GetSignatures(ctx, requestInstanceDigest)
	}
	return nil, fmt.Errorf("finding instance for instance digest %q to read signatures: %w", digest, ErrDigestNotFound)
}

func (s *supplementedImageSource) LayerInfosForCopy(ctx context.Context, instanceDigest *digest.Digest) ([]types.BlobInfo, error) {
	var src types.ImageSource
	requestInstanceDigest := instanceDigest
	errMsgDigest := ""
	if instanceDigest == nil {
		if sourceInstance, ok := s.sourceInstancesByInstance[""]; ok {
			src = sourceInstance
		}
	} else {
		errMsgDigest = string(*instanceDigest)
		if sourceInstance, ok := s.sourceInstancesByInstance[*instanceDigest]; ok {
			src = sourceInstance
		}
		if *instanceDigest == s.sourceDefaultInstances[src] {
			requestInstanceDigest = nil
		}
	}
	if src != nil {
		blobInfos, err := src.LayerInfosForCopy(ctx, requestInstanceDigest)
		if err != nil {
			return nil, fmt.Errorf("reading layer infos for copy from instance %q: %w", instanceDigest, err)
		}
		var manifestDigest digest.Digest
		if instanceDigest != nil {
			manifestDigest = *instanceDigest
		}
		for _, blobInfo := range blobInfos {
			s.instancesByBlobDigest[blobInfo.Digest] = manifestDigest
		}
		return blobInfos, nil
	}
	return nil, fmt.Errorf("finding instance for instance digest %q to copy layers: %w", errMsgDigest, ErrDigestNotFound)
}
