//go:build !remote

package libimage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/go-digest"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libimage/platform"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	storageTransport "go.podman.io/image/v5/storage"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
)

// Image represents an image in the containers storage and allows for further
// operations and data manipulation.
type Image struct {
	// ListData that is being set by (*Runtime).ListImages().  Note that
	// the data may be outdated.
	ListData struct {
		// Dangling indicates if the image is dangling.  Use
		// `IsDangling()` to compute the latest state.
		IsDangling *bool
		// Parent points to the parent image.  Use `Parent()` to
		// compute the latest state.
		Parent *Image
	}

	// Backwards pointer to the runtime.
	runtime *Runtime

	// Counterpart in the local containers storage.
	storageImage *storage.Image

	// Image reference to the containers storage.
	storageReference types.ImageReference

	// All fields in the below structure are cached.  They may be cleared
	// at any time.  When adding a new field, please make sure to clear
	// it in `(*Image).reload()`.
	cached struct {
		// Image source.  Cached for performance reasons.
		imageSource types.ImageSource
		// Inspect data we get from containers/image.
		partialInspectData *types.ImageInspectInfo
		// Fully assembled image data.
		completeInspectData *ImageData
		// Corresponding OCI image.
		ociv1Image *ociv1.Image
		// Names() parsed into references.
		namesReferences []reference.Reference
		// Calculating the Size() is expensive, so cache it.
		size *int64
	}
}

// reload the image and pessimistically clear all cached data.
func (i *Image) reload() error {
	logrus.Tracef("Reloading image %s", i.ID())
	img, err := i.runtime.store.Image(i.ID())
	if err != nil {
		return fmt.Errorf("reloading image: %w", err)
	}
	i.storageImage = img
	i.cached.imageSource = nil
	i.cached.partialInspectData = nil
	i.cached.completeInspectData = nil
	i.cached.ociv1Image = nil
	i.cached.namesReferences = nil
	i.cached.size = nil
	return nil
}

// isCorrupted returns an error if the image may be corrupted.
func (i *Image) isCorrupted(ctx context.Context, name string) error {
	// If it's a manifest list, we're good for now.
	if _, err := i.getManifestList(); err == nil {
		return nil
	}

	ref, err := i.StorageReference()
	if err != nil {
		return err
	}

	img, err := ref.NewImage(ctx, nil)
	if err != nil {
		if name == "" {
			name = i.ID()[:12]
		}
		return fmt.Errorf("Image %s exists in local storage but may be corrupted (remove the image to resolve the issue): %v", name, err)
	}
	return img.Close()
}

// Names returns associated names with the image which may be a mix of tags and
// digests.
func (i *Image) Names() []string {
	return i.storageImage.Names
}

// setEphemeralNames sets the names of the image.
//
// WARNING: this only affects the in-memory values, they are not written into the backing storage.
func (i *Image) setEphemeralNames(names []string) {
	i.storageImage.Names = names
}

// NamesReferences returns Names() as references.
func (i *Image) NamesReferences() ([]reference.Reference, error) {
	if i.cached.namesReferences != nil {
		return i.cached.namesReferences, nil
	}
	refs := make([]reference.Reference, 0, len(i.Names()))
	for _, name := range i.Names() {
		ref, err := reference.Parse(name)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	i.cached.namesReferences = refs
	return refs, nil
}

// StorageImage returns the underlying storage.Image.
func (i *Image) StorageImage() *storage.Image {
	return i.storageImage
}

// NamesHistory returns a string array of names previously associated with the
// image, which may be a mixture of tags and digests.
func (i *Image) NamesHistory() []string {
	return i.storageImage.NamesHistory
}

// ID returns the ID of the image.
func (i *Image) ID() string {
	return i.storageImage.ID
}

// Digest is a digest value that we can use to locate the image, if one was
// specified at creation-time.  Typically it is the digest of one among
// possibly many digests that we have stored for the image, so many
// applications are better off using the entire list returned by Digests().
func (i *Image) Digest() digest.Digest {
	// TODO: we return the image digest or the one of the manifest list
	// which can lead to issues depending on the callers' assumptions.
	// Hence, deprecate in favor of Digest_s_.
	return i.storageImage.Digest
}

// Digests is a list of digest values of the image's manifests, and possibly a
// manually-specified value, that we can use to locate the image.  If Digest is
// set, its value is also in this list.
func (i *Image) Digests() []digest.Digest {
	return i.storageImage.Digests
}

// hasDigest returns whether the specified value matches any digest of the
// image.
func (i *Image) hasDigest(wantedDigest digest.Digest) bool {
	return slices.Contains(i.Digests(), wantedDigest)
}

// containsDigestPrefix returns whether the specified value matches any digest of the
// image. It checks for the prefix and not a full match.
func (i *Image) containsDigestPrefix(wantedDigestPrefix string) bool {
	for _, d := range i.Digests() {
		if strings.HasPrefix(d.String(), wantedDigestPrefix) {
			return true
		}
	}
	return false
}

// IsReadOnly returns whether the image is set read only.
func (i *Image) IsReadOnly() bool {
	return i.storageImage.ReadOnly
}

// IsDangling returns true if the image is dangling, that is an untagged image
// without children and not used in a manifest list.
func (i *Image) IsDangling(ctx context.Context) (bool, error) {
	images, layers, err := i.runtime.getImagesAndLayers()
	if err != nil {
		return false, err
	}
	tree, err := i.runtime.newLayerTreeFromData(images, layers, true)
	if err != nil {
		return false, err
	}
	return i.isDangling(ctx, tree)
}

// isDangling returns true if the image is dangling, that is an untagged image
// without children and not used in a manifest list.  If tree is nil, it will created for this invocation only.
func (i *Image) isDangling(ctx context.Context, tree *layerTree) (bool, error) {
	if len(i.Names()) > 0 {
		return false, nil
	}
	children, err := i.getChildren(ctx, false, tree)
	if err != nil {
		return false, err
	}
	_, usedInManfiestList := tree.manifestListDigests[i.Digest()]
	return (len(children) == 0 && !usedInManfiestList), nil
}

// IsIntermediate returns true if the image is an intermediate image, that is
// an untagged image with children.
func (i *Image) IsIntermediate(ctx context.Context) (bool, error) {
	return i.isIntermediate(ctx, nil)
}

// isIntermediate returns true if the image is an intermediate image, that is
// an untagged image with children.  If tree is nil, it will created for this
// invocation only.
func (i *Image) isIntermediate(ctx context.Context, tree *layerTree) (bool, error) {
	if len(i.Names()) > 0 {
		return false, nil
	}
	children, err := i.getChildren(ctx, false, tree)
	if err != nil {
		return false, err
	}
	return len(children) != 0, nil
}

// Created returns the time the image was created.
func (i *Image) Created() time.Time {
	return i.storageImage.Created
}

// Labels returns the label of the image.
func (i *Image) Labels(ctx context.Context) (map[string]string, error) {
	data, err := i.inspectInfo(ctx)
	if err != nil {
		isManifestList, listErr := i.IsManifestList(ctx)
		if listErr != nil {
			err = fmt.Errorf("fallback error checking whether image is a manifest list: %v: %w", err, err)
		} else if isManifestList {
			logrus.Debugf("Ignoring error: cannot return labels for manifest list or image index %s", i.ID())
			return nil, nil
		}
		return nil, err
	}

	return data.Labels, nil
}

// TopLayer returns the top layer id as a string.
func (i *Image) TopLayer() string {
	return i.storageImage.TopLayer
}

// Parent returns the parent image or nil if there is none.
func (i *Image) Parent(ctx context.Context) (*Image, error) {
	tree, err := i.runtime.newFreshLayerTree()
	if err != nil {
		return nil, err
	}
	return i.parent(ctx, tree)
}

func (i *Image) parent(ctx context.Context, tree *layerTree) (*Image, error) {
	return tree.parent(ctx, i)
}

// HasChildren returns indicates if the image has children.
func (i *Image) HasChildren(ctx context.Context) (bool, error) {
	children, err := i.getChildren(ctx, false, nil)
	if err != nil {
		return false, err
	}
	return len(children) > 0, nil
}

// Children returns the image's children.
func (i *Image) Children(ctx context.Context) ([]*Image, error) {
	children, err := i.getChildren(ctx, true, nil)
	if err != nil {
		return nil, err
	}
	return children, nil
}

// getChildren returns a list of imageIDs that depend on the image. If all is
// false, only the first child image is returned.  If tree is nil, it will be
// created for this invocation only.
func (i *Image) getChildren(ctx context.Context, all bool, tree *layerTree) ([]*Image, error) {
	if tree == nil {
		t, err := i.runtime.newFreshLayerTree()
		if err != nil {
			return nil, err
		}
		tree = t
	}
	return tree.children(ctx, i, all)
}

// Containers returns a list of containers using the image.
func (i *Image) Containers() ([]string, error) {
	var containerIDs []string
	containers, err := i.runtime.store.Containers()
	if err != nil {
		return nil, err
	}
	imageID := i.ID()
	for i := range containers {
		if containers[i].ImageID == imageID {
			containerIDs = append(containerIDs, containers[i].ID)
		}
	}
	return containerIDs, nil
}

// removeContainers removes all containers using the image.
func (i *Image) removeContainers(options *RemoveImagesOptions) error {
	if !options.Force && !options.ExternalContainers {
		// Nothing to do.
		return nil
	}

	if options.Force && options.RemoveContainerFunc != nil {
		logrus.Debugf("Removing containers of image %s with custom removal function", i.ID())
		if err := options.RemoveContainerFunc(i.ID()); err != nil {
			return err
		}
	}

	containers, err := i.Containers()
	if err != nil {
		return err
	}

	if !options.Force && options.ExternalContainers {
		// All containers must be external ones.
		for _, cID := range containers {
			isExternal, err := options.IsExternalContainerFunc(cID)
			if err != nil {
				return fmt.Errorf("checking if %s is an external container: %w", cID, err)
			}
			if !isExternal {
				return fmt.Errorf("cannot remove container %s: not an external container", cID)
			}
		}
	}

	logrus.Debugf("Removing containers of image %s from the local containers storage", i.ID())
	var multiE error
	for _, cID := range containers {
		if err := i.runtime.store.DeleteContainer(cID); err != nil {
			// If the container does not exist anymore, we're good.
			if !errors.Is(err, storage.ErrContainerUnknown) {
				multiE = multierror.Append(multiE, err)
			}
		}
	}

	return multiE
}

// RemoveContainerFunc allows for customizing the removal of containers using
// an image specified by imageID.
type RemoveContainerFunc func(imageID string) error

// RemoveImageReport is the assembled data from removing *one* image.
type RemoveImageReport struct {
	// ID of the image.
	ID string
	// Image was removed.
	Removed bool
	// Size of the removed image.  Only set when explicitly requested in
	// RemoveImagesOptions.
	Size int64
	// The untagged tags.
	Untagged []string
}

// remove removes the image along with all dangling parent images that no other
// image depends on.  The image must not be set read-only and not be used by
// containers.  Returns IDs of removed/untagged images in order.
//
// If the image is used by containers return storage.ErrImageUsedByContainer.
// Use force to remove these containers.
//
// NOTE: the rmMap is used to assemble image-removal data across multiple
// invocations of this function.  The recursive nature requires some
// bookkeeping to make sure that all data is aggregated correctly.
//
// This function is internal.  Users of libimage should always use
// `(*Runtime).RemoveImages()`.
func (i *Image) remove(ctx context.Context, rmMap map[string]*RemoveImageReport, referencedBy string, options *RemoveImagesOptions) ([]string, error) {
	processedIDs := []string{}
	return i.removeRecursive(ctx, rmMap, processedIDs, referencedBy, options)
}

func (i *Image) removeRecursive(ctx context.Context, rmMap map[string]*RemoveImageReport, processedIDs []string, referencedBy string, options *RemoveImagesOptions) ([]string, error) {
	// If referencedBy is empty, the image is considered to be removed via
	// `image remove --all` which alters the logic below.

	// The removal logic below is complex.  There is a number of rules
	// inherited from Podman and Buildah (and Docker).  This function
	// should be the *only* place to extend the removal logic so we keep it
	// sealed in one place.  Make sure to add verbose comments to leave
	// some breadcrumbs for future readers.
	logrus.Debugf("Removing image %s", i.ID())

	if i.IsReadOnly() {
		return processedIDs, fmt.Errorf("cannot remove read-only image %q", i.ID())
	}

	if i.runtime.eventChannel != nil {
		defer i.runtime.writeEvent(&Event{ID: i.ID(), Name: referencedBy, Time: time.Now(), Type: EventTypeImageRemove})
	}

	// Check if already visisted this image.
	report, exists := rmMap[i.ID()]
	if exists {
		// If the image has already been removed, we're done.
		if report.Removed {
			return processedIDs, nil
		}
	} else {
		report = &RemoveImageReport{ID: i.ID()}
		rmMap[i.ID()] = report
	}

	// The image may have already been (partially) removed, so we need to
	// have a closer look at the errors.  On top, image removal should be
	// tolerant toward corrupted images.
	handleError := func(err error) error {
		if ErrorIsImageUnknown(err) {
			// The image or layers of the image may already have been removed
			// in which case we consider the image to be removed.
			return nil
		}
		return err
	}

	// Calculate the size if requested.  `podman-image-prune` likes to
	// report the regained size.
	if options.WithSize {
		size, err := i.Size()
		if handleError(err) != nil {
			return processedIDs, err
		}
		report.Size = size
	}

	skipRemove := false
	numNames := len(i.Names())

	// NOTE: the `numNames != 1` check is not only a performance
	// optimization but also preserves existing Podman/Docker behaviour.
	// If image "foo" is used by a container and has only this tag/name,
	// an `rmi foo` will not untag "foo" but instead attempt to remove the
	// entire image.  If there's a container using "foo", we should get an
	// error.
	if referencedBy != "" && numNames != 1 {
		byID := strings.HasPrefix(i.ID(), referencedBy)
		byDigest := strings.HasPrefix(referencedBy, "sha256:")
		if !options.Force {
			if byID && numNames > 1 {
				return processedIDs, fmt.Errorf("unable to delete image %q by ID with more than one tag (%s): please force removal", i.ID(), i.Names())
			} else if byDigest && numNames > 1 {
				// FIXME - Docker will remove the digest but containers storage
				// does not support that yet, so our hands are tied.
				return processedIDs, fmt.Errorf("unable to delete image %q by digest with more than one tag (%s): please force removal", i.ID(), i.Names())
			}
		}

		// Only try to untag if we know it's not an ID or digest.
		if !byID && !byDigest {
			if err := i.Untag(referencedBy); handleError(err) != nil {
				return processedIDs, err
			}
			report.Untagged = append(report.Untagged, referencedBy)

			// If there's still tags left, we cannot delete it.
			skipRemove = len(i.Names()) > 0
		}
	}

	processedIDs = append(processedIDs, i.ID())
	if skipRemove {
		return processedIDs, nil
	}

	// Perform the container removal, if needed.
	if err := i.removeContainers(options); err != nil {
		return processedIDs, err
	}

	// Podman/Docker compat: we only report an image as removed if it has
	// no children. Otherwise, the data is effectively still present in the
	// storage despite the image being removed.
	hasChildren, err := i.HasChildren(ctx)
	if err != nil {
		// We must be tolerant toward corrupted images.
		// See containers/podman commit fd9dd7065d44.
		logrus.Warnf("Failed to determine if an image is a parent: %v, ignoring the error", err)
		hasChildren = false
	}

	// If there's a dangling parent that no other image depends on, remove
	// it recursively.
	parent, err := i.Parent(ctx)
	if err != nil {
		// We must be tolerant toward corrupted images.
		// See containers/podman commit fd9dd7065d44.
		logrus.Warnf("Failed to determine parent of image: %v, ignoring the error", err)
		parent = nil
	}

	if _, err := i.runtime.store.DeleteImage(i.ID(), true); handleError(err) != nil {
		if errors.Is(err, storage.ErrImageUsedByContainer) {
			err = fmt.Errorf("%w: consider listing external containers and force-removing image", err)
		}
		return processedIDs, err
	}

	report.Untagged = append(report.Untagged, i.Names()...)
	if i.runtime.eventChannel != nil {
		for _, name := range i.Names() {
			i.runtime.writeEvent(&Event{ID: i.ID(), Name: name, Time: time.Now(), Type: EventTypeImageUntag})
		}
	}

	if !hasChildren {
		report.Removed = true
	}

	// Do not delete any parents if NoPrune is true
	if options.NoPrune {
		return processedIDs, nil
	}

	// Check if can remove the parent image.
	if parent == nil {
		return processedIDs, nil
	}

	// Only remove the parent if it's dangling, that is being untagged and
	// without children.
	danglingParent, err := parent.IsDangling(ctx)
	if err != nil {
		// See Podman commit fd9dd7065d44: we need to
		// be tolerant toward corrupted images.
		logrus.Warnf("Failed to determine if an image is a parent: %v, ignoring the error", err)
		danglingParent = false
	}
	if !danglingParent {
		return processedIDs, nil
	}
	// Recurse into removing the parent.
	return parent.removeRecursive(ctx, rmMap, processedIDs, "", options)
}

var errTagDigest = errors.New("tag by digest not supported")

// Tag the image with the specified name and store it in the local containers
// storage.  The name is normalized according to the rules of NormalizeName.
func (i *Image) Tag(name string) error {
	if strings.HasPrefix(name, "sha256:") { // ambiguous input
		return fmt.Errorf("%s: %w", name, errTagDigest)
	}

	ref, err := NormalizeName(name)
	if err != nil {
		return fmt.Errorf("normalizing name %q: %w", name, err)
	}

	if _, isDigested := ref.(reference.Digested); isDigested {
		return fmt.Errorf("%s: %w", name, errTagDigest)
	}

	logrus.Debugf("Tagging image %s with %q", i.ID(), ref.String())
	if i.runtime.eventChannel != nil {
		defer i.runtime.writeEvent(&Event{ID: i.ID(), Name: name, Time: time.Now(), Type: EventTypeImageTag})
	}

	if err := i.runtime.store.AddNames(i.ID(), []string{ref.String()}); err != nil {
		return err
	}

	return i.reload()
}

// to have some symmetry with the errors from containers/storage.
var errTagUnknown = errors.New("tag not known")

// TODO (@vrothberg) - `docker rmi sha256:` will remove the digest from the
// image.  However, that's something containers storage does not support.
var errUntagDigest = errors.New("untag by digest not supported")

// Untag the image with the specified name and make the change persistent in
// the local containers storage.  The name is normalized according to the rules
// of NormalizeName.
func (i *Image) Untag(name string) error {
	if strings.HasPrefix(name, "sha256:") { // ambiguous input
		return fmt.Errorf("%s: %w", name, errUntagDigest)
	}

	ref, err := NormalizeName(name)
	if err != nil {
		return err
	}

	// FIXME: this is breaking Podman CI but must be re-enabled once
	// c/storage supports altering the digests of an image.  Then,
	// Podman will do the right thing.
	//
	// !!! Also make sure to re-enable the tests !!!
	//
	// if _, isDigested := ref.(reference.Digested); isDigested {
	// 	return fmt.Errorf("%s: %w", name, errUntagDigest)
	// }

	name = ref.String()

	// Return an error if the name is not found, the c/storage
	// RemoveNames() API does not create one if no match is found.
	if !slices.Contains(i.Names(), name) {
		return fmt.Errorf("%s: %w", name, errTagUnknown)
	}

	logrus.Debugf("Untagging %q from image %s", ref.String(), i.ID())
	if i.runtime.eventChannel != nil {
		defer i.runtime.writeEvent(&Event{ID: i.ID(), Name: name, Time: time.Now(), Type: EventTypeImageUntag})
	}

	if err := i.runtime.store.RemoveNames(i.ID(), []string{name}); err != nil {
		return err
	}

	return i.reload()
}

// RepoTags returns a string slice of repotags associated with the image.
func (i *Image) RepoTags() ([]string, error) {
	namedTagged, err := i.NamedTaggedRepoTags()
	if err != nil {
		return nil, err
	}
	repoTags := make([]string, len(namedTagged))
	for i := range namedTagged {
		repoTags[i] = namedTagged[i].String()
	}
	return repoTags, nil
}

// NamedTaggedRepoTags returns the repotags associated with the image as a
// slice of reference.NamedTagged.
func (i *Image) NamedTaggedRepoTags() ([]reference.NamedTagged, error) {
	repoTags := make([]reference.NamedTagged, 0, len(i.Names()))
	for _, name := range i.Names() {
		parsed, err := reference.Parse(name)
		if err != nil {
			return nil, err
		}
		named, isNamed := parsed.(reference.Named)
		if !isNamed {
			continue
		}
		tagged, isTagged := named.(reference.NamedTagged)
		if !isTagged {
			continue
		}
		repoTags = append(repoTags, tagged)
	}
	return repoTags, nil
}

// NamedRepoTags returns the repotags associated with the image as a
// slice of reference.Named.
func (i *Image) NamedRepoTags() ([]reference.Named, error) {
	// FIXME: the NamedRepoTags name is a bit misleading as it can return
	// repo@digest values if that’s how an image was pulled.
	var repoTags []reference.Named
	for _, name := range i.Names() {
		parsed, err := reference.Parse(name)
		if err != nil {
			return nil, err
		}
		if named, isNamed := parsed.(reference.Named); isNamed {
			repoTags = append(repoTags, named)
		}
	}
	return repoTags, nil
}

// referenceFuzzilyMatchingRepoAndTag checks if the image’s repo (and tag if requiredTag != "") matches a fuzzy short input,
// and if so, returns the matching reference.
//
// DO NOT ADD ANY NEW USERS OF THIS SEMANTICS. Rely on existing libimage calls like LookupImage instead,
// and handle unqualified the way it does (c/image/pkg/shortnames).
func (i *Image) referenceFuzzilyMatchingRepoAndTag(requiredRepo reference.Named, requiredTag string) (reference.Named, error) {
	repoTags, err := i.NamedRepoTags()
	if err != nil {
		return nil, err
	}

	name := requiredRepo.Name()
	for _, r := range repoTags {
		if requiredTag != "" {
			tagged, isTagged := r.(reference.NamedTagged)
			if !isTagged || tagged.Tag() != requiredTag {
				continue
			}
		}

		repoName := r.Name()
		if !strings.HasSuffix(repoName, name) {
			continue
		}
		if len(repoName) == len(name) { // full match
			return r, nil
		}
		if repoName[len(repoName)-len(name)-1] == '/' { // matches at repo
			return r, nil
		}
	}

	return nil, nil
}

// RepoDigests returns a string array of repodigests associated with the image.
func (i *Image) RepoDigests() ([]string, error) {
	repoDigests := []string{}
	added := make(map[string]struct{})

	for _, name := range i.Names() {
		for _, imageDigest := range append(i.Digests(), i.Digest()) {
			if imageDigest == "" {
				continue
			}

			named, err := reference.ParseNormalizedNamed(name)
			if err != nil {
				return nil, err
			}

			canonical, err := reference.WithDigest(reference.TrimNamed(named), imageDigest)
			if err != nil {
				return nil, err
			}

			if _, alreadyInList := added[canonical.String()]; !alreadyInList {
				repoDigests = append(repoDigests, canonical.String())
				added[canonical.String()] = struct{}{}
			}
		}
	}
	sort.Strings(repoDigests)
	return repoDigests, nil
}

// Mount the image with the specified mount options and label, both of which
// are directly passed down to the containers storage.  Returns the fully
// evaluated path to the mount point.
func (i *Image) Mount(_ context.Context, mountOptions []string, mountLabel string) (string, error) {
	if i.runtime.eventChannel != nil {
		defer i.runtime.writeEvent(&Event{ID: i.ID(), Name: "", Time: time.Now(), Type: EventTypeImageMount})
	}

	mountPoint, err := i.runtime.store.MountImage(i.ID(), mountOptions, mountLabel)
	if err != nil {
		return "", err
	}
	mountPoint, err = filepath.EvalSymlinks(mountPoint)
	if err != nil {
		return "", err
	}
	logrus.Debugf("Mounted image %s at %q", i.ID(), mountPoint)
	return mountPoint, nil
}

// Mountpoint returns the path to image's mount point.  The path is empty if
// the image is not mounted.
func (i *Image) Mountpoint() (string, error) {
	for _, layerID := range append([]string{i.TopLayer()}, i.storageImage.MappedTopLayers...) {
		mountedTimes, err := i.runtime.store.Mounted(layerID)
		if err != nil {
			if errors.Is(err, storage.ErrLayerUnknown) {
				// Can happen, Podman did it, but there's no
				// explanation why.
				continue
			}
			return "", err
		}
		if mountedTimes > 0 {
			layer, err := i.runtime.store.Layer(layerID)
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(layer.MountPoint)
		}
	}
	return "", nil
}

// Unmount the image.  Use force to ignore the reference counter and forcefully
// unmount.
func (i *Image) Unmount(force bool) error {
	if i.runtime.eventChannel != nil {
		defer i.runtime.writeEvent(&Event{ID: i.ID(), Name: "", Time: time.Now(), Type: EventTypeImageUnmount})
	}
	logrus.Debugf("Unmounted image %s", i.ID())
	_, err := i.runtime.store.UnmountImage(i.ID(), force)
	return err
}

// Size computes the size of the image layers and associated data.
func (i *Image) Size() (int64, error) {
	if i.cached.size != nil {
		return *i.cached.size, nil
	}

	size, err := i.runtime.store.ImageSize(i.ID())
	i.cached.size = &size
	return size, err
}

// HasDifferentDigestOptions allows for customizing the check if another
// (remote) image has a different digest.
type HasDifferentDigestOptions struct {
	// containers-auth.json(5) file to use when authenticating against
	// container registries.
	AuthFilePath string
	// Allow contacting registries over HTTP, or HTTPS with failed TLS
	// verification. Note that this does not affect other TLS connections.
	InsecureSkipTLSVerify types.OptionalBool
}

// HasDifferentDigest returns true if the image specified by `remoteRef` has a
// different digest than the local one.  This check can be useful to check for
// updates on remote registries.
func (i *Image) HasDifferentDigest(ctx context.Context, remoteRef types.ImageReference, options *HasDifferentDigestOptions) (bool, error) {
	// We need to account for the arch that the image uses.  It seems
	// common on ARM to tweak this option to pull the correct image.  See
	// github.com/containers/podman/issues/6613.
	inspectInfo, err := i.inspectInfo(ctx)
	if err != nil {
		return false, err
	}

	sys := i.runtime.systemContextCopy()
	sys.ArchitectureChoice = inspectInfo.Architecture
	// OS and variant may not be set, so let's check to avoid accidental
	// overrides of the runtime settings.
	if inspectInfo.Os != "" {
		sys.OSChoice = inspectInfo.Os
	}
	if inspectInfo.Variant != "" {
		sys.VariantChoice = inspectInfo.Variant
	}

	if options != nil {
		if options.AuthFilePath != "" {
			sys.AuthFilePath = options.AuthFilePath
		}
		if options.InsecureSkipTLSVerify != types.OptionalBoolUndefined {
			sys.DockerInsecureSkipTLSVerify = options.InsecureSkipTLSVerify
			sys.OCIInsecureSkipTLSVerify = options.InsecureSkipTLSVerify == types.OptionalBoolTrue
			sys.DockerDaemonInsecureSkipTLSVerify = options.InsecureSkipTLSVerify == types.OptionalBoolTrue
		}
	}

	return i.hasDifferentDigestWithSystemContext(ctx, remoteRef, sys)
}

func (i *Image) hasDifferentDigestWithSystemContext(ctx context.Context, remoteRef types.ImageReference, sys *types.SystemContext) (bool, error) {
	remoteImg, err := remoteRef.NewImage(ctx, sys)
	if err != nil {
		return false, err
	}
	defer remoteImg.Close()

	rawManifest, rawManifestMIMEType, err := remoteImg.Manifest(ctx)
	if err != nil {
		return false, err
	}

	// If the remote ref's manifest is a list, try to zero in on the image
	// in the list that we would eventually try to pull.
	var remoteDigest digest.Digest
	if manifest.MIMETypeIsMultiImage(rawManifestMIMEType) {
		list, err := manifest.ListFromBlob(rawManifest, rawManifestMIMEType)
		if err != nil {
			return false, err
		}
		remoteDigest, err = list.ChooseInstance(sys)
		if err != nil {
			return false, err
		}
	} else {
		remoteDigest, err = manifest.Digest(rawManifest)
		if err != nil {
			return false, err
		}
	}

	// Check if we already have that image's manifest in this image.  A
	// single image can have multiple manifests that describe the same
	// config blob and layers, so treat any match as a successful match.
	for _, digest := range append(i.Digests(), i.Digest()) {
		if digest.Validate() != nil {
			continue
		}
		if digest.String() == remoteDigest.String() {
			return false, nil
		}
	}
	// No matching digest found in the local image.
	return true, nil
}

// driverData gets the driver data from the store on a layer.
func (i *Image) driverData() (*DriverData, error) {
	store := i.runtime.store
	layerID := i.TopLayer()
	driver, err := store.GraphDriver()
	if err != nil {
		return nil, err
	}
	metaData, err := driver.Metadata(layerID)
	if err != nil {
		return nil, err
	}
	if mountTimes, err := store.Mounted(layerID); mountTimes == 0 || err != nil {
		delete(metaData, "MergedDir")
	}
	return &DriverData{
		Name: driver.String(),
		Data: metaData,
	}, nil
}

// StorageReference returns the image's reference to the containers storage
// using the image ID.
func (i *Image) StorageReference() (types.ImageReference, error) {
	if i.storageReference != nil {
		return i.storageReference, nil
	}
	ref, err := storageTransport.Transport.ParseStoreReference(i.runtime.store, "@"+i.ID())
	if err != nil {
		return nil, err
	}
	i.storageReference = ref
	return ref, nil
}

// source returns the possibly cached image reference.
func (i *Image) source(ctx context.Context) (types.ImageSource, error) {
	if i.cached.imageSource != nil {
		return i.cached.imageSource, nil
	}
	ref, err := i.StorageReference()
	if err != nil {
		return nil, err
	}
	src, err := ref.NewImageSource(ctx, i.runtime.systemContextCopy())
	if err != nil {
		return nil, err
	}
	i.cached.imageSource = src
	return src, nil
}

// rawConfigBlob returns the image's config as a raw byte slice.  Users need to
// unmarshal it to the corresponding type (OCI, Docker v2s{1,2}).
func (i *Image) rawConfigBlob(ctx context.Context) ([]byte, error) {
	ref, err := i.StorageReference()
	if err != nil {
		return nil, err
	}

	imageCloser, err := ref.NewImage(ctx, i.runtime.systemContextCopy())
	if err != nil {
		return nil, err
	}
	defer imageCloser.Close()

	return imageCloser.ConfigBlob(ctx)
}

// Manifest returns the raw data and the MIME type of the image's manifest.
func (i *Image) Manifest(ctx context.Context) (rawManifest []byte, mimeType string, err error) {
	src, err := i.source(ctx)
	if err != nil {
		return nil, "", err
	}
	return image.UnparsedInstance(src, nil).Manifest(ctx)
}

// getImageID creates an image object and uses the hex value of the config
// blob's digest (if it has one) as the image ID for parsing the store reference.
func getImageID(ctx context.Context, src types.ImageReference, sys *types.SystemContext) (string, error) {
	newImg, err := src.NewImage(ctx, sys)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := newImg.Close(); err != nil {
			logrus.Errorf("Failed to close image: %q", err)
		}
	}()
	imageDigest := newImg.ConfigInfo().Digest
	if err = imageDigest.Validate(); err != nil {
		return "", fmt.Errorf("getting config info: %w", err)
	}
	return "@" + imageDigest.Encoded(), nil
}

// Checks whether the image matches the specified platform.
// Returns
//   - 1) a matching error that can be used for logging (or returning) what does not match
//   - 2) a bool indicating whether architecture, os or variant were set (some callers need that to decide whether they need to throw an error)
//   - 3) a fatal error that occurred prior to check for matches (e.g., storage errors etc.)
func (i *Image) matchesPlatform(ctx context.Context, os, arch, variant string) (error, bool, error) {
	if err := i.isCorrupted(ctx, ""); err != nil {
		return err, false, nil
	}
	inspectInfo, err := i.inspectInfo(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("inspecting image: %w", err)
	}

	customPlatform := len(os)+len(arch)+len(variant) != 0

	expected, err := platforms.Parse(platform.ToString(os, arch, variant))
	if err != nil {
		return nil, false, fmt.Errorf("parsing host platform: %v", err)
	}
	fromImage, err := platforms.Parse(platform.ToString(inspectInfo.Os, inspectInfo.Architecture, inspectInfo.Variant))
	if err != nil {
		return nil, false, fmt.Errorf("parsing image platform: %v", err)
	}

	if platforms.NewMatcher(expected).Match(fromImage) {
		return nil, customPlatform, nil
	}

	return fmt.Errorf("image platform (%s) does not match the expected platform (%s)", platforms.Format(fromImage), platforms.Format(expected)), customPlatform, nil
}
