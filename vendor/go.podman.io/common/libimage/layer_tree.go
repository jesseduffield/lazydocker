//go:build !remote

package libimage

import (
	"context"
	"errors"

	digest "github.com/opencontainers/go-digest"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage"
	storageTypes "go.podman.io/storage/types"
)

// layerTree is an internal representation of local layers.
type layerTree struct {
	// nodes is the actual layer tree with layer IDs being keys.
	nodes map[string]*layerNode
	// ociCache is a cache for Image.ID -> OCI Image. Translations are done
	// on-demand.
	ociCache map[string]*ociv1.Image
	// emptyImages do not have any top-layer so we cannot create a
	// *layerNode for them.
	emptyImages []*Image
	// manifestList keep track of images based on their digest.
	// Library will use this map when checking if a image is dangling.
	// If an image is used in a manifestList it is NOT dangling
	manifestListDigests map[digest.Digest]struct{}
}

// node returns a layerNode for the specified layerID.
func (t *layerTree) node(layerID string) *layerNode {
	node, exists := t.nodes[layerID]
	if !exists {
		node = &layerNode{}
		t.nodes[layerID] = node
	}
	return node
}

// ErrorIsImageUnknown returns true if the specified error indicates that an
// image is unknown or has been partially removed (e.g., a missing layer).
func ErrorIsImageUnknown(err error) bool {
	return errors.Is(err, storage.ErrImageUnknown) ||
		errors.Is(err, storageTypes.ErrLayerUnknown) ||
		errors.Is(err, storageTypes.ErrSizeUnknown) ||
		errors.Is(err, storage.ErrNotAnImage)
}

// toOCI returns an OCI image for the specified image.
//
// WARNING: callers are responsible for handling cases where the target image
// has been (partially) removed and can use `ErrorIsImageUnknown` to detect it.
func (t *layerTree) toOCI(ctx context.Context, i *Image) (*ociv1.Image, error) {
	var err error
	oci, exists := t.ociCache[i.ID()]
	if !exists {
		oci, err = i.toOCI(ctx)
		if err == nil {
			t.ociCache[i.ID()] = oci
		}
	}
	return oci, err
}

// layerNode is a node in a layerTree.  It's ID is the key in a layerTree.
type layerNode struct {
	children []*layerNode
	images   []*Image
	parent   *layerNode
	layer    *storage.Layer
}

// repoTags assemble all repo tags all of images of the layer node.
func (l *layerNode) repoTags() ([]string, error) {
	orderedTags := []string{}
	visitedTags := make(map[string]bool)

	for _, image := range l.images {
		repoTags, err := image.RepoTags()
		if err != nil {
			return nil, err
		}
		for _, tag := range repoTags {
			if _, visited := visitedTags[tag]; visited {
				continue
			}
			visitedTags[tag] = true
			orderedTags = append(orderedTags, tag)
		}
	}

	return orderedTags, nil
}

// newFreshLayerTree extracts a layerTree from consistent layers and images in the local storage.
func (r *Runtime) newFreshLayerTree() (*layerTree, error) {
	images, layers, err := r.getImagesAndLayers()
	if err != nil {
		return nil, err
	}
	return r.newLayerTreeFromData(images, layers, false)
}

// newLayerTreeFromData extracts a layerTree from the given the layers and images.
// The caller is responsible for (layers, images) being consistent.
func (r *Runtime) newLayerTreeFromData(images []*Image, layers []storage.Layer, generateManifestDigestList bool) (*layerTree, error) {
	tree := layerTree{
		nodes:               make(map[string]*layerNode),
		ociCache:            make(map[string]*ociv1.Image),
		manifestListDigests: make(map[digest.Digest]struct{}),
	}

	// First build a tree purely based on layer information.
	for i := range layers {
		node := tree.node(layers[i].ID)
		node.layer = &layers[i]
		if layers[i].Parent == "" {
			continue
		}
		parent := tree.node(layers[i].Parent)
		node.parent = parent
		parent.children = append(parent.children, node)
	}

	// Now assign the images to each (top) layer.
	for i := range images {
		img := images[i] // do not leak loop variable outside the scope
		topLayer := img.TopLayer()
		if topLayer == "" {
			tree.emptyImages = append(tree.emptyImages, img)
			// When img is a manifest list, cache the lists of
			// digests refereenced in manifest list. Digests can
			// be used to check for dangling images.
			if !generateManifestDigestList {
				continue
			}
			// ignore errors, common errors are
			//  - image is not manifest
			//  - image has been removed from the store in the meantime
			// In all cases we should ensure image listing still works and not error out.
			mlist, err := img.ToManifestList()
			if err != nil {
				// If it is not a manifest it likely is a regular image so just ignore it.
				// If the image is unknown that likely means there was a race where the image/manifest
				// was removed after out MultiList() call so we ignore that as well.
				if errors.Is(err, ErrNotAManifestList) || errors.Is(err, storageTypes.ErrImageUnknown) {
					continue
				}
				return nil, err
			}

			for _, digest := range mlist.list.Instances() {
				tree.manifestListDigests[digest] = struct{}{}
			}
			continue
		}
		node, exists := tree.nodes[topLayer]
		if !exists {
			// Note: erroring out in this case has turned out having been a
			// mistake. Users may not be able to recover, so we're now
			// throwing a warning to guide them to resolve the issue and
			// turn the errors non-fatal.
			logrus.Warnf("Top layer %s of image %s not found in layer tree. The storage may be corrupted, consider running `podman system check`.", topLayer, img.ID())
			continue
		}
		node.images = append(node.images, img)
	}

	return &tree, nil
}

// children returns the child images of parent. Child images are images with
// either the same top layer as parent or parent being the true parent layer.
// Furthermore, the history of the parent and child images must match with the
// parent having one history item less.  If all is true, all images are
// returned.  Otherwise, the first image is returned.  Note that manifest lists
// do not have children.
func (t *layerTree) children(ctx context.Context, parent *Image, all bool) ([]*Image, error) {
	if parent.TopLayer() == "" {
		if isManifestList, _ := parent.IsManifestList(ctx); isManifestList {
			return nil, nil
		}
	}

	parentID := parent.ID()
	parentOCI, err := t.toOCI(ctx, parent)
	if err != nil {
		if ErrorIsImageUnknown(err) {
			return nil, nil
		}
		return nil, err
	}

	// checkParent returns true if child and parent are in such a relation.
	checkParent := func(child *Image) (bool, error) {
		if parentID == child.ID() {
			return false, nil
		}
		childOCI, err := t.toOCI(ctx, child)
		if err != nil {
			if ErrorIsImageUnknown(err) {
				return false, nil
			}
			return false, err
		}
		// History check.
		return areParentAndChild(parentOCI, childOCI), nil
	}

	var children []*Image

	// Empty images are special in that they do not have any physical layer
	// but yet can have a parent-child relation.  Hence, compare the
	// "parent" image to all other known empty images.
	if parent.TopLayer() == "" {
		for i := range t.emptyImages {
			empty := t.emptyImages[i]
			isManifest, err := empty.IsManifestList(ctx)
			if err != nil {
				return nil, err
			}
			if isManifest {
				// If this is a manifest list and is already
				// marked as empty then no instance can be
				// selected from this list therefore its
				// better to skip this.
				continue
			}
			isParent, err := checkParent(empty)
			if err != nil {
				return nil, err
			}
			if isParent {
				children = append(children, empty)
				if !all {
					break
				}
			}
		}
		return children, nil
	}

	parentNode, exists := t.nodes[parent.TopLayer()]
	if !exists {
		// Note: erroring out in this case has turned out having been a
		// mistake. Users may not be able to recover, so we're now
		// throwing a warning to guide them to resolve the issue and
		// turn the errors non-fatal.
		logrus.Warnf("Layer %s not found in layer tree. The storage may be corrupted, consider running `podman system check`.", parent.TopLayer())
		return children, nil
	}

	// addChildrenFrom adds child images of parent to children.  Returns
	// true if any image is a child of parent.
	addChildrenFromNode := func(node *layerNode) (bool, error) {
		foundChildren := false
		for i, childImage := range node.images {
			isChild, err := checkParent(childImage)
			if err != nil {
				return foundChildren, err
			}
			if isChild {
				foundChildren = true
				children = append(children, node.images[i])
				if all {
					return foundChildren, nil
				}
			}
		}
		return foundChildren, nil
	}

	// First check images where parent's top layer is also the parent
	// layer.
	for _, childNode := range parentNode.children {
		found, err := addChildrenFromNode(childNode)
		if err != nil {
			return nil, err
		}
		if found && all {
			return children, nil
		}
	}

	// Now check images with the same top layer.
	if _, err := addChildrenFromNode(parentNode); err != nil {
		return nil, err
	}

	return children, nil
}

// parent returns the parent image or nil if no parent image could be found.
// Note that manifest lists do not have parents.
func (t *layerTree) parent(ctx context.Context, child *Image) (*Image, error) {
	if child.TopLayer() == "" {
		if isManifestList, _ := child.IsManifestList(ctx); isManifestList {
			return nil, nil
		}
	}

	childID := child.ID()
	childOCI, err := t.toOCI(ctx, child)
	if err != nil {
		if ErrorIsImageUnknown(err) {
			return nil, nil
		}
		return nil, err
	}

	// Empty images are special in that they do not have any physical layer
	// but yet can have a parent-child relation.  Hence, compare the
	// "child" image to all other known empty images.
	if child.TopLayer() == "" {
		for _, empty := range t.emptyImages {
			if childID == empty.ID() {
				continue
			}
			isManifest, err := empty.IsManifestList(ctx)
			if err != nil {
				return nil, err
			}
			if isManifest {
				// If this is a manifest list and is already
				// marked as empty then no instance can be
				// selected from this list therefore its
				// better to skip this.
				continue
			}
			emptyOCI, err := t.toOCI(ctx, empty)
			if err != nil {
				if ErrorIsImageUnknown(err) {
					return nil, nil
				}
				return nil, err
			}
			// History check.
			if areParentAndChild(emptyOCI, childOCI) {
				return empty, nil
			}
		}
		return nil, nil
	}

	node, exists := t.nodes[child.TopLayer()]
	if !exists {
		// Note: erroring out in this case has turned out having been a
		// mistake. Users may not be able to recover, so we're now
		// throwing a warning to guide them to resolve the issue and
		// turn the errors non-fatal.
		logrus.Warnf("Layer %s not found in layer tree. The storage may be corrupted, consider running `podman system check`.", child.TopLayer())
		return nil, nil
	}

	// Check images from the parent node (i.e., parent layer) and images
	// with the same layer (i.e., same top layer).
	images := node.images
	if node.parent != nil {
		images = append(images, node.parent.images...)
	}
	for _, parent := range images {
		if parent.ID() == childID {
			continue
		}
		parentOCI, err := t.toOCI(ctx, parent)
		if err != nil {
			if ErrorIsImageUnknown(err) {
				return nil, nil
			}
			return nil, err
		}
		// History check.
		if areParentAndChild(parentOCI, childOCI) {
			return parent, nil
		}
	}

	return nil, nil
}
