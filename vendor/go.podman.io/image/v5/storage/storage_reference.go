//go:build !containers_image_storage_stub

package storage

import (
	"context"
	"fmt"
	"slices"
	"strings"

	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

// A storageReference holds an arbitrary name and/or an ID, which is a 32-byte
// value hex-encoded into a 64-character string, and a reference to a Store
// where an image is, or would be, kept.
// Either "named" or "id" must be set.
type storageReference struct {
	transport storageTransport
	named     reference.Named // may include a tag and/or a digest
	id        string
}

func newReference(transport storageTransport, named reference.Named, id string) (*storageReference, error) {
	if named == nil && id == "" {
		return nil, ErrInvalidReference
	}
	if named != nil && reference.IsNameOnly(named) {
		return nil, fmt.Errorf("reference %s has neither a tag nor a digest: %w", named.String(), ErrInvalidReference)
	}
	if id != "" {
		if err := validateImageID(id); err != nil {
			return nil, fmt.Errorf("invalid ID value %q: %v: %w", id, err.Error(), ErrInvalidReference)
		}
	}
	// We take a copy of the transport, which contains a pointer to the
	// store that it used for resolving this reference, so that the
	// transport that we'll return from Transport() won't be affected by
	// further calls to the original transport's SetStore() method.
	return &storageReference{
		transport: transport,
		named:     named,
		id:        id,
	}, nil
}

// imageMatchesRepo returns true iff image.Names contains an element with the same repo as ref
func imageMatchesRepo(image *storage.Image, ref reference.Named) bool {
	repo := ref.Name()
	return slices.ContainsFunc(image.Names, func(name string) bool {
		if named, err := reference.ParseNormalizedNamed(name); err == nil && named.Name() == repo {
			return true
		}
		return false
	})
}

// multiArchImageMatchesSystemContext returns true if the passed-in image both contains a
// multi-arch manifest that matches the passed-in digest, and the image is the per-platform
// image instance that matches sys.
//
// See the comment in storageReference.ResolveImage explaining why
// this check is necessary.
func multiArchImageMatchesSystemContext(store storage.Store, img *storage.Image, manifestDigest digest.Digest, sys *types.SystemContext) bool {
	// Load the manifest that matches the specified digest.
	// We don't need to care about storage.ImageDigestBigDataKey because
	// manifests lists are only stored into storage by c/image versions
	// that know about manifestBigDataKey, and only using that key.
	key, err := manifestBigDataKey(manifestDigest)
	if err != nil {
		return false // This should never happen, manifestDigest comes from a reference.Digested, and that validates the format.
	}
	manifestBytes, err := store.ImageBigData(img.ID, key)
	if err != nil {
		return false
	}
	// The manifest is either a list, or not a list.  If it's a list, find
	// the digest of the instance that matches the current system, and try
	// to load that manifest from the image record, and use it.
	manifestType := manifest.GuessMIMEType(manifestBytes)
	if !manifest.MIMETypeIsMultiImage(manifestType) {
		// manifestDigest directly specifies a per-platform image, so we aren't
		// choosing among different variants.
		return false
	}
	list, err := manifest.ListFromBlob(manifestBytes, manifestType)
	if err != nil {
		return false
	}
	chosenInstance, err := list.ChooseInstance(sys)
	if err != nil {
		return false
	}
	key, err = manifestBigDataKey(chosenInstance)
	if err != nil {
		return false
	}
	_, err = store.ImageBigData(img.ID, key)
	return err == nil // true if img.ID is based on chosenInstance.
}

// Resolve the reference's name to an image ID in the store, if there's already
// one present with the same name or ID, and return the image.
//
// Returns an error matching ErrNoSuchImage if an image matching ref was not found.
func (s *storageReference) resolveImage(sys *types.SystemContext) (*storage.Image, error) {
	var loadedImage *storage.Image
	if s.id == "" && s.named != nil {
		// Look for an image that has the expanded reference name as an explicit Name value.
		image, err := s.transport.store.Image(s.named.String())
		if image != nil && err == nil {
			loadedImage = image
			s.id = image.ID
		}
	}
	if s.id == "" && s.named != nil {
		if digested, ok := s.named.(reference.Digested); ok {
			// Look for an image with the specified digest that has the same name,
			// though possibly with a different tag or digest, as a Name value, so
			// that the canonical reference can be implicitly resolved to the image.
			//
			// Typically there should be at most one such image, because the same
			// manifest digest implies the same config, and we choose the storage ID
			// based on the config (deduplicating images), except:
			// - the user can explicitly specify an ID when creating the image.
			//   In this case we don't have a preference among the alternatives.
			// - when pulling an image from a multi-platform manifest list, we also
			//   store the manifest list in the image; this allows referencing a
			//   per-platform image using the manifest list digest, but that also
			//   means that we can have multiple genuinely different images in the
			//   storage matching the same manifest list digest (if pulled using different
			//   SystemContext.{OS,Architecture,Variant}Choice to the same storage).
			//   In this case we prefer the image matching the current SystemContext.
			images, err := s.transport.store.ImagesByDigest(digested.Digest())
			if err == nil && len(images) > 0 {
				for _, image := range images {
					if imageMatchesRepo(image, s.named) {
						if loadedImage == nil || multiArchImageMatchesSystemContext(s.transport.store, image, digested.Digest(), sys) {
							loadedImage = image
							s.id = image.ID
						}
					}
				}
			}
		}
	}
	if s.id == "" {
		logrus.Debugf("reference %q does not resolve to an image ID", s.StringWithinTransport())
		// %.0w makes the error visible to error.Unwrap() without including any text.
		// ErrNoSuchImage ultimately is “identifier is not an image”, which is not helpful for identifying the root cause.
		return nil, fmt.Errorf("reference %q does not resolve to an image ID%.0w", s.StringWithinTransport(), ErrNoSuchImage)
	}
	if loadedImage == nil {
		img, err := s.transport.store.Image(s.id)
		if err != nil {
			return nil, fmt.Errorf("reading image %q: %w", s.id, err)
		}
		loadedImage = img
	}
	if s.named != nil {
		if !imageMatchesRepo(loadedImage, s.named) {
			logrus.Errorf("no image matching reference %q found", s.StringWithinTransport())
			return nil, ErrNoSuchImage
		}
	}
	// Default to having the image digest that we hand back match the most recently
	// added manifest...
	if digest, ok := loadedImage.BigDataDigests[storage.ImageDigestBigDataKey]; ok {
		loadedImage.Digest = digest
	}
	// ... unless the named reference says otherwise, and it matches one of the digests
	// in the image.  For those cases, set the Digest field to that value, for the
	// sake of older consumers that don't know there's a whole list in there now.
	if s.named != nil {
		if digested, ok := s.named.(reference.Digested); ok {
			digest := digested.Digest()
			if slices.Contains(loadedImage.Digests, digest) {
				loadedImage.Digest = digest
			}
		}
	}
	return loadedImage, nil
}

// Return a Transport object that defaults to using the same store that we used
// to build this reference object.
func (s storageReference) Transport() types.ImageTransport {
	return &storageTransport{
		store:         s.transport.store,
		defaultUIDMap: s.transport.defaultUIDMap,
		defaultGIDMap: s.transport.defaultGIDMap,
	}
}

// Return a name with a tag or digest, if we have either, else return it bare.
func (s storageReference) DockerReference() reference.Named {
	return s.named
}

// Return a name with a tag, prefixed with the graph root and driver name, to
// disambiguate between images which may be present in multiple stores and
// share only their names.
func (s storageReference) StringWithinTransport() string {
	optionsList := ""
	options := s.transport.store.GraphOptions()
	if len(options) > 0 {
		optionsList = ":" + strings.Join(options, ",")
	}
	res := "[" + s.transport.store.GraphDriverName() + "@" + s.transport.store.GraphRoot() + "+" + s.transport.store.RunRoot() + optionsList + "]"
	if s.named != nil {
		res += s.named.String()
	}
	if s.id != "" {
		res += "@" + s.id
	}
	return res
}

func (s storageReference) PolicyConfigurationIdentity() string {
	res := "[" + s.transport.store.GraphDriverName() + "@" + s.transport.store.GraphRoot() + "]"
	if s.named != nil {
		res += s.named.String()
	}
	if s.id != "" {
		res += "@" + s.id
	}
	return res
}

// Also accept policy that's tied to the combination of the graph root and
// driver name, to apply to all images stored in the Store, and to just the
// graph root, in case we're using multiple drivers in the same directory for
// some reason.
func (s storageReference) PolicyConfigurationNamespaces() []string {
	storeSpec := "[" + s.transport.store.GraphDriverName() + "@" + s.transport.store.GraphRoot() + "]"
	driverlessStoreSpec := "[" + s.transport.store.GraphRoot() + "]"
	namespaces := []string{}
	if s.named != nil {
		if s.id != "" {
			// The reference without the ID is also a valid namespace.
			namespaces = append(namespaces, storeSpec+s.named.String())
		}
		tagged, isTagged := s.named.(reference.Tagged)
		_, isDigested := s.named.(reference.Digested)
		if isTagged && isDigested { // s.named is "name:tag@digest"; add a "name:tag" parent namespace.
			namespaces = append(namespaces, storeSpec+s.named.Name()+":"+tagged.Tag())
		}
		components := strings.Split(s.named.Name(), "/")
		for len(components) > 0 {
			namespaces = append(namespaces, storeSpec+strings.Join(components, "/"))
			components = components[:len(components)-1]
		}
	}
	namespaces = append(namespaces, storeSpec)
	namespaces = append(namespaces, driverlessStoreSpec)
	return namespaces
}

// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned ImageCloser.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
func (s storageReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return newImage(ctx, sys, s)
}

func (s storageReference) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	img, err := s.resolveImage(sys)
	if err != nil {
		return err
	}
	layers, err := s.transport.store.DeleteImage(img.ID, true)
	if err == nil {
		logrus.Debugf("deleted image %q", img.ID)
		for _, layer := range layers {
			logrus.Debugf("deleted layer %q", layer)
		}
	}
	return err
}

func (s storageReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	return newImageSource(sys, s)
}

func (s storageReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return newImageDestination(sys, s)
}

// ResolveReference finds the underlying storage image for a storage.Transport reference.
// It returns that image, and an updated reference which can be used to refer back to the _same_
// image again.
//
// This matters if the input reference contains a tagged name; the destination of the tag can
// move in local storage. The updated reference returned by this function contains the resolved
// image ID, so later uses of that updated reference will either continue to refer to the same
// image, or fail.
//
// Note that it _is_ possible for the later uses to fail, either because the image was removed
// completely, or because the name used in the reference was untaged (even if the underlying image
// ID still exists in local storage).
//
// Returns an error matching ErrNoSuchImage if an image matching ref was not found.
func ResolveReference(ref types.ImageReference) (types.ImageReference, *storage.Image, error) {
	sref, ok := ref.(*storageReference)
	if !ok {
		return nil, nil, fmt.Errorf("trying to resolve a non-%s: reference %q", Transport.Name(),
			transports.ImageName(ref))
	}
	clone := *sref // A shallow copy we can update
	img, err := clone.resolveImage(nil)
	if err != nil {
		return nil, nil, err
	}
	return clone, img, nil
}
