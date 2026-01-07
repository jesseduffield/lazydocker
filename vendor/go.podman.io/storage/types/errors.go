package types

import (
	"errors"
)

var (
	// ErrContainerUnknown indicates that there was no container with the specified name or ID.
	ErrContainerUnknown = errors.New("container not known")
	// ErrDigestUnknown indicates that we were unable to compute the digest of a specified item.
	ErrDigestUnknown = errors.New("could not compute digest of item")
	// ErrDuplicateID indicates that an ID which is to be assigned to a new item is already being used.
	ErrDuplicateID = errors.New("that ID is already in use")
	// ErrDuplicateImageNames indicates that the read-only store uses the same name for multiple images.
	ErrDuplicateImageNames = errors.New("read-only image store assigns the same name to multiple images")
	// ErrDuplicateLayerNames indicates that the read-only store uses the same name for multiple layers.
	ErrDuplicateLayerNames = errors.New("read-only layer store assigns the same name to multiple layers")
	// ErrDuplicateName indicates that a name which is to be assigned to a new item is already being used.
	ErrDuplicateName = errors.New("that name is already in use")
	// ErrImageUnknown indicates that there was no image with the specified name or ID.
	ErrImageUnknown = errors.New("image not known")
	// ErrImageUsedByContainer is returned when the caller attempts to delete an image that is a container's image.
	ErrImageUsedByContainer = errors.New("image is in use by a container")
	// ErrIncompleteOptions is returned when the caller attempts to initialize a Store without providing required information.
	ErrIncompleteOptions = errors.New("missing necessary StoreOptions")
	// ErrInvalidBigDataName indicates that the name for a big data item is not acceptable; it may be empty.
	ErrInvalidBigDataName = errors.New("not a valid name for a big data item")
	// ErrLayerHasChildren is returned when the caller attempts to delete a layer that has children.
	ErrLayerHasChildren = errors.New("layer has children")
	// ErrLayerNotMounted is returned when the requested information can only be computed for a mounted layer, and the layer is not mounted.
	ErrLayerNotMounted = errors.New("layer is not mounted")
	// ErrLayerUnknown indicates that there was no layer with the specified name or ID.
	ErrLayerUnknown = errors.New("layer not known")
	// ErrLayerUsedByContainer is returned when the caller attempts to delete a layer that is a container's layer.
	ErrLayerUsedByContainer = errors.New("layer is in use by a container")
	// ErrLayerUsedByImage is returned when the caller attempts to delete a layer that is an image's top layer.
	ErrLayerUsedByImage = errors.New("layer is in use by an image")
	// ErrLoadError indicates that there was an initialization error.
	ErrLoadError = errors.New("error loading storage metadata")
	// ErrNotAContainer is returned when the caller attempts to delete a container that isn't a container.
	ErrNotAContainer = errors.New("identifier is not a container")
	// ErrNotALayer is returned when the caller attempts to delete a layer that isn't a layer.
	ErrNotALayer = errors.New("identifier is not a layer")
	// ErrNotAnID is returned when the caller attempts to read or write metadata from an item that doesn't exist.
	ErrNotAnID = errors.New("identifier is not a layer, image, or container")
	// ErrNotAnImage is returned when the caller attempts to delete an image that isn't an image.
	ErrNotAnImage = errors.New("identifier is not an image")
	// ErrParentIsContainer is returned when a caller attempts to create a layer as a child of a container's layer.
	ErrParentIsContainer = errors.New("would-be parent layer is a container")
	// ErrParentUnknown indicates that we didn't record the ID of the parent of the specified layer.
	ErrParentUnknown = errors.New("parent of layer not known")
	// ErrSizeUnknown is returned when the caller asks for the size of a big data item, but the Store couldn't determine the answer.
	ErrSizeUnknown = errors.New("size is not known")
	// ErrStoreIsReadOnly is returned when the caller makes a call to a read-only store that would require modifying its contents.
	ErrStoreIsReadOnly = errors.New("called a write method on a read-only store")
	// ErrNotSupported is returned when the requested functionality is not supported.
	ErrNotSupported = errors.New("not supported")
	// ErrInvalidMappings is returned when the specified mappings are invalid.
	ErrInvalidMappings = errors.New("invalid mappings specified")
	// ErrNoAvailableIDs is returned when there are not enough unused IDS within the user namespace.
	ErrNoAvailableIDs = errors.New("not enough unused IDs in user namespace")

	// ErrLayerUnaccounted describes a layer that is present in the lower-level storage driver,
	// but which is not known to or managed by the higher-level driver-agnostic logic.
	ErrLayerUnaccounted = errors.New("layer in lower level storage driver not accounted for")
	// ErrLayerUnreferenced describes a layer which is not used by any image or container.
	ErrLayerUnreferenced = errors.New("layer not referenced by any images or containers")
	// ErrLayerIncorrectContentDigest describes a layer for which the contents of one or more
	// files which were added in the layer appear to have changed.  It may instead look like an
	// unnamed "file integrity checksum failed" error.
	ErrLayerIncorrectContentDigest = errors.New("layer content incorrect digest")
	// ErrLayerIncorrectContentSize describes a layer for which regenerating the diff that was
	// used to populate the layer produced a diff of a different size.  We check the digest
	// first, so it's highly unlikely you'll ever see this error.
	ErrLayerIncorrectContentSize = errors.New("layer content incorrect size")
	// ErrLayerContentModified describes a layer which contains contents which should not be
	// there, or for which ownership/permissions/dates have been changed.
	ErrLayerContentModified = errors.New("layer content modified")
	// ErrLayerDataMissing describes a layer which is missing a big data item.
	ErrLayerDataMissing = errors.New("layer data item is missing")
	// ErrLayerMissing describes a layer which is the missing parent of a layer.
	ErrLayerMissing = errors.New("layer is missing")
	// ErrImageLayerMissing describes an image which claims to have a layer that we don't know
	// about.
	ErrImageLayerMissing = errors.New("image layer is missing")
	// ErrImageDataMissing describes an image which is missing a big data item.
	ErrImageDataMissing = errors.New("image data item is missing")
	// ErrImageDataIncorrectSize describes an image which has a big data item which looks like
	// its size has changed, likely because it's been modified somehow.
	ErrImageDataIncorrectSize = errors.New("image data item has incorrect size")
	// ErrContainerImageMissing describes a container which claims to be based on an image that
	// we don't know about.
	ErrContainerImageMissing = errors.New("image missing")
	// ErrContainerDataMissing describes a container which is missing a big data item.
	ErrContainerDataMissing = errors.New("container data item is missing")
	// ErrContainerDataIncorrectSize describes a container which has a big data item which looks
	// like its size has changed, likely because it's been modified somehow.
	ErrContainerDataIncorrectSize = errors.New("container data item has incorrect size")
)
