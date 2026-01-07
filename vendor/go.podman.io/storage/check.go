package storage

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	drivers "go.podman.io/storage/drivers"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/types"
)

var (
	// ErrLayerUnaccounted describes a layer that is present in the lower-level storage driver,
	// but which is not known to or managed by the higher-level driver-agnostic logic.
	ErrLayerUnaccounted = types.ErrLayerUnaccounted
	// ErrLayerUnreferenced describes a layer which is not used by any image or container.
	ErrLayerUnreferenced = types.ErrLayerUnreferenced
	// ErrLayerIncorrectContentDigest describes a layer for which the contents of one or more
	// files which were added in the layer appear to have changed.  It may instead look like an
	// unnamed "file integrity checksum failed" error.
	ErrLayerIncorrectContentDigest = types.ErrLayerIncorrectContentDigest
	// ErrLayerIncorrectContentSize describes a layer for which regenerating the diff that was
	// used to populate the layer produced a diff of a different size.  We check the digest
	// first, so it's highly unlikely you'll ever see this error.
	ErrLayerIncorrectContentSize = types.ErrLayerIncorrectContentSize
	// ErrLayerContentModified describes a layer which contains contents which should not be
	// there, or for which ownership/permissions/dates have been changed.
	ErrLayerContentModified = types.ErrLayerContentModified
	// ErrLayerDataMissing describes a layer which is missing a big data item.
	ErrLayerDataMissing = types.ErrLayerDataMissing
	// ErrLayerMissing describes a layer which is the missing parent of a layer.
	ErrLayerMissing = types.ErrLayerMissing
	// ErrImageLayerMissing describes an image which claims to have a layer that we don't know
	// about.
	ErrImageLayerMissing = types.ErrImageLayerMissing
	// ErrImageDataMissing describes an image which is missing a big data item.
	ErrImageDataMissing = types.ErrImageDataMissing
	// ErrImageDataIncorrectSize describes an image which has a big data item which looks like
	// its size has changed, likely because it's been modified somehow.
	ErrImageDataIncorrectSize = types.ErrImageDataIncorrectSize
	// ErrContainerImageMissing describes a container which claims to be based on an image that
	// we don't know about.
	ErrContainerImageMissing = types.ErrContainerImageMissing
	// ErrContainerDataMissing describes a container which is missing a big data item.
	ErrContainerDataMissing = types.ErrContainerDataMissing
	// ErrContainerDataIncorrectSize describes a container which has a big data item which looks
	// like its size has changed, likely because it's been modified somehow.
	ErrContainerDataIncorrectSize = types.ErrContainerDataIncorrectSize
)

const (
	defaultMaximumUnreferencedLayerAge = 24 * time.Hour
)

// CheckOptions is the set of options for Check(), specifying which tests to perform.
type CheckOptions struct {
	LayerUnreferencedMaximumAge *time.Duration // maximum allowed age of unreferenced layers
	LayerDigests                bool           // check that contents of image layer diffs can still be reconstructed
	LayerMountable              bool           // check that layers are mountable
	LayerContents               bool           // check that contents of image layers match their diffs, with no unexpected changes, requires LayerMountable
	LayerData                   bool           // check that associated "big" data items are present and can be read
	ImageData                   bool           // check that associated "big" data items are present, can be read, and match the recorded size
	ContainerData               bool           // check that associated "big" data items are present and can be read
}

// checkIgnore is used to tell functions that compare the contents of a mounted
// layer to the contents that we'd expect it to have to ignore certain
// discrepancies
type checkIgnore struct {
	ownership, timestamps, permissions, filetype bool
}

// CheckMost returns a CheckOptions with mostly just "quick" checks enabled.
func CheckMost() *CheckOptions {
	return &CheckOptions{
		LayerDigests:   true,
		LayerMountable: true,
		LayerContents:  false,
		LayerData:      true,
		ImageData:      true,
		ContainerData:  true,
	}
}

// CheckEverything returns a CheckOptions with every check enabled.
func CheckEverything() *CheckOptions {
	return &CheckOptions{
		LayerDigests:   true,
		LayerMountable: true,
		LayerContents:  true,
		LayerData:      true,
		ImageData:      true,
		ContainerData:  true,
	}
}

// CheckReport is a list of detected problems.
type CheckReport struct {
	Layers                map[string][]error // damaged read-write layers
	ROLayers              map[string][]error // damaged read-only layers
	layerParentsByLayerID map[string]string
	layerOrder            map[string]int
	Images                map[string][]error // damaged read-write images (including those with damaged layers)
	ROImages              map[string][]error // damaged read-only images (including those with damaged layers)
	Containers            map[string][]error // damaged containers (including those based on damaged images)
}

// RepairOptions is the set of options for Repair().
type RepairOptions struct {
	RemoveContainers bool // Remove damaged containers
}

// RepairEverything returns a RepairOptions with every optional remediation
// enabled.
func RepairEverything() *RepairOptions {
	return &RepairOptions{
		RemoveContainers: true,
	}
}

// Check returns a list of problems with what's in the store, as a whole.  It can be very expensive
// to call.
func (s *store) Check(options *CheckOptions) (CheckReport, error) {
	var ignore checkIgnore
	for _, o := range s.graphOptions {
		if strings.Contains(o, "ignore_chown_errors=true") {
			ignore.ownership = true
		}
		if strings.Contains(o, "force_mask=") {
			ignore.ownership = true
			ignore.permissions = true
			ignore.filetype = true
		}
	}
	for o := range s.pullOptions {
		if strings.Contains(o, "use_hard_links") {
			if s.pullOptions[o] == "true" {
				ignore.timestamps = true
			}
		}
	}

	if options == nil {
		options = CheckMost()
	}

	report := CheckReport{
		Layers:                make(map[string][]error),
		ROLayers:              make(map[string][]error),
		layerParentsByLayerID: make(map[string]string), // layers ID -> their parent's ID, if there is one
		layerOrder:            make(map[string]int),    // layers ID -> order for removal, if we needed to remove them all
		Images:                make(map[string][]error),
		ROImages:              make(map[string][]error),
		Containers:            make(map[string][]error),
	}

	// This map will track known layer IDs.  If we have multiple stores, read-only ones can
	// contain copies of layers that are in the read-write store, but we'll only ever be
	// mounting or extracting contents from the read-write versions, since we always search it
	// first.  The boolean will track if the layer is referenced by at least one image or
	// container.
	referencedLayers := make(map[string]bool)
	referencedROLayers := make(map[string]bool)

	// This map caches the headers for items included in layer diffs.
	diffHeadersByLayer := make(map[string][]*tar.Header)
	var diffHeadersByLayerMutex sync.Mutex

	// Walk the list of layer stores, looking at each layer that we didn't see in a
	// previously-visited store.
	if _, _, err := readOrWriteAllLayerStores(s, func(store roLayerStore) (struct{}, bool, error) {
		layers, err := store.Layers()
		if err != nil {
			return struct{}{}, true, err
		}
		isReadWrite := roLayerStoreIsReallyReadWrite(store)
		readWriteDesc := ""
		if !isReadWrite {
			readWriteDesc = "read-only "
		}
		// Examine each layer in turn.
		for i := range layers {
			layer := layers[i]
			id := layer.ID
			// If we've already seen a layer with this ID, no need to process it again.
			if _, checked := referencedLayers[id]; checked {
				continue
			}
			if _, checked := referencedROLayers[id]; checked {
				continue
			}
			// Note the parent of this layer, and add it to the map of known layers so
			// that we know that we've visited it, but we haven't confirmed that it's
			// used by anything.
			report.layerParentsByLayerID[id] = layer.Parent
			if isReadWrite {
				referencedLayers[id] = false
			} else {
				referencedROLayers[id] = false
			}
			logrus.Debugf("checking %slayer %s", readWriteDesc, id)
			// Check that all of the big data items are present and can be read.  We
			// have no digest or size information to compare the contents to (grumble),
			// so we can't verify that the contents haven't been changed since they
			// were stored.
			if options.LayerData {
				for _, name := range layer.BigDataNames {
					func() {
						rc, err := store.BigData(id, name)
						if err != nil {
							if errors.Is(err, os.ErrNotExist) {
								err := fmt.Errorf("%slayer %s: data item %q: %w", readWriteDesc, id, name, ErrLayerDataMissing)
								if isReadWrite {
									report.Layers[id] = append(report.Layers[id], err)
								} else {
									report.ROLayers[id] = append(report.ROLayers[id], err)
								}
								return
							}
							err = fmt.Errorf("%slayer %s: data item %q: %w", readWriteDesc, id, name, err)
							if isReadWrite {
								report.Layers[id] = append(report.Layers[id], err)
							} else {
								report.ROLayers[id] = append(report.ROLayers[id], err)
							}
							return
						}
						defer rc.Close()
						if _, err = io.Copy(io.Discard, rc); err != nil {
							err = fmt.Errorf("%slayer %s: data item %q: %w", readWriteDesc, id, name, err)
							if isReadWrite {
								report.Layers[id] = append(report.Layers[id], err)
							} else {
								report.ROLayers[id] = append(report.ROLayers[id], err)
							}
							return
						}
					}()
				}
			}
			// Check that the content we get back when extracting the layer's contents
			// match the recorded digest and size.  A layer for which they're not given
			// isn't a part of an image, and is likely the read-write layer for a
			// container, and we can't vouch for the integrity of its contents.
			// For each layer with known contents, record the headers for the layer's
			// diff, which we can use to reconstruct the expected contents for the tree
			// we see when the layer is mounted.
			if options.LayerDigests && layer.UncompressedDigest != "" {
				func() {
					expectedDigest := layer.UncompressedDigest
					// Double-check that the digest isn't invalid somehow.
					if err := layer.UncompressedDigest.Validate(); err != nil {
						err := fmt.Errorf("%slayer %s: %w", readWriteDesc, id, err)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], err)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], err)
						}
						return
					}
					// Extract the diff.
					uncompressed := archive.Uncompressed
					diffOptions := DiffOptions{
						Compression: &uncompressed,
					}
					diff, err := store.Diff("", id, &diffOptions)
					if err != nil {
						err := fmt.Errorf("%slayer %s: %w", readWriteDesc, id, err)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], err)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], err)
						}
						return
					}
					// Digest and count the length of the diff.
					digester := expectedDigest.Algorithm().Digester()
					counter := ioutils.NewWriteCounter(digester.Hash())
					reader := io.TeeReader(diff, counter)
					var wg sync.WaitGroup
					var archiveErr error
					wg.Add(1)
					go func(layerID string, diffReader io.Reader) {
						// Read the diff, one item at a time.
						tr := tar.NewReader(diffReader)
						hdr, err := tr.Next()
						for err == nil {
							diffHeadersByLayerMutex.Lock()
							diffHeadersByLayer[layerID] = append(diffHeadersByLayer[layerID], hdr)
							diffHeadersByLayerMutex.Unlock()
							hdr, err = tr.Next()
						}
						if !errors.Is(err, io.EOF) {
							archiveErr = err
						}
						// consume any trailer after the EOF marker
						if _, err := io.Copy(io.Discard, diffReader); err != nil {
							err = fmt.Errorf("layer %s: consume any trailer after the EOF marker: %w", layerID, err)
							if isReadWrite {
								report.Layers[layerID] = append(report.Layers[layerID], err)
							} else {
								report.ROLayers[layerID] = append(report.ROLayers[layerID], err)
							}
						}
						wg.Done()
					}(id, reader)
					wg.Wait()
					diff.Close()
					if archiveErr != nil {
						// Reading the diff didn't end as expected
						diffHeadersByLayerMutex.Lock()
						delete(diffHeadersByLayer, id)
						diffHeadersByLayerMutex.Unlock()
						archiveErr = fmt.Errorf("%slayer %s: %w", readWriteDesc, id, archiveErr)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], archiveErr)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], archiveErr)
						}
						return
					}
					if digester.Digest() != layer.UncompressedDigest {
						// The diff digest didn't match.
						diffHeadersByLayerMutex.Lock()
						delete(diffHeadersByLayer, id)
						diffHeadersByLayerMutex.Unlock()
						err := fmt.Errorf("%slayer %s: %w", readWriteDesc, id, ErrLayerIncorrectContentDigest)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], err)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], err)
						}
					}
					if layer.UncompressedSize != -1 && counter.Count != layer.UncompressedSize {
						// We expected the diff to have a specific size, and
						// it didn't match.
						diffHeadersByLayerMutex.Lock()
						delete(diffHeadersByLayer, id)
						diffHeadersByLayerMutex.Unlock()
						err := fmt.Errorf("%slayer %s: read %d bytes instead of %d bytes: %w", readWriteDesc, id, counter.Count, layer.UncompressedSize, ErrLayerIncorrectContentSize)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], err)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], err)
						}
					}
				}()
			}
		}
		// At this point we're out of things that we can be sure will work in read-only
		// stores, so skip the rest for any stores that aren't also read-write stores.
		if !isReadWrite {
			return struct{}{}, false, nil
		}
		// Content and mount checks are also things that we can only be sure will work in
		// read-write stores.
		for i := range layers {
			layer := layers[i]
			id := layer.ID
			// Compare to what we see when we mount the layer and walk the tree, and
			// flag cases where content is in the layer that shouldn't be there.  The
			// tar-split implementation of Diff() won't catch this problem by itself.
			if options.LayerMountable {
				func() {
					// Mount the layer.
					mountPoint, err := s.graphDriver.Get(id, drivers.MountOpts{MountLabel: layer.MountLabel, Options: []string{"ro"}})
					if err != nil {
						err := fmt.Errorf("%slayer %s: %w", readWriteDesc, id, err)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], err)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], err)
						}
						return
					}
					// Unmount the layer when we're done in here.
					defer func() {
						if err := s.graphDriver.Put(id); err != nil {
							err := fmt.Errorf("%slayer %s: %w", readWriteDesc, id, err)
							if isReadWrite {
								report.Layers[id] = append(report.Layers[id], err)
							} else {
								report.ROLayers[id] = append(report.ROLayers[id], err)
							}
							return
						}
					}()
					// If we're not looking at layer contents, or we didn't
					// look at the diff for this layer, we're done here.
					if !options.LayerDigests || layer.UncompressedDigest == "" || !options.LayerContents {
						return
					}
					// Build a list of all of the changes in all of the layers
					// that make up the tree we're looking at.
					diffHeaderSet := [][]*tar.Header{}
					// If we don't know _all_ of the changes that produced this
					// layer, it's not part of an image, so we're done here.
					for layerID := id; layerID != ""; layerID = report.layerParentsByLayerID[layerID] {
						diffHeadersByLayerMutex.Lock()
						layerChanges, haveChanges := diffHeadersByLayer[layerID]
						diffHeadersByLayerMutex.Unlock()
						if !haveChanges {
							return
						}
						// The diff headers for this layer go _before_ those of
						// layers that inherited some of its contents.
						diffHeaderSet = append([][]*tar.Header{layerChanges}, diffHeaderSet...)
					}
					expectedCheckDirectory := newCheckDirectoryDefaults()
					for _, diffHeaders := range diffHeaderSet {
						expectedCheckDirectory.headers(diffHeaders)
					}
					// Scan the directory tree under the mount point.
					var idmap *idtools.IDMappings
					if !s.canUseShifting(layer.UIDMap, layer.GIDMap) {
						// we would have had to chown() layer contents to match ID maps
						idmap = idtools.NewIDMappingsFromMaps(layer.UIDMap, layer.GIDMap)
					}
					actualCheckDirectory, err := newCheckDirectoryFromDirectory(mountPoint)
					if err != nil {
						err := fmt.Errorf("scanning contents of %slayer %s: %w", readWriteDesc, id, err)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], err)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], err)
						}
						return
					}
					// Every departure from our expectations is an error.
					diffs := compareCheckDirectory(expectedCheckDirectory, actualCheckDirectory, idmap, ignore)
					for _, diff := range diffs {
						err := fmt.Errorf("%slayer %s: %s, %w", readWriteDesc, id, diff, ErrLayerContentModified)
						if isReadWrite {
							report.Layers[id] = append(report.Layers[id], err)
						} else {
							report.ROLayers[id] = append(report.ROLayers[id], err)
						}
					}
				}()
			}
		}
		// Check that we don't have any dangling parent layer references.
		for id, parent := range report.layerParentsByLayerID {
			// If this layer doesn't have a parent, no problem.
			if parent == "" {
				continue
			}
			// If we've already seen a layer with this parent ID, skip it.
			if _, checked := referencedLayers[parent]; checked {
				continue
			}
			if _, checked := referencedROLayers[parent]; checked {
				continue
			}
			// We haven't seen a layer with the ID that this layer's record
			// says is its parent's ID.
			err := fmt.Errorf("%slayer %s: %w", readWriteDesc, parent, ErrLayerMissing)
			report.Layers[id] = append(report.Layers[id], err)
		}
		return struct{}{}, false, nil
	}); err != nil {
		return CheckReport{}, err
	}

	// This map will track examined images.  If we have multiple stores, read-only ones can
	// contain copies of images that are also in the read-write store, or the read-write store
	// may contain a duplicate entry that refers to layers in the read-only stores, but when
	// trying to export them, we only look at the first copy of the image.
	examinedImages := make(map[string]struct{})

	// Walk the list of image stores, looking at each image that we didn't see in a
	// previously-visited store.
	if _, _, err := readAllImageStores(s, func(store roImageStore) (struct{}, bool, error) {
		images, err := store.Images()
		if err != nil {
			return struct{}{}, true, err
		}
		isReadWrite := roImageStoreIsReallyReadWrite(store)
		readWriteDesc := ""
		if !isReadWrite {
			readWriteDesc = "read-only "
		}
		// Examine each image in turn.
		for i := range images {
			image := images[i]
			id := image.ID
			// If we've already seen an image with this ID, skip it.
			if _, checked := examinedImages[id]; checked {
				continue
			}
			examinedImages[id] = struct{}{}
			logrus.Debugf("checking %simage %s", readWriteDesc, id)
			if options.ImageData {
				// Check that all of the big data items are present and reading them
				// back gives us the right amount of data.  Even though we record
				// digests that can be used to look them up, we don't know how they
				// were calculated (they're only used as lookup keys), so do not try
				// to check them.
				for _, key := range image.BigDataNames {
					func() {
						data, err := store.BigData(id, key)
						if err != nil {
							if errors.Is(err, os.ErrNotExist) {
								err = fmt.Errorf("%simage %s: data item %q: %w", readWriteDesc, id, key, ErrImageDataMissing)
								if isReadWrite {
									report.Images[id] = append(report.Images[id], err)
								} else {
									report.ROImages[id] = append(report.ROImages[id], err)
								}
								return
							}
							err = fmt.Errorf("%simage %s: data item %q: %w", readWriteDesc, id, key, err)
							if isReadWrite {
								report.Images[id] = append(report.Images[id], err)
							} else {
								report.ROImages[id] = append(report.ROImages[id], err)
							}
							return
						}
						if int64(len(data)) != image.BigDataSizes[key] {
							err = fmt.Errorf("%simage %s: data item %q: %w", readWriteDesc, id, key, ErrImageDataIncorrectSize)
							if isReadWrite {
								report.Images[id] = append(report.Images[id], err)
							} else {
								report.ROImages[id] = append(report.ROImages[id], err)
							}
							return
						}
					}()
				}
			}
			// Walk the layers list for the image.  For every layer that the image uses
			// that has errors, the layer's errors are also the image's errors.
			examinedImageLayers := make(map[string]struct{})
			for _, topLayer := range append([]string{image.TopLayer}, image.MappedTopLayers...) {
				if topLayer == "" {
					continue
				}
				if _, checked := examinedImageLayers[topLayer]; checked {
					continue
				}
				examinedImageLayers[topLayer] = struct{}{}
				for layer := topLayer; layer != ""; layer = report.layerParentsByLayerID[layer] {
					// The referenced layer should have a corresponding entry in
					// one map or the other.
					_, checked := referencedLayers[layer]
					_, checkedRO := referencedROLayers[layer]
					if !checked && !checkedRO {
						err := fmt.Errorf("layer %s: %w", layer, ErrImageLayerMissing)
						err = fmt.Errorf("%simage %s: %w", readWriteDesc, id, err)
						if isReadWrite {
							report.Images[id] = append(report.Images[id], err)
						} else {
							report.ROImages[id] = append(report.ROImages[id], err)
						}
					} else {
						// Count this layer as referenced.  Whether by the
						// image or one of its child layers doesn't matter
						// at this point.
						if _, ok := referencedLayers[layer]; ok {
							referencedLayers[layer] = true
						}
						if _, ok := referencedROLayers[layer]; ok {
							referencedROLayers[layer] = true
						}
					}
					if isReadWrite {
						if len(report.Layers[layer]) > 0 {
							report.Images[id] = append(report.Images[id], report.Layers[layer]...)
						}
						if len(report.ROLayers[layer]) > 0 {
							report.Images[id] = append(report.Images[id], report.ROLayers[layer]...)
						}
					} else {
						if len(report.Layers[layer]) > 0 {
							report.ROImages[id] = append(report.ROImages[id], report.Layers[layer]...)
						}
						if len(report.ROLayers[layer]) > 0 {
							report.ROImages[id] = append(report.ROImages[id], report.ROLayers[layer]...)
						}
					}
				}
			}
		}
		return struct{}{}, false, nil
	}); err != nil {
		return CheckReport{}, err
	}

	// Iterate over each container in turn.
	if _, _, err := readContainerStore(s, func() (struct{}, bool, error) {
		containers, err := s.containerStore.Containers()
		if err != nil {
			return struct{}{}, true, err
		}
		for i := range containers {
			container := containers[i]
			id := container.ID
			logrus.Debugf("checking container %s", id)
			if options.ContainerData {
				// Check that all of the big data items are present and reading them
				// back gives us the right amount of data.
				for _, key := range container.BigDataNames {
					func() {
						data, err := s.containerStore.BigData(id, key)
						if err != nil {
							if errors.Is(err, os.ErrNotExist) {
								err = fmt.Errorf("container %s: data item %q: %w", id, key, ErrContainerDataMissing)
								report.Containers[id] = append(report.Containers[id], err)
								return
							}
							err = fmt.Errorf("container %s: data item %q: %w", id, key, err)
							report.Containers[id] = append(report.Containers[id], err)
							return
						}
						if int64(len(data)) != container.BigDataSizes[key] {
							err = fmt.Errorf("container %s: data item %q: %w", id, key, ErrContainerDataIncorrectSize)
							report.Containers[id] = append(report.Containers[id], err)
							return
						}
					}()
				}
			}
			// Look at the container's base image.  If the image has errors, the image's errors
			// are the container's errors.
			if container.ImageID != "" {
				if _, checked := examinedImages[container.ImageID]; !checked {
					err := fmt.Errorf("image %s: %w", container.ImageID, ErrContainerImageMissing)
					report.Containers[id] = append(report.Containers[id], err)
				}
				if len(report.Images[container.ImageID]) > 0 {
					report.Containers[id] = append(report.Containers[id], report.Images[container.ImageID]...)
				}
				if len(report.ROImages[container.ImageID]) > 0 {
					report.Containers[id] = append(report.Containers[id], report.ROImages[container.ImageID]...)
				}
			}
			// Count the container's layer as referenced.
			if container.LayerID != "" {
				referencedLayers[container.LayerID] = true
			}
		}
		return struct{}{}, false, nil
	}); err != nil {
		return CheckReport{}, err
	}

	// Now go back through all of the layer stores, and flag any layers which don't belong
	// to an image or a container, and has been around longer than we can reasonably expect
	// such a layer to be present before a corresponding image record is added.
	if _, _, err := readAllLayerStores(s, func(store roLayerStore) (struct{}, bool, error) {
		if isReadWrite := roLayerStoreIsReallyReadWrite(store); !isReadWrite {
			return struct{}{}, false, nil
		}
		layers, err := store.Layers()
		if err != nil {
			return struct{}{}, true, err
		}
		for _, layer := range layers {
			maximumAge := defaultMaximumUnreferencedLayerAge
			if options.LayerUnreferencedMaximumAge != nil {
				maximumAge = *options.LayerUnreferencedMaximumAge
			}
			if referenced := referencedLayers[layer.ID]; !referenced {
				if layer.Created.IsZero() || layer.Created.Add(maximumAge).Before(time.Now()) {
					// Either we don't (and never will) know when this layer was
					// created, or it was created far enough in the past that we're
					// reasonably sure it's not part of an image that's being written
					// right now.
					err := fmt.Errorf("layer %s: %w", layer.ID, ErrLayerUnreferenced)
					report.Layers[layer.ID] = append(report.Layers[layer.ID], err)
				}
			}
		}
		return struct{}{}, false, nil
	}); err != nil {
		return CheckReport{}, err
	}

	// If the driver can tell us about which layers it knows about, we should have previously
	// examined all of them.  Any that we didn't are probably just wasted space.
	// Note: if the driver doesn't support enumerating layers, it returns ErrNotSupported.
	if err := s.startUsingGraphDriver(); err != nil {
		return CheckReport{}, err
	}
	defer s.stopUsingGraphDriver()
	layerList, err := s.graphDriver.ListLayers()
	if err != nil && !errors.Is(err, drivers.ErrNotSupported) {
		return CheckReport{}, err
	}
	if !errors.Is(err, drivers.ErrNotSupported) {
		for i, id := range layerList {
			if _, known := referencedLayers[id]; !known {
				err := fmt.Errorf("layer %s: %w", id, ErrLayerUnaccounted)
				report.Layers[id] = append(report.Layers[id], err)
			}
			report.layerOrder[id] = i + 1
		}
	}

	return report, nil
}

func roLayerStoreIsReallyReadWrite(store roLayerStore) bool {
	return store.(*layerStore).lockfile.IsReadWrite()
}

func roImageStoreIsReallyReadWrite(store roImageStore) bool {
	return store.(*imageStore).lockfile.IsReadWrite()
}

// Repair removes items which are themselves damaged, or which depend on items which are damaged.
// Errors are returned if an attempt to delete an item fails.
func (s *store) Repair(report CheckReport, options *RepairOptions) []error {
	if options == nil {
		options = RepairEverything()
	}
	var errs []error
	// Just delete damaged containers.
	if options.RemoveContainers {
		for id := range report.Containers {
			err := s.DeleteContainer(id)
			if err != nil && !errors.Is(err, ErrContainerUnknown) {
				err := fmt.Errorf("deleting container %s: %w", id, err)
				errs = append(errs, err)
			}
		}
	}
	// Now delete damaged images.  Note which layers were removed as part of removing those images.
	deletedLayers := make(map[string]struct{})
	for id := range report.Images {
		layers, err := s.DeleteImage(id, true)
		if err != nil {
			if !errors.Is(err, ErrImageUnknown) && !errors.Is(err, ErrLayerUnknown) {
				err := fmt.Errorf("deleting image %s: %w", id, err)
				errs = append(errs, err)
			}
		} else {
			for _, layer := range layers {
				logrus.Debugf("deleted layer %s", layer)
				deletedLayers[layer] = struct{}{}
			}
			logrus.Debugf("deleted image %s", id)
		}
	}
	// Build a list of the layers that we need to remove, sorted with parents of layers before
	// layers that they are parents of.
	layersToDelete := make([]string, 0, len(report.Layers))
	for id := range report.Layers {
		layersToDelete = append(layersToDelete, id)
	}
	depth := func(id string) int {
		d := 0
		parent := report.layerParentsByLayerID[id]
		for parent != "" {
			d++
			parent = report.layerParentsByLayerID[parent]
		}
		return d
	}
	isUnaccounted := func(errs []error) bool {
		return slices.ContainsFunc(errs, func(err error) bool {
			return errors.Is(err, ErrLayerUnaccounted)
		})
	}
	sort.Slice(layersToDelete, func(i, j int) bool {
		// we've not heard of either of them, so remove them in the order the driver suggested
		if isUnaccounted(report.Layers[layersToDelete[i]]) &&
			isUnaccounted(report.Layers[layersToDelete[j]]) &&
			report.layerOrder[layersToDelete[i]] != 0 && report.layerOrder[layersToDelete[j]] != 0 {
			return report.layerOrder[layersToDelete[i]] < report.layerOrder[layersToDelete[j]]
		}
		// always delete the one we've heard of first
		if isUnaccounted(report.Layers[layersToDelete[i]]) && !isUnaccounted(report.Layers[layersToDelete[j]]) {
			return false
		}
		// always delete the one we've heard of first
		if !isUnaccounted(report.Layers[layersToDelete[i]]) && isUnaccounted(report.Layers[layersToDelete[j]]) {
			return true
		}
		// we've heard of both of them; the one that's on the end of a longer chain goes first
		return depth(layersToDelete[i]) > depth(layersToDelete[j]) // closer-to-a-notional-base layers get removed later
	})
	// Now delete the layers that haven't been removed along with images.
	for _, id := range layersToDelete {
		if _, ok := deletedLayers[id]; ok {
			continue
		}
		for _, reportedErr := range report.Layers[id] {
			var err error
			// If a layer was unaccounted for, remove it at the storage driver level.
			// Otherwise, remove it at the higher level and let the higher level
			// logic worry about telling the storage driver to delete the layer.
			if errors.Is(reportedErr, ErrLayerUnaccounted) {
				if err = s.graphDriver.Remove(id); err != nil {
					err = fmt.Errorf("deleting storage layer %s: %v", id, err)
				} else {
					logrus.Debugf("deleted storage layer %s", id)
				}
			} else {
				var stillMounted bool
				if stillMounted, err = s.Unmount(id, true); err == nil && !stillMounted {
					logrus.Debugf("unmounted layer %s", id)
				} else if err != nil {
					logrus.Debugf("unmounting layer %s: %v", id, err)
				} else {
					logrus.Debugf("layer %s still mounted", id)
				}
				if err = s.DeleteLayer(id); err != nil {
					err = fmt.Errorf("deleting layer %s: %w", id, err)
					logrus.Debugf("deleted layer %s", id)
				}
			}
			if err != nil && !errors.Is(err, ErrLayerUnknown) && !errors.Is(err, ErrNotALayer) && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// compareFileInfo returns a string summarizing what's different between the two checkFileInfos
func compareFileInfo(a, b checkFileInfo, idmap *idtools.IDMappings, ignore checkIgnore) string {
	var comparison []string
	if a.typeflag != b.typeflag && !ignore.filetype {
		comparison = append(comparison, fmt.Sprintf("filetype:%v￫%v", a.typeflag, b.typeflag))
	}
	if idmap != nil && !idmap.Empty() {
		mappedUID, mappedGID, err := idmap.ToContainer(idtools.IDPair{UID: b.uid, GID: b.gid})
		if err != nil {
			return err.Error()
		}
		b.uid, b.gid = mappedUID, mappedGID
	}
	if a.uid != b.uid && !ignore.ownership {
		comparison = append(comparison, fmt.Sprintf("uid:%d￫%d", a.uid, b.uid))
	}
	if a.gid != b.gid && !ignore.ownership {
		comparison = append(comparison, fmt.Sprintf("gid:%d￫%d", a.gid, b.gid))
	}
	if a.size != b.size {
		comparison = append(comparison, fmt.Sprintf("size:%d￫%d", a.size, b.size))
	}
	if (os.ModeType|os.ModePerm)&a.mode != (os.ModeType|os.ModePerm)&b.mode && !ignore.permissions {
		comparison = append(comparison, fmt.Sprintf("mode:%04o￫%04o", a.mode, b.mode))
	}
	if a.mtime != b.mtime && !ignore.timestamps {
		comparison = append(comparison, fmt.Sprintf("mtime:0x%x￫0x%x", a.mtime, b.mtime))
	}
	return strings.Join(comparison, ",")
}

// checkFileInfo is what we care about for files
type checkFileInfo struct {
	typeflag byte
	uid, gid int
	size     int64
	mode     os.FileMode
	mtime    int64 // unix-style whole seconds
}

// checkDirectory is a node in a filesystem record, possibly the top
type checkDirectory struct {
	directory map[string]*checkDirectory // subdirectories
	file      map[string]checkFileInfo   // non-directories
	checkFileInfo
}

// newCheckDirectory creates an empty checkDirectory
func newCheckDirectory(uid, gid int, size int64, mode os.FileMode, mtime int64) *checkDirectory {
	return &checkDirectory{
		directory: make(map[string]*checkDirectory),
		file:      make(map[string]checkFileInfo),
		checkFileInfo: checkFileInfo{
			typeflag: tar.TypeDir,
			uid:      uid,
			gid:      gid,
			size:     size,
			mode:     mode,
			mtime:    mtime,
		},
	}
}

// newCheckDirectoryDefaults creates an empty checkDirectory with hardwired defaults for the UID
// (0), GID (0), size (0) and permissions (0o555)
func newCheckDirectoryDefaults() *checkDirectory {
	return newCheckDirectory(0, 0, 0, 0o555, time.Now().Unix())
}

// newCheckDirectoryFromDirectory creates a checkDirectory for an on-disk directory tree
func newCheckDirectoryFromDirectory(dir string) (*checkDirectory, error) {
	cd := newCheckDirectoryDefaults()
	err := filepath.Walk(dir, func(walkpath string, info os.FileInfo, err error) error {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		rel, err := filepath.Rel(dir, walkpath)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "") // we don't record link targets, so don't bother looking it up
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		cd.header(hdr)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cd, nil
}

// add adds an item to a checkDirectory
func (c *checkDirectory) add(path string, typeflag byte, uid, gid int, size int64, mode os.FileMode, mtime int64) {
	components := strings.Split(path, "/")
	if components[len(components)-1] == "" {
		components = components[:len(components)-1]
	}
	if components[0] == "." {
		components = components[1:]
	}
	if typeflag != tar.TypeReg {
		size = 0
	}
	switch len(components) {
	case 0:
		c.uid = uid
		c.gid = gid
		c.mode = mode
		c.mtime = mtime
		return
	case 1:
		switch typeflag {
		case tar.TypeDir:
			delete(c.file, components[0])
			// directory entries are mergers, not replacements
			if _, present := c.directory[components[0]]; !present {
				c.directory[components[0]] = newCheckDirectory(uid, gid, size, mode, mtime)
			} else {
				c.directory[components[0]].checkFileInfo = checkFileInfo{
					typeflag: tar.TypeDir,
					uid:      uid,
					gid:      gid,
					size:     size,
					mode:     mode,
					mtime:    mtime,
				}
			}
		case tar.TypeXGlobalHeader:
			// ignore, since even though it looks like a valid pathname, it doesn't end
			// up on the filesystem
		default:
			// treat these as TypeReg items
			delete(c.directory, components[0])
			c.file[components[0]] = checkFileInfo{
				typeflag: typeflag,
				uid:      uid,
				gid:      gid,
				size:     size,
				mode:     mode,
				mtime:    mtime,
			}
		}
		return
	}
	subdirectory := c.directory[components[0]]
	if subdirectory == nil {
		subdirectory = newCheckDirectory(uid, gid, size, mode, mtime)
		c.directory[components[0]] = subdirectory
	}
	subdirectory.add(strings.Join(components[1:], "/"), typeflag, uid, gid, size, mode, mtime)
}

// remove removes an item from a checkDirectory
func (c *checkDirectory) remove(path string) {
	parent, rest, ok := strings.Cut(path, "/")
	if !ok {
		delete(c.directory, parent)
		delete(c.file, parent)
		return
	}
	subdirectory := c.directory[parent]
	if subdirectory != nil {
		subdirectory.remove(rest)
	}
}

// header updates a checkDirectory using information from the passed-in header
func (c *checkDirectory) header(hdr *tar.Header) {
	name := path.Clean(hdr.Name)
	dir, base := path.Split(name)
	if file, ok := strings.CutPrefix(base, archive.WhiteoutPrefix); ok {
		if base == archive.WhiteoutOpaqueDir {
			c.remove(path.Clean(dir))
			c.add(path.Clean(dir), tar.TypeDir, hdr.Uid, hdr.Gid, hdr.Size, os.FileMode(hdr.Mode), hdr.ModTime.Unix())
		} else {
			c.remove(path.Join(dir, file))
		}
	} else {
		if hdr.Typeflag == tar.TypeLink {
			// look up the attributes of the target of the hard link
			// n.b. by convention, Linkname is always relative to the
			// root directory of the archive, which is not always the
			// same as being relative to hdr.Name
			directory := c
			for component := range strings.SplitSeq(path.Clean(hdr.Linkname), "/") {
				if component == "." || component == ".." {
					continue
				}
				if subdir, ok := directory.directory[component]; ok {
					directory = subdir
					continue
				}
				if file, ok := directory.file[component]; ok {
					hdr.Typeflag = file.typeflag
					hdr.Uid = file.uid
					hdr.Gid = file.gid
					hdr.Size = file.size
					hdr.Mode = int64(file.mode)
					hdr.ModTime = time.Unix(file.mtime, 0)
				}
				break
			}
		}
		c.add(name, hdr.Typeflag, hdr.Uid, hdr.Gid, hdr.Size, os.FileMode(hdr.Mode), hdr.ModTime.Unix())
	}
}

// headers updates a checkDirectory using information from the passed-in header slice
func (c *checkDirectory) headers(hdrs []*tar.Header) {
	hdrs = slices.Clone(hdrs)
	// sort the headers from the diff to ensure that whiteouts appear
	// before content when they both appear in the same directory, per
	// https://github.com/opencontainers/image-spec/blob/main/layer.md#whiteouts
	// and that hard links appear after other types of entries
	sort.SliceStable(hdrs, func(i, j int) bool {
		if hdrs[i].Typeflag != tar.TypeLink && hdrs[j].Typeflag == tar.TypeLink {
			return true
		}
		if hdrs[i].Typeflag == tar.TypeLink && hdrs[j].Typeflag != tar.TypeLink {
			return false
		}
		idir, ifile := path.Split(hdrs[i].Name)
		jdir, jfile := path.Split(hdrs[j].Name)
		if idir != jdir {
			return hdrs[i].Name < hdrs[j].Name
		}
		if ifile == archive.WhiteoutOpaqueDir {
			return true
		}
		if strings.HasPrefix(ifile, archive.WhiteoutPrefix) && !strings.HasPrefix(jfile, archive.WhiteoutPrefix) {
			return true
		}
		return false
	})
	for _, hdr := range hdrs {
		c.header(hdr)
	}
}

// names provides a sorted list of the path names in the directory tree
func (c *checkDirectory) names() []string {
	names := make([]string, 0, len(c.file)+len(c.directory))
	for name := range c.file {
		names = append(names, name)
	}
	for name, subdirectory := range c.directory {
		names = append(names, name+"/")
		for _, subname := range subdirectory.names() {
			names = append(names, name+"/"+subname)
		}
	}
	return names
}

// compareCheckSubdirectory walks two subdirectory trees and returns a list of differences
func compareCheckSubdirectory(path string, a, b *checkDirectory, idmap *idtools.IDMappings, ignore checkIgnore) []string {
	var diff []string
	if a == nil {
		a = newCheckDirectoryDefaults()
	}
	if b == nil {
		b = newCheckDirectoryDefaults()
	}
	for aname, adir := range a.directory {
		if bdir, present := b.directory[aname]; !present {
			// directory was removed
			diff = append(diff, "-"+path+"/"+aname+"/")
			diff = append(diff, compareCheckSubdirectory(path+"/"+aname, adir, nil, idmap, ignore)...)
		} else {
			// directory is in both trees; descend
			if attributes := compareFileInfo(adir.checkFileInfo, bdir.checkFileInfo, idmap, ignore); attributes != "" {
				diff = append(diff, path+"/"+aname+"("+attributes+")")
			}
			diff = append(diff, compareCheckSubdirectory(path+"/"+aname, adir, bdir, idmap, ignore)...)
		}
	}
	for bname, bdir := range b.directory {
		if _, present := a.directory[bname]; !present {
			// directory added
			diff = append(diff, "+"+path+"/"+bname+"/")
			diff = append(diff, compareCheckSubdirectory(path+"/"+bname, nil, bdir, idmap, ignore)...)
		}
	}
	for aname, afile := range a.file {
		if bfile, present := b.file[aname]; !present {
			// non-directory removed or replaced
			diff = append(diff, "-"+path+"/"+aname)
		} else {
			// item is in both trees; compare
			if attributes := compareFileInfo(afile, bfile, idmap, ignore); attributes != "" {
				diff = append(diff, path+"/"+aname+"("+attributes+")")
			}
		}
	}
	for bname := range b.file {
		filetype, present := a.file[bname]
		if !present {
			// non-directory added or replaced with something else
			diff = append(diff, "+"+path+"/"+bname)
			continue
		}
		if attributes := compareFileInfo(filetype, b.file[bname], idmap, ignore); attributes != "" {
			// non-directory replaced with non-directory
			diff = append(diff, "+"+path+"/"+bname+"("+attributes+")")
		}
	}
	return diff
}

// compareCheckDirectory walks two directory trees and returns a sorted list of differences
func compareCheckDirectory(a, b *checkDirectory, idmap *idtools.IDMappings, ignore checkIgnore) []string {
	diff := compareCheckSubdirectory("", a, b, idmap, ignore)
	sort.Slice(diff, func(i, j int) bool {
		if strings.Compare(diff[i][1:], diff[j][1:]) < 0 {
			return true
		}
		if diff[i][0] == '-' {
			return true
		}
		return false
	})
	return diff
}
