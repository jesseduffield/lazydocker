//go:build !remote

package libimage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	deepcopy "github.com/jinzhu/copier"
	jsoniter "github.com/json-iterator/go"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage/define"
	"go.podman.io/common/libimage/platform"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/shortnames"
	storageTransport "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

// Faster than the standard library, see https://github.com/json-iterator/go.
var json = jsoniter.ConfigCompatibleWithStandardLibrary

// tmpdir returns a path to a temporary directory.
func tmpdir() (string, error) {
	var tmpdir string
	defaultContainerConfig, err := config.Default()
	if err == nil {
		tmpdir, err = defaultContainerConfig.ImageCopyTmpDir()
		if err == nil {
			return tmpdir, nil
		}
	}
	return tmpdir, err
}

// RuntimeOptions allow for creating a customized Runtime.
type RuntimeOptions struct {
	// The base system context of the runtime which will be used throughout
	// the entire lifespan of the Runtime.  Certain options in some
	// functions may override specific fields.
	SystemContext *types.SystemContext
}

// setRegistriesConfPath sets the registries.conf path for the specified context.
func setRegistriesConfPath(systemContext *types.SystemContext) {
	if systemContext.SystemRegistriesConfPath != "" {
		return
	}
	if envOverride, ok := os.LookupEnv("CONTAINERS_REGISTRIES_CONF"); ok {
		systemContext.SystemRegistriesConfPath = envOverride
		return
	}
	if envOverride, ok := os.LookupEnv("REGISTRIES_CONFIG_PATH"); ok {
		systemContext.SystemRegistriesConfPath = envOverride
		return
	}
}

// Runtime is responsible for image management and storing them in a containers
// storage.
type Runtime struct {
	// Use to send events out to users.
	eventChannel chan *Event
	// Underlying storage store.
	store storage.Store
	// Global system context.  No pointer to simplify copying and modifying
	// it.
	systemContext types.SystemContext
}

// SystemContext returns a copy of the runtime's system context.
func (r *Runtime) SystemContext() *types.SystemContext {
	return r.systemContextCopy()
}

// Returns a copy of the runtime's system context.
func (r *Runtime) systemContextCopy() *types.SystemContext {
	var sys types.SystemContext
	_ = deepcopy.Copy(&sys, &r.systemContext)
	return &sys
}

// EventChannel creates a buffered channel for events that the Runtime will use
// to write events to.  Callers are expected to read from the channel in a
// timely manner.
// Can be called once for a given Runtime.
func (r *Runtime) EventChannel() chan *Event {
	if r.eventChannel != nil {
		return r.eventChannel
	}
	r.eventChannel = make(chan *Event, 100)
	return r.eventChannel
}

// RuntimeFromStore returns a Runtime for the specified store.
func RuntimeFromStore(store storage.Store, options *RuntimeOptions) (*Runtime, error) {
	if options == nil {
		options = &RuntimeOptions{}
	}

	var systemContext types.SystemContext
	if options.SystemContext != nil {
		systemContext = *options.SystemContext
	} else {
		systemContext = types.SystemContext{}
	}
	if systemContext.BigFilesTemporaryDir == "" {
		tmpdir, err := tmpdir()
		if err != nil {
			return nil, err
		}
		systemContext.BigFilesTemporaryDir = tmpdir
	}

	setRegistriesConfPath(&systemContext)

	return &Runtime{
		store:         store,
		systemContext: systemContext,
	}, nil
}

// RuntimeFromStoreOptions returns a return for the specified store options.
func RuntimeFromStoreOptions(runtimeOptions *RuntimeOptions, storeOptions *storage.StoreOptions) (*Runtime, error) {
	if storeOptions == nil {
		storeOptions = &storage.StoreOptions{}
	}
	store, err := storage.GetStore(*storeOptions)
	if err != nil {
		return nil, err
	}
	storageTransport.Transport.SetStore(store)
	return RuntimeFromStore(store, runtimeOptions)
}

// Shutdown attempts to free any kernel resources which are being used by the
// underlying driver.  If "force" is true, any mounted (i.e., in use) layers
// are unmounted beforehand.  If "force" is not true, then layers being in use
// is considered to be an error condition.
func (r *Runtime) Shutdown(force bool) error {
	_, err := r.store.Shutdown(force)
	if r.eventChannel != nil {
		close(r.eventChannel)
	}
	return err
}

// storageToImage transforms a storage.Image to an Image.
func (r *Runtime) storageToImage(storageImage *storage.Image, ref types.ImageReference) *Image {
	return &Image{
		runtime:          r,
		storageImage:     storageImage,
		storageReference: ref,
	}
}

// getImagesAndLayers obtains consistent slices of Image and storage.Layer.
func (r *Runtime) getImagesAndLayers() ([]*Image, []storage.Layer, error) {
	snapshot, err := r.store.MultiList(
		storage.MultiListOptions{
			Images: true,
			Layers: true,
		})
	if err != nil {
		return nil, nil, err
	}
	images := []*Image{}
	for i := range snapshot.Images {
		images = append(images, r.storageToImage(&snapshot.Images[i], nil))
	}
	return images, snapshot.Layers, nil
}

// Exists returns true if the specified image exists in the local containers
// storage.  Note that it may return false if an image corrupted.
func (r *Runtime) Exists(name string) (bool, error) {
	image, _, err := r.LookupImage(name, nil)
	if err != nil && !errors.Is(err, storage.ErrImageUnknown) {
		return false, err
	}
	if image == nil {
		return false, nil
	}
	if err := image.isCorrupted(context.Background(), name); err != nil {
		logrus.Error(err)
		return false, nil
	}
	return true, nil
}

// LookupImageOptions allow for customizing local image lookups.
type LookupImageOptions struct {
	// Lookup an image matching the specified architecture.
	Architecture string
	// Lookup an image matching the specified OS.
	OS string
	// Lookup an image matching the specified variant.
	Variant string

	// Controls the behavior when checking the platform of an image.
	PlatformPolicy define.PlatformPolicy

	// If set, do not look for items/instances in the manifest list that
	// match the current platform but return the manifest list as is.
	// only check for manifest list, return ErrNotAManifestList if not found.
	lookupManifest bool

	// If matching images resolves to a manifest list, return manifest list
	// instead of resolving to image instance, if manifest list is not found
	// try resolving image.
	ManifestList bool

	// If the image resolves to a manifest list, we usually lookup a
	// matching instance and error if none could be found.  In this case,
	// just return the manifest list.  Required for image removal.
	returnManifestIfNoInstance bool
}

var errNoHexValue = errors.New("invalid format: no 64-byte hexadecimal value")

// LookupImage looks up `name` in the local container storage.  Returns the
// image and the name it has been found with.  Note that name may also use the
// `containers-storage:` prefix used to refer to the containers-storage
// transport.  Returns storage.ErrImageUnknown if the image could not be found.
//
// Unless specified via the options, the image will be looked up by name only
// without matching the architecture, os or variant.  An exception is if the
// image resolves to a manifest list, where an instance of the manifest list
// matching the local or specified platform (via options.{Architecture,OS,Variant})
// is returned.
//
// If the specified name uses the `containers-storage` transport, the resolved
// name is empty.
func (r *Runtime) LookupImage(name string, options *LookupImageOptions) (*Image, string, error) {
	logrus.Debugf("Looking up image %q in local containers storage", name)

	if options == nil {
		options = &LookupImageOptions{}
	}

	// If needed extract the name sans transport.
	storageRef, err := alltransports.ParseImageName(name)
	if err == nil {
		if storageRef.Transport().Name() != storageTransport.Transport.Name() {
			return nil, "", fmt.Errorf("unsupported transport %q for looking up local images", storageRef.Transport().Name())
		}
		_, img, err := storageTransport.ResolveReference(storageRef)
		if err != nil {
			if errors.Is(err, storageTransport.ErrNoSuchImage) {
				// backward compatibility
				return nil, "", storage.ErrImageUnknown
			}
			return nil, "", err
		}
		logrus.Debugf("Found image %q in local containers storage (%s)", name, storageRef.StringWithinTransport())
		return r.storageToImage(img, storageRef), "", nil
	}
	// Docker compat: strip off the tag iff name is tagged and digested
	// (e.g., fedora:latest@sha256...).  In that case, the tag is stripped
	// off and entirely ignored.  The digest is the sole source of truth.
	normalizedName, possiblyUnqualifiedNamedReference, err := normalizeTaggedDigestedString(name)
	if err != nil {
		return nil, "", fmt.Errorf(`parsing reference %q: %w`, name, err)
	}
	name = normalizedName

	byDigest := false
	originalName := name
	if strings.HasPrefix(name, "sha256:") {
		byDigest = true
		name = strings.TrimPrefix(name, "sha256:")
	}
	byFullID := reference.IsFullIdentifier(name)

	if byDigest && !byFullID {
		return nil, "", fmt.Errorf("%s: %v", originalName, errNoHexValue)
	}

	// If the name clearly refers to a local image, try to look it up.
	if byFullID || byDigest {
		img, err := r.lookupImageInLocalStorage(originalName, name, nil, options)
		if err != nil {
			return nil, "", err
		}
		if img != nil {
			return img, originalName, nil
		}
		return nil, "", fmt.Errorf("%s: %w", originalName, storage.ErrImageUnknown)
	}

	// Unless specified, set the platform specified in the system context
	// for later platform matching.  Builder likes to set these things via
	// the system context at runtime creation.
	if options.Architecture == "" {
		options.Architecture = r.systemContext.ArchitectureChoice
	}
	if options.OS == "" {
		options.OS = r.systemContext.OSChoice
	}
	if options.Variant == "" {
		options.Variant = r.systemContext.VariantChoice
	}
	// Normalize platform to be OCI compatible (e.g., "aarch64" -> "arm64").
	options.OS, options.Architecture, options.Variant = platform.Normalize(options.OS, options.Architecture, options.Variant)

	// Second, try out the candidates as resolved by shortnames. This takes
	// "localhost/" prefixed images into account as well.
	candidates, err := shortnames.ResolveLocally(&r.systemContext, name)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", name, storage.ErrImageUnknown)
	}
	// Backwards compat: normalize to docker.io as some users may very well
	// rely on that.
	if dockerNamed, err := reference.ParseDockerRef(name); err == nil {
		candidates = append(candidates, dockerNamed)
	}

	for _, candidate := range candidates {
		img, err := r.lookupImageInLocalStorage(name, candidate.String(), candidate, options)
		if err != nil {
			return nil, "", err
		}
		if img != nil {
			return img, candidate.String(), err
		}
	}

	// The specified name may refer to a short ID. Note that this *must*
	// happen after the short-name expansion as done above.
	img, err := r.lookupImageInLocalStorage(name, name, nil, options)
	if err != nil {
		return nil, "", err
	}
	if img != nil {
		return img, name, err
	}

	return r.lookupImageInDigestsAndRepoTags(name, possiblyUnqualifiedNamedReference, options)
}

// lookupImageInLocalStorage looks up the specified candidate for name in the
// storage and checks whether it's matching the system context.
func (r *Runtime) lookupImageInLocalStorage(name, candidate string, namedCandidate reference.Named, options *LookupImageOptions) (*Image, error) {
	logrus.Debugf("Trying %q ...", candidate)

	var err error
	var img *storage.Image
	var ref types.ImageReference

	// FIXME: the lookup logic for manifest lists needs improvement.
	// See https://github.com/containers/common/pull/1505#discussion_r1242677279
	// for details.

	// For images pulled by tag, Image.Names does not currently contain a
	// repo@digest value, so such an input would not match directly in
	// c/storage.
	if namedCandidate != nil {
		namedCandidate = reference.TagNameOnly(namedCandidate)
		ref, err = storageTransport.Transport.NewStoreReference(r.store, namedCandidate, "")
		if err != nil {
			return nil, err
		}
		_, img, err = storageTransport.ResolveReference(ref)
		if err != nil {
			if errors.Is(err, storageTransport.ErrNoSuchImage) {
				return nil, nil
			}
			return nil, err
		}
		// NOTE: we must reparse the reference another time below since
		// an ordinary image may have resolved into a per-platform image
		// without any regard to options.{Architecture,OS,Variant}.
	} else {
		img, err = r.store.Image(candidate)
		if err != nil {
			if errors.Is(err, storage.ErrImageUnknown) {
				return nil, nil
			}
			return nil, err
		}
	}
	ref, err = storageTransport.Transport.ParseStoreReference(r.store, img.ID)
	if err != nil {
		return nil, err
	}

	image := r.storageToImage(img, ref)
	logrus.Debugf("Found image %q as %q in local containers storage", name, candidate)

	// If we referenced a manifest list, we need to check whether we can
	// find a matching instance in the local containers storage.
	isManifestList, err := image.IsManifestList(context.Background())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// We must be tolerant toward corrupted images.
			// See containers/podman commit fd9dd7065d44.
			logrus.Warnf("Failed to determine if an image is a manifest list: %v, ignoring the error", err)
			return image, nil
		}
		return nil, err
	}
	if options.lookupManifest || options.ManifestList {
		if isManifestList {
			return image, nil
		}
		// return ErrNotAManifestList if lookupManifest is set otherwise try resolving image.
		if options.lookupManifest {
			return nil, fmt.Errorf("%s: %w", candidate, ErrNotAManifestList)
		}
	}

	if isManifestList {
		logrus.Debugf("Candidate %q is a manifest list, looking up matching instance", candidate)
		manifestList, err := image.ToManifestList()
		if err != nil {
			return nil, err
		}
		instance, err := manifestList.LookupInstance(context.Background(), options.Architecture, options.OS, options.Variant)
		if err != nil {
			if options.returnManifestIfNoInstance {
				logrus.Debug("No matching instance was found: returning manifest list instead")
				return image, nil
			}
			return nil, fmt.Errorf("%v: %w", err, storage.ErrImageUnknown)
		}
		ref, err = storageTransport.Transport.ParseStoreReference(r.store, "@"+instance.ID())
		if err != nil {
			return nil, err
		}
		image = instance
	}

	// Also print the string within the storage transport.  That may aid in
	// debugging when using additional stores since we see explicitly where
	// the store is and which driver (options) are used.
	logrus.Debugf("Found image %q as %q in local containers storage (%s)", name, candidate, ref.StringWithinTransport())

	// Do not perform any further platform checks if the image was
	// requested by ID.  In that case, we must assume that the user/tool
	// know what they're doing.
	if strings.HasPrefix(image.ID(), candidate) {
		return image, nil
	}

	// Ignore the (fatal) error since the image may be corrupted, which
	// will bubble up at other places.  During lookup, we just return it as
	// is.
	if matchError, customPlatform, _ := image.matchesPlatform(context.Background(), options.OS, options.Architecture, options.Variant); matchError != nil {
		if customPlatform {
			logrus.Debugf("%v", matchError)
			// Return nil if the user clearly requested a custom
			// platform and the located image does not match.
			return nil, nil
		}
		switch options.PlatformPolicy {
		case define.PlatformPolicyDefault:
			logrus.Debugf("%v", matchError)
		case define.PlatformPolicyWarn:
			logrus.Warnf("%v", matchError)
		}
	}

	return image, nil
}

// lookupImageInDigestsAndRepoTags attempts to match name against any image in
// the local containers storage.  If name is digested, it will be compared
// against image digests.  Otherwise, it will be looked up in the repo tags.
func (r *Runtime) lookupImageInDigestsAndRepoTags(name string, possiblyUnqualifiedNamedReference reference.Named, options *LookupImageOptions) (*Image, string, error) {
	originalName := name // we may change name below

	if possiblyUnqualifiedNamedReference == nil {
		return nil, "", fmt.Errorf("%s: %w", originalName, storage.ErrImageUnknown)
	}
	if !shortnames.IsShortName(name) {
		return nil, "", fmt.Errorf("%s: %w", originalName, storage.ErrImageUnknown)
	}

	var requiredDigest digest.Digest // or ""
	var requiredTag string           // or ""

	possiblyUnqualifiedNamedReference = reference.TagNameOnly(possiblyUnqualifiedNamedReference) // Docker compat: make sure to add the "latest" tag if needed.
	if digested, ok := possiblyUnqualifiedNamedReference.(reference.Digested); ok {
		requiredDigest = digested.Digest()
		name = reference.TrimNamed(possiblyUnqualifiedNamedReference).String()
	} else if namedTagged, ok := possiblyUnqualifiedNamedReference.(reference.NamedTagged); ok {
		requiredTag = namedTagged.Tag()
	} else { // This should never happen after the reference.TagNameOnly above.
		return nil, "", fmt.Errorf("%s: %w (could not cast to tagged)", originalName, storage.ErrImageUnknown)
	}

	allImages, err := r.ListImages(context.Background(), nil)
	if err != nil {
		return nil, "", err
	}

	for _, image := range allImages {
		named, err := image.referenceFuzzilyMatchingRepoAndTag(possiblyUnqualifiedNamedReference, requiredTag)
		if err != nil {
			return nil, "", err
		}
		if named == nil {
			continue
		}
		img, err := r.lookupImageInLocalStorage(name, named.String(), named, options)
		if err != nil {
			return nil, "", err
		}
		if img != nil {
			if requiredDigest != "" {
				if !img.hasDigest(requiredDigest) {
					continue
				}
				named = reference.TrimNamed(named)
				canonical, err := reference.WithDigest(named, requiredDigest)
				if err != nil {
					return nil, "", fmt.Errorf("building canonical reference with digest %q and matched %q: %w", requiredDigest.String(), named.String(), err)
				}
				return img, canonical.String(), nil
			}
			return img, named.String(), nil
		}
	}

	return nil, "", fmt.Errorf("%s: %w", originalName, storage.ErrImageUnknown)
}

// ResolveName resolves the specified name.  If the name resolves to a local
// image, the fully resolved name will be returned.  Otherwise, the name will
// be properly normalized.
//
// Note that an empty string is returned as is.
func (r *Runtime) ResolveName(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	image, resolvedName, err := r.LookupImage(name, nil)
	if err != nil && !errors.Is(err, storage.ErrImageUnknown) {
		return "", err
	}

	if image != nil && !strings.HasPrefix(image.ID(), resolvedName) {
		return resolvedName, err
	}

	normalized, err := NormalizeName(name)
	if err != nil {
		return "", err
	}

	return normalized.String(), nil
}

// IsExternalContainerFunc allows for checking whether the specified container
// is an external one.  The definition of an external container can be set by
// callers.
type IsExternalContainerFunc func(containerID string) (bool, error)

// ListImagesOptions allow for customizing listing images.
type ListImagesOptions struct {
	// Filters to filter the listed images.  Supported filters are
	// * after,before,since=image
	// * containers=true,false,external
	// * dangling=true,false
	// * intermediate=true,false (useful for pruning images)
	// * id=id
	// * label=key[=value]
	// * readonly=true,false
	// * reference=name[:tag] (wildcards allowed)
	Filters []string
	// IsExternalContainerFunc allows for checking whether the specified
	// container is an external one (when containers=external filter is
	// used).  The definition of an external container can be set by
	// callers.
	IsExternalContainerFunc IsExternalContainerFunc
	// SetListData will populate the Image.ListData fields of returned images.
	SetListData bool
}

// ListImagesByNames lists the images in the local container storage by specified names
// The name lookups use the LookupImage semantics.
func (r *Runtime) ListImagesByNames(names []string) ([]*Image, error) {
	images := []*Image{}
	for _, name := range names {
		image, _, err := r.LookupImage(name, nil)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, nil
}

// ListImages lists the images in the local container storage and filter the images by ListImagesOptions
//
// podman images consumes the output of ListImages and produces one line for each tag in each Image.Names value,
// rather than one line for each Image with all Names, so if options.Filters contains one reference filter
// with a fully qualified image name without negation, it is considered a query so it makes more sense for
// the user to see only the corresponding names in the output, not all the names of the deduplicated
// image; therefore, we make the corresponding names available to the caller by overwriting the actual image names
// with the corresponding names when the reference filter matches and the reference is a fully qualified image name
// (i.e., contains a tag or digest, not just a bare repository name).
//
// This overwriting is done only in memory and is not written to storage in any way.
func (r *Runtime) ListImages(ctx context.Context, options *ListImagesOptions) ([]*Image, error) {
	if options == nil {
		options = &ListImagesOptions{}
	}

	filters, needsLayerTree, err := r.compileImageFilters(ctx, options)
	if err != nil {
		return nil, err
	}

	if options.SetListData {
		needsLayerTree = true
	}

	snapshot, err := r.store.MultiList(
		storage.MultiListOptions{
			Images: true,
			Layers: needsLayerTree,
		})
	if err != nil {
		return nil, err
	}
	images := []*Image{}
	for i := range snapshot.Images {
		images = append(images, r.storageToImage(&snapshot.Images[i], nil))
	}

	// If explicitly requested by the user, pre-compute and cache the
	// dangling and parent information of all filtered images.  That will
	// considerably speed things up for callers who need this information
	// as the layer tree will computed once for all instead of once for
	// each individual image (see containers/podman/issues/17828).

	var tree *layerTree
	if needsLayerTree {
		tree, err = r.newLayerTreeFromData(images, snapshot.Layers, true)
		if err != nil {
			return nil, err
		}
	}

	filtered, err := r.filterImages(ctx, images, filters, tree)
	if err != nil {
		return nil, err
	}

	if !options.SetListData {
		return filtered, nil
	}

	for i := range filtered {
		isDangling, err := filtered[i].isDangling(ctx, tree)
		if err != nil {
			return nil, err
		}
		filtered[i].ListData.IsDangling = &isDangling

		parent, err := filtered[i].parent(ctx, tree)
		if err != nil {
			return nil, err
		}
		filtered[i].ListData.Parent = parent
	}

	return filtered, nil
}

// RemoveImagesOptions allow for customizing image removal.
type RemoveImagesOptions struct {
	// Force will remove all containers from the local storage that are
	// using a removed image.  Use RemoveContainerFunc for a custom logic.
	// If set, all child images will be removed as well.
	Force bool
	// LookupManifest will expect all specified names to be manifest lists (no instance look up).
	// This allows for removing manifest lists.
	// By default, RemoveImages will attempt to resolve to a manifest instance matching
	// the local platform (i.e., os, architecture, variant).
	LookupManifest bool
	// RemoveContainerFunc allows for a custom logic for removing
	// containers using a specific image.  By default, all containers in
	// the local containers storage will be removed (if Force is set).
	RemoveContainerFunc RemoveContainerFunc
	// Ignore if a specified image does not exist and do not throw an error.
	Ignore bool
	// IsExternalContainerFunc allows for checking whether the specified
	// container is an external one (when containers=external filter is
	// used).  The definition of an external container can be set by
	// callers.
	IsExternalContainerFunc IsExternalContainerFunc
	// Remove external containers even when Force is false.  Requires
	// IsExternalContainerFunc to be specified.
	ExternalContainers bool
	// Filters to filter the removed images.  Supported filters are
	// * after,before,since=image
	// * containers=true,false,external
	// * dangling=true,false
	// * intermediate=true,false (useful for pruning images)
	// * id=id
	// * label=key[=value]
	// * readonly=true,false
	// * reference=name[:tag] (wildcards allowed)
	Filters []string
	// The RemoveImagesReport will include the size of the removed image.
	// This information may be useful when pruning images to figure out how
	// much space was freed. However, computing the size of an image is
	// comparatively expensive, so it is made optional.
	WithSize bool
	// NoPrune will not remove dangling images
	NoPrune bool
}

// RemoveImages removes images specified by names.  If no names are specified,
// remove images as specified via the options' filters.  All images are
// expected to exist in the local containers storage.
//
// If an image has more names than one name, the image will be untagged with
// the specified name.  RemoveImages returns a slice of untagged and removed
// images.
//
// Note that most errors are non-fatal and collected into `rmErrors` return
// value.
func (r *Runtime) RemoveImages(ctx context.Context, names []string, options *RemoveImagesOptions) (reports []*RemoveImageReport, rmErrors []error) {
	if options == nil {
		options = &RemoveImagesOptions{}
	}

	if options.ExternalContainers && options.IsExternalContainerFunc == nil {
		return nil, []error{errors.New("libimage error: cannot remove external containers without callback")}
	}

	// The logic here may require some explanation.  Image removal is
	// surprisingly complex since it is recursive (intermediate parents are
	// removed) and since multiple items in `names` may resolve to the
	// *same* image.  On top, the data in the containers storage is shared,
	// so we need to be careful and the code must be robust.  That is why
	// users can only remove images via this function; the logic may be
	// complex but the execution path is clear.

	// Bundle an image with a possible empty slice of names to untag.  That
	// allows for a decent untagging logic and to bundle multiple
	// references to the same *Image (and circumvent consistency issues).
	type deleteMe struct {
		image        *Image
		referencedBy []string
	}

	appendError := func(err error) {
		rmErrors = append(rmErrors, err)
	}

	deleteMap := make(map[string]*deleteMe) // ID -> deleteMe
	toDelete := []string{}
	// Look up images in the local containers storage and fill out
	// toDelete and the deleteMap.
	switch {
	case len(names) > 0:
		// prepare lookupOptions
		var lookupOptions *LookupImageOptions
		if options.LookupManifest {
			// LookupManifest configured as true make sure we only remove manifests and no referenced images.
			lookupOptions = &LookupImageOptions{lookupManifest: true}
		} else {
			lookupOptions = &LookupImageOptions{returnManifestIfNoInstance: true}
		}
		// Look up the images one-by-one.  That allows for removing
		// images that have been looked up successfully while reporting
		// lookup errors at the end.
		for _, name := range names {
			img, resolvedName, err := r.LookupImage(name, lookupOptions)
			if err != nil {
				if options.Ignore && errors.Is(err, storage.ErrImageUnknown) {
					continue
				}
				appendError(err)
				continue
			}
			dm, exists := deleteMap[img.ID()]
			if !exists {
				toDelete = append(toDelete, img.ID())
				dm = &deleteMe{image: img}
				deleteMap[img.ID()] = dm
			}
			dm.referencedBy = append(dm.referencedBy, resolvedName)
		}

	default:
		options := &ListImagesOptions{
			IsExternalContainerFunc: options.IsExternalContainerFunc,
			Filters:                 options.Filters,
		}
		filteredImages, err := r.ListImages(ctx, options)
		if err != nil {
			appendError(err)
			return nil, rmErrors
		}
		for _, img := range filteredImages {
			toDelete = append(toDelete, img.ID())
			deleteMap[img.ID()] = &deleteMe{image: img}
		}
	}

	// Return early if there's no image to delete.
	if len(deleteMap) == 0 {
		return nil, rmErrors
	}

	// Now remove the images in the given order.
	rmMap := make(map[string]*RemoveImageReport)
	orderedIDs := []string{}
	visitedIDs := make(map[string]bool)
	for _, id := range toDelete {
		del, exists := deleteMap[id]
		if !exists {
			appendError(fmt.Errorf("internal error: ID %s not in found in image-deletion map", id))
			continue
		}
		if len(del.referencedBy) == 0 {
			del.referencedBy = []string{""}
		}
		for _, ref := range del.referencedBy {
			processedIDs, err := del.image.remove(ctx, rmMap, ref, options)
			if err != nil {
				appendError(err)
			}
			// NOTE: make sure to add given ID only once to orderedIDs.
			for _, id := range processedIDs {
				if visited := visitedIDs[id]; visited {
					continue
				}
				orderedIDs = append(orderedIDs, id)
				visitedIDs[id] = true
			}
		}
	}

	// Finally, we can assemble the reports slice.
	for _, id := range orderedIDs {
		report, exists := rmMap[id]
		if exists {
			reports = append(reports, report)
		}
	}

	return reports, rmErrors
}
