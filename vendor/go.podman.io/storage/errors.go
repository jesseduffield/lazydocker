package storage

import (
	"errors"

	"go.podman.io/storage/types"
)

var (
	// ErrContainerUnknown indicates that there was no container with the specified name or ID.
	ErrContainerUnknown = types.ErrContainerUnknown
	// ErrDigestUnknown indicates that we were unable to compute the digest of a specified item.
	ErrDigestUnknown = types.ErrDigestUnknown
	// ErrDuplicateID indicates that an ID which is to be assigned to a new item is already being used.
	ErrDuplicateID = types.ErrDuplicateID
	// ErrDuplicateImageNames indicates that the read-only store uses the same name for multiple images.
	ErrDuplicateImageNames = types.ErrDuplicateImageNames
	// ErrDuplicateLayerNames indicates that the read-only store uses the same name for multiple layers.
	ErrDuplicateLayerNames = types.ErrDuplicateLayerNames
	// ErrDuplicateName indicates that a name which is to be assigned to a new item is already being used.
	ErrDuplicateName = types.ErrDuplicateName
	// ErrImageUnknown indicates that there was no image with the specified name or ID.
	ErrImageUnknown = types.ErrImageUnknown
	// ErrImageUsedByContainer is returned when the caller attempts to delete an image that is a container's image.
	ErrImageUsedByContainer = types.ErrImageUsedByContainer
	// ErrIncompleteOptions is returned when the caller attempts to initialize a Store without providing required information.
	ErrIncompleteOptions = types.ErrIncompleteOptions
	// ErrInvalidBigDataName indicates that the name for a big data item is not acceptable; it may be empty.
	ErrInvalidBigDataName = types.ErrInvalidBigDataName
	// ErrLayerHasChildren is returned when the caller attempts to delete a layer that has children.
	ErrLayerHasChildren = types.ErrLayerHasChildren
	// ErrLayerNotMounted is returned when the requested information can only be computed for a mounted layer, and the layer is not mounted.
	ErrLayerNotMounted = types.ErrLayerNotMounted
	// ErrLayerUnknown indicates that there was no layer with the specified name or ID.
	ErrLayerUnknown = types.ErrLayerUnknown
	// ErrLayerUsedByContainer is returned when the caller attempts to delete a layer that is a container's layer.
	ErrLayerUsedByContainer = types.ErrLayerUsedByContainer
	// ErrLayerUsedByImage is returned when the caller attempts to delete a layer that is an image's top layer.
	ErrLayerUsedByImage = types.ErrLayerUsedByImage
	// ErrLoadError indicates that there was an initialization error.
	ErrLoadError = types.ErrLoadError
	// ErrNotAContainer is returned when the caller attempts to delete a container that isn't a container.
	ErrNotAContainer = types.ErrNotAContainer
	// ErrNotALayer is returned when the caller attempts to delete a layer that isn't a layer.
	ErrNotALayer = types.ErrNotALayer
	// ErrNotAnID is returned when the caller attempts to read or write metadata from an item that doesn't exist.
	ErrNotAnID = types.ErrNotAnID
	// ErrNotAnImage is returned when the caller attempts to delete an image that isn't an image.
	ErrNotAnImage = types.ErrNotAnImage
	// ErrParentIsContainer is returned when a caller attempts to create a layer as a child of a container's layer.
	ErrParentIsContainer = types.ErrParentIsContainer
	// ErrParentUnknown indicates that we didn't record the ID of the parent of the specified layer.
	ErrParentUnknown = types.ErrParentUnknown
	// ErrSizeUnknown is returned when the caller asks for the size of a big data item, but the Store couldn't determine the answer.
	ErrSizeUnknown = types.ErrSizeUnknown
	// ErrStoreIsReadOnly is returned when the caller makes a call to a read-only store that would require modifying its contents.
	ErrStoreIsReadOnly = types.ErrStoreIsReadOnly
	// ErrNotSupported is returned when the requested functionality is not supported.
	ErrNotSupported = types.ErrNotSupported
	// ErrInvalidMappings is returned when the specified mappings are invalid.
	ErrInvalidMappings = types.ErrInvalidMappings
	// ErrInvalidNameOperation is returned when updateName is called with invalid operation.
	// Internal error
	errInvalidUpdateNameOperation = errors.New("invalid update name operation")
)
