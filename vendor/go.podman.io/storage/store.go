package storage

import (
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	// register all of the built-in drivers
	_ "go.podman.io/storage/drivers/register"
	"golang.org/x/sync/errgroup"

	digest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	drivers "go.podman.io/storage/drivers"
	"go.podman.io/storage/internal/dedup"
	"go.podman.io/storage/internal/tempdir"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/directory"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/parsers"
	"go.podman.io/storage/pkg/stringutils"
	"go.podman.io/storage/pkg/system"
	"go.podman.io/storage/types"
)

type updateNameOperation int

const (
	setNames updateNameOperation = iota
	addNames
	removeNames
)

const (
	volatileFlag     = "Volatile"
	mountLabelFlag   = "MountLabel"
	processLabelFlag = "ProcessLabel"
	mountOptsFlag    = "MountOpts"
)

var (
	stores     []*store
	storesLock sync.Mutex
)

// roMetadataStore wraps a method for reading metadata associated with an ID.
type roMetadataStore interface {
	// Metadata reads metadata associated with an item with the specified ID.
	Metadata(id string) (string, error)
}

// rwMetadataStore wraps a method for setting metadata associated with an ID.
type rwMetadataStore interface {
	// SetMetadata updates the metadata associated with the item with the specified ID.
	SetMetadata(id, metadata string) error
}

// metadataStore wraps up methods for getting and setting metadata associated with IDs.
type metadataStore interface {
	roMetadataStore
	rwMetadataStore
}

// ApplyStagedLayerOptions contains options to pass to ApplyStagedLayer
type ApplyStagedLayerOptions struct {
	ID           string        // Mandatory
	ParentLayer  string        // Optional
	Names        []string      // Optional
	MountLabel   string        // Optional
	Writeable    bool          // Optional
	LayerOptions *LayerOptions // Optional

	DiffOutput  *drivers.DriverWithDifferOutput  // Mandatory
	DiffOptions *drivers.ApplyDiffWithDifferOpts // Mandatory
}

// MultiListOptions contains options to pass to MultiList
type MultiListOptions struct {
	Images     bool // if true, Images will be listed in the result
	Layers     bool // if true, layers will be listed in the result
	Containers bool // if true, containers will be listed in the result
}

// MultiListResult contains slices of Images, Layers or Containers listed by MultiList method
type MultiListResult struct {
	Images     []Image
	Layers     []Layer
	Containers []Container
}

// An roBigDataStore wraps up the read-only big-data related methods of the
// various types of file-based lookaside stores that we implement.
type roBigDataStore interface {
	// BigData retrieves a (potentially large) piece of data associated with
	// this ID, if it has previously been set.
	BigData(id, key string) ([]byte, error)

	// BigDataSize retrieves the size of a (potentially large) piece of
	// data associated with this ID, if it has previously been set.
	BigDataSize(id, key string) (int64, error)

	// BigDataDigest retrieves the digest of a (potentially large) piece of
	// data associated with this ID, if it has previously been set.
	BigDataDigest(id, key string) (digest.Digest, error)

	// BigDataNames() returns a list of the names of previously-stored pieces of
	// data.
	BigDataNames(id string) ([]string, error)
}

// A rwImageBigDataStore wraps up how we store big-data associated with images.
type rwImageBigDataStore interface {
	// SetBigData stores a (potentially large) piece of data associated
	// with this ID.
	// Pass github.com/containers/image/manifest.Digest as digestManifest
	// to allow ByDigest to find images by their correct digests.
	SetBigData(id, key string, data []byte, digestManifest func([]byte) (digest.Digest, error)) error
}

// A containerBigDataStore wraps up how we store big-data associated with containers.
type containerBigDataStore interface {
	roBigDataStore
	// SetBigData stores a (potentially large) piece of data associated
	// with this ID.
	SetBigData(id, key string, data []byte) error
}

// A roLayerBigDataStore wraps up how we store RO big-data associated with layers.
type roLayerBigDataStore interface {
	// SetBigData stores a (potentially large) piece of data associated
	// with this ID.
	BigData(id, key string) (io.ReadCloser, error)

	// BigDataNames() returns a list of the names of previously-stored pieces of
	// data.
	BigDataNames(id string) ([]string, error)
}

// A rwLayerBigDataStore wraps up how we store big-data associated with layers.
type rwLayerBigDataStore interface {
	// SetBigData stores a (potentially large) piece of data associated
	// with this ID.
	SetBigData(id, key string, data io.Reader) error
}

// A flaggableStore can have flags set and cleared on items which it manages.
type flaggableStore interface {
	// ClearFlag removes a named flag from an item in the store.
	ClearFlag(id string, flag string) error

	// SetFlag sets a named flag and its value on an item in the store.
	SetFlag(id string, flag string, value any) error
}

type StoreOptions = types.StoreOptions

type DedupHashMethod = dedup.DedupHashMethod

const (
	DedupHashInvalid  = dedup.DedupHashInvalid
	DedupHashCRC      = dedup.DedupHashCRC
	DedupHashFileSize = dedup.DedupHashFileSize
	DedupHashSHA256   = dedup.DedupHashSHA256
)

type (
	DedupOptions = dedup.DedupOptions
	DedupResult  = dedup.DedupResult
)

// DedupArgs is used to pass arguments to the Dedup command.
type DedupArgs struct {
	// Options that are passed directly to the internal/dedup.DedupDirs function.
	Options DedupOptions
}

// Store wraps up the various types of file-based stores that we use into a
// singleton object that initializes and manages them all together.
type Store interface {
	// RunRoot, GraphRoot, GraphDriverName, and GraphOptions retrieve
	// settings that were passed to GetStore() when the object was created.
	RunRoot() string
	GraphRoot() string
	ImageStore() string
	TransientStore() bool
	GraphDriverName() string
	GraphOptions() []string
	PullOptions() map[string]string
	UIDMap() []idtools.IDMap
	GIDMap() []idtools.IDMap

	// GraphDriver obtains and returns a handle to the graph Driver object used
	// by the Store.
	GraphDriver() (drivers.Driver, error)

	// CreateLayer creates a new layer in the underlying storage driver,
	// optionally having the specified ID (one will be assigned if none is
	// specified), with the specified layer (or no layer) as its parent,
	// and with optional names.  (The writeable flag is ignored.)
	CreateLayer(id, parent string, names []string, mountLabel string, writeable bool, options *LayerOptions) (*Layer, error)

	// PutLayer combines the functions of CreateLayer and ApplyDiff,
	// marking the layer for automatic removal if applying the diff fails
	// for any reason.
	//
	// Note that we do some of this work in a child process.  The calling
	// process's main() function needs to import our pkg/reexec package and
	// should begin with something like this in order to allow us to
	// properly start that child process:
	//   if reexec.Init() {
	//       return
	//   }
	PutLayer(id, parent string, names []string, mountLabel string, writeable bool, options *LayerOptions, diff io.Reader) (*Layer, int64, error)

	// CreateImage creates a new image, optionally with the specified ID
	// (one will be assigned if none is specified), with optional names,
	// referring to a specified image, and with optional metadata.  An
	// image is a record which associates the ID of a layer with a
	// additional bookkeeping information which the library stores for the
	// convenience of its caller.
	CreateImage(id string, names []string, layer, metadata string, options *ImageOptions) (*Image, error)

	// CreateContainer creates a new container, optionally with the
	// specified ID (one will be assigned if none is specified), with
	// optional names, using the specified image's top layer as the basis
	// for the container's layer, and assigning the specified ID to that
	// layer (one will be created if none is specified).  A container is a
	// layer which is associated with additional bookkeeping information
	// which the library stores for the convenience of its caller.
	CreateContainer(id string, names []string, image, layer, metadata string, options *ContainerOptions) (*Container, error)

	// Metadata retrieves the metadata which is associated with a layer,
	// image, or container (whichever the passed-in ID refers to).
	Metadata(id string) (string, error)

	// SetMetadata updates the metadata which is associated with a layer,
	// image, or container (whichever the passed-in ID refers to) to match
	// the specified value.  The metadata value can be retrieved at any
	// time using Metadata, or using Layer, Image, or Container and reading
	// the object directly.
	SetMetadata(id, metadata string) error

	// Exists checks if there is a layer, image, or container which has the
	// passed-in ID or name.
	Exists(id string) bool

	// Status asks for a status report, in the form of key-value pairs,
	// from the underlying storage driver.  The contents vary from driver
	// to driver.
	Status() ([][2]string, error)

	// Delete removes the layer, image, or container which has the
	// passed-in ID or name.  Note that no safety checks are performed, so
	// this can leave images with references to layers which do not exist,
	// and layers with references to parents which no longer exist.
	Delete(id string) error

	// DeleteLayer attempts to remove the specified layer.  If the layer is the
	// parent of any other layer, or is referred to by any images, it will return
	// an error.
	DeleteLayer(id string) error

	// DeleteImage removes the specified image if it is not referred to by
	// any containers.  If its top layer is then no longer referred to by
	// any other images and is not the parent of any other layers, its top
	// layer will be removed.  If that layer's parent is no longer referred
	// to by any other images and is not the parent of any other layers,
	// then it, too, will be removed.  This procedure will be repeated
	// until a layer which should not be removed, or the base layer, is
	// reached, at which point the list of removed layers is returned.  If
	// the commit argument is false, the image and layers are not removed,
	// but the list of layers which would be removed is still returned.
	DeleteImage(id string, commit bool) (layers []string, err error)

	// DeleteContainer removes the specified container and its layer.  If
	// there is no matching container, or if the container exists but its
	// layer does not, an error will be returned.
	DeleteContainer(id string) error

	// Wipe removes all known layers, images, and containers.
	Wipe() error

	// MountImage mounts an image to temp directory and returns the mount point.
	// MountImage allows caller to mount an image. Images will always
	// be mounted read/only
	MountImage(id string, mountOptions []string, mountLabel string) (string, error)

	// Unmount attempts to unmount an image, given an ID.
	// Returns whether or not the layer is still mounted.
	// WARNING: The return value may already be obsolete by the time it is available
	// to the caller, so it can be used for heuristic sanity checks at best. It should almost always be ignored.
	UnmountImage(id string, force bool) (bool, error)

	// Mount attempts to mount a layer, image, or container for access, and
	// returns the pathname if it succeeds.
	// Note if the mountLabel == "", the default label for the container
	// will be used.
	//
	// Note that we do some of this work in a child process.  The calling
	// process's main() function needs to import our pkg/reexec package and
	// should begin with something like this in order to allow us to
	// properly start that child process:
	//   if reexec.Init() {
	//       return
	//   }
	Mount(id, mountLabel string) (string, error)

	// Unmount attempts to unmount a layer, image, or container, given an ID, a
	// name, or a mount path. Returns whether or not the layer is still mounted.
	// WARNING: The return value may already be obsolete by the time it is available
	// to the caller, so it can be used for heuristic sanity checks at best. It should almost always be ignored.
	Unmount(id string, force bool) (bool, error)

	// Mounted returns number of times the layer has been mounted.
	//
	// WARNING: This value might already be obsolete by the time it is returned;
	// In situations where concurrent mount/unmount attempts can happen, this field
	// should not be used for any decisions, maybe apart from heuristic user warnings.
	Mounted(id string) (int, error)

	// Changes returns a summary of the changes which would need to be made
	// to one layer to make its contents the same as a second layer.  If
	// the first layer is not specified, the second layer's parent is
	// assumed.  Each Change structure contains a Path relative to the
	// layer's root directory, and a Kind which is either ChangeAdd,
	// ChangeModify, or ChangeDelete.
	Changes(from, to string) ([]archive.Change, error)

	// DiffSize returns a count of the size of the tarstream which would
	// specify the changes returned by Changes.
	DiffSize(from, to string) (int64, error)

	// Diff returns the tarstream which would specify the changes returned
	// by Changes.  If options are passed in, they can override default
	// behaviors.
	Diff(from, to string, options *DiffOptions) (io.ReadCloser, error)

	// ApplyDiff applies a tarstream to a layer.  Information about the
	// tarstream is cached with the layer.  Typically, a layer which is
	// populated using a tarstream will be expected to not be modified in
	// any other way, either before or after the diff is applied.
	//
	// Note that we do some of this work in a child process.  The calling
	// process's main() function needs to import our pkg/reexec package and
	// should begin with something like this in order to allow us to
	// properly start that child process:
	//   if reexec.Init() {
	//       return
	//   }
	ApplyDiff(to string, diff io.Reader) (int64, error)

	// PrepareStagedLayer applies a diff to a layer.
	// It is the caller responsibility to clean the staging directory if it is not
	// successfully applied with ApplyStagedLayer.
	// The caller must ensure [Store.ApplyStagedLayer] or [Store.CleanupStagedLayer] is called eventually
	// with the returned [drivers.DriverWithDifferOutput] object.
	PrepareStagedLayer(options *drivers.ApplyDiffWithDifferOpts, differ drivers.Differ) (*drivers.DriverWithDifferOutput, error)

	// ApplyStagedLayer combines the functions of creating a layer and using the staging
	// directory to populate it.
	// It marks the layer for automatic removal if applying the diff fails for any reason.
	ApplyStagedLayer(args ApplyStagedLayerOptions) (*Layer, error)

	// CleanupStagedLayer cleanups the staging directory.  It can be used to cleanup the staging directory on errors
	CleanupStagedLayer(diffOutput *drivers.DriverWithDifferOutput) error

	// DifferTarget gets the path to the differ target.
	DifferTarget(id string) (string, error)

	// LayersByCompressedDigest returns a slice of the layers with the
	// specified compressed digest value recorded for them.
	LayersByCompressedDigest(d digest.Digest) ([]Layer, error)

	// LayersByUncompressedDigest returns a slice of the layers with the
	// specified uncompressed digest value recorded for them.
	LayersByUncompressedDigest(d digest.Digest) ([]Layer, error)

	// LayersByTOCDigest returns a slice of the layers with the
	// specified TOC digest value recorded for them.
	LayersByTOCDigest(d digest.Digest) ([]Layer, error)

	// LayerSize returns a cached approximation of the layer's size, or -1
	// if we don't have a value on hand.
	LayerSize(id string) (int64, error)

	// LayerParentOwners returns the UIDs and GIDs of owners of parents of
	// the layer's mountpoint for which the layer's UID and GID maps (if
	// any are defined) don't contain corresponding IDs.
	LayerParentOwners(id string) ([]int, []int, error)

	// Layers returns a list of the currently known layers.
	Layers() ([]Layer, error)

	// Images returns a list of the currently known images.
	Images() ([]Image, error)

	// Containers returns a list of the currently known containers.
	Containers() ([]Container, error)

	// Names returns the list of names for a layer, image, or container.
	Names(id string) ([]string, error)

	// Free removes the store from the list of stores
	Free()

	// SetNames changes the list of names for a layer, image, or container.
	// Duplicate names are removed from the list automatically.
	// Deprecated: Prone to race conditions, suggested alternatives are `AddNames` and `RemoveNames`.
	SetNames(id string, names []string) error

	// AddNames adds the list of names for a layer, image, or container.
	// Duplicate names are removed from the list automatically.
	AddNames(id string, names []string) error

	// RemoveNames removes the list of names for a layer, image, or container.
	// Duplicate names are removed from the list automatically.
	RemoveNames(id string, names []string) error

	// ListImageBigData retrieves a list of the (possibly large) chunks of
	// named data associated with an image.
	ListImageBigData(id string) ([]string, error)

	// ImageBigData retrieves a (possibly large) chunk of named data
	// associated with an image.
	ImageBigData(id, key string) ([]byte, error)

	// ImageBigDataSize retrieves the size of a (possibly large) chunk
	// of named data associated with an image.
	ImageBigDataSize(id, key string) (int64, error)

	// ImageBigDataDigest retrieves the digest of a (possibly large) chunk
	// of named data associated with an image.
	ImageBigDataDigest(id, key string) (digest.Digest, error)

	// SetImageBigData stores a (possibly large) chunk of named data
	// associated with an image.  Pass
	// github.com/containers/image/manifest.Digest as digestManifest to
	// allow ImagesByDigest to find images by their correct digests.
	SetImageBigData(id, key string, data []byte, digestManifest func([]byte) (digest.Digest, error)) error

	// ImageDirectory returns a path of a directory which the caller can
	// use to store data, specific to the image, which the library does not
	// directly manage.  The directory will be deleted when the image is
	// deleted.
	ImageDirectory(id string) (string, error)

	// ImageRunDirectory returns a path of a directory which the caller can
	// use to store data, specific to the image, which the library does not
	// directly manage.  The directory will be deleted when the host system
	// is restarted.
	ImageRunDirectory(id string) (string, error)

	// ListLayerBigData retrieves a list of the (possibly large) chunks of
	// named data associated with a layer.
	ListLayerBigData(id string) ([]string, error)

	// LayerBigData retrieves a (possibly large) chunk of named data
	// associated with a layer.
	LayerBigData(id, key string) (io.ReadCloser, error)

	// SetLayerBigData stores a (possibly large) chunk of named data
	// associated with a layer.
	SetLayerBigData(id, key string, data io.Reader) error

	// ImageSize computes the size of the image's layers and ancillary data.
	ImageSize(id string) (int64, error)

	// ListContainerBigData retrieves a list of the (possibly large) chunks of
	// named data associated with a container.
	ListContainerBigData(id string) ([]string, error)

	// ContainerBigData retrieves a (possibly large) chunk of named data
	// associated with a container.
	ContainerBigData(id, key string) ([]byte, error)

	// ContainerBigDataSize retrieves the size of a (possibly large)
	// chunk of named data associated with a container.
	ContainerBigDataSize(id, key string) (int64, error)

	// ContainerBigDataDigest retrieves the digest of a (possibly large)
	// chunk of named data associated with a container.
	ContainerBigDataDigest(id, key string) (digest.Digest, error)

	// SetContainerBigData stores a (possibly large) chunk of named data
	// associated with a container.
	SetContainerBigData(id, key string, data []byte) error

	// ContainerSize computes the size of the container's layer and ancillary
	// data.  Warning:  this is a potentially expensive operation.
	ContainerSize(id string) (int64, error)

	// Layer returns a specific layer.
	Layer(id string) (*Layer, error)

	// Image returns a specific image.
	Image(id string) (*Image, error)

	// ImagesByTopLayer returns a list of images which reference the specified
	// layer as their top layer.  They will have different IDs and names
	// and may have different metadata, big data items, and flags.
	ImagesByTopLayer(id string) ([]*Image, error)

	// ImagesByDigest returns a list of images which contain a big data item
	// named ImageDigestBigDataKey whose contents have the specified digest.
	ImagesByDigest(d digest.Digest) ([]*Image, error)

	// Container returns a specific container.
	Container(id string) (*Container, error)

	// ContainerByLayer returns a specific container based on its layer ID or
	// name.
	ContainerByLayer(id string) (*Container, error)

	// ContainerDirectory returns a path of a directory which the caller
	// can use to store data, specific to the container, which the library
	// does not directly manage.  The directory will be deleted when the
	// container is deleted.
	ContainerDirectory(id string) (string, error)

	// SetContainerDirectoryFile is a convenience function which stores
	// a piece of data in the specified file relative to the container's
	// directory.
	SetContainerDirectoryFile(id, file string, data []byte) error

	// FromContainerDirectory is a convenience function which reads
	// the contents of the specified file relative to the container's
	// directory.
	FromContainerDirectory(id, file string) ([]byte, error)

	// ContainerRunDirectory returns a path of a directory which the
	// caller can use to store data, specific to the container, which the
	// library does not directly manage.  The directory will be deleted
	// when the host system is restarted.
	ContainerRunDirectory(id string) (string, error)

	// SetContainerRunDirectoryFile is a convenience function which stores
	// a piece of data in the specified file relative to the container's
	// run directory.
	SetContainerRunDirectoryFile(id, file string, data []byte) error

	// FromContainerRunDirectory is a convenience function which reads
	// the contents of the specified file relative to the container's run
	// directory.
	FromContainerRunDirectory(id, file string) ([]byte, error)

	// ContainerParentOwners returns the UIDs and GIDs of owners of parents
	// of the container's layer's mountpoint for which the layer's UID and
	// GID maps (if any are defined) don't contain corresponding IDs.
	ContainerParentOwners(id string) ([]int, []int, error)

	// Lookup returns the ID of a layer, image, or container with the specified
	// name or ID.
	Lookup(name string) (string, error)

	// Shutdown attempts to free any kernel resources which are being used
	// by the underlying driver.  If "force" is true, any mounted (i.e., in
	// use) layers are unmounted beforehand.  If "force" is not true, then
	// layers being in use is considered to be an error condition.  A list
	// of still-mounted layers is returned along with possible errors.
	Shutdown(force bool) (layers []string, err error)

	// Version returns version information, in the form of key-value pairs, from
	// the storage package.
	Version() ([][2]string, error)

	// GetDigestLock returns digest-specific Locker.
	GetDigestLock(digest.Digest) (Locker, error)

	// LayerFromAdditionalLayerStore searches the additional layer store and returns an object
	// which can create a layer with the specified TOC digest associated with the specified image
	// reference. Note that this hasn't been stored to this store yet: the actual creation of
	// a usable layer is done by calling the returned object's PutAs() method.  After creating
	// a layer, the caller must then call the object's Release() method to free any temporary
	// resources which were allocated for the object by this method or the object's PutAs()
	// method.
	// This API is experimental and can be changed without bumping the major version number.
	LookupAdditionalLayer(tocDigest digest.Digest, imageref string) (AdditionalLayer, error)

	// Tries to clean up remainders of previous containers or layers that are not
	// references in the json files. These can happen in the case of unclean
	// shutdowns or regular restarts in transient store mode.
	GarbageCollect() error

	// Check returns a report of things that look wrong in the store.
	Check(options *CheckOptions) (CheckReport, error)
	// Repair attempts to remediate problems mentioned in the CheckReport,
	// usually by deleting layers and images which are damaged.  If the
	// right options are set, it will remove containers as well.
	Repair(report CheckReport, options *RepairOptions) []error

	// MultiList returns a MultiListResult structure that contains layer, image, or container
	// extracts according to the values in MultiListOptions.
	// MultiList returns consistent values as of a single point in time.
	// WARNING: The values may already be out of date by the time they are returned to the caller.
	MultiList(MultiListOptions) (MultiListResult, error)

	// Dedup deduplicates layers in the store.
	Dedup(DedupArgs) (drivers.DedupResult, error)
}

// AdditionalLayer represents a layer that is contained in the additional layer store
// This API is experimental and can be changed without bumping the major version number.
type AdditionalLayer interface {
	// PutAs creates layer based on this handler, using diff contents from the additional
	// layer store.
	PutAs(id, parent string, names []string) (*Layer, error)

	// TOCDigest returns the digest of TOC of this layer. Returns "" if unknown.
	TOCDigest() digest.Digest

	// CompressedSize returns the compressed size of this layer
	CompressedSize() int64

	// Release tells the additional layer store that we don't use this handler.
	Release()
}

type AutoUserNsOptions = types.AutoUserNsOptions

type IDMappingOptions = types.IDMappingOptions

// LayerOptions is used for passing options to a Store's CreateLayer() and PutLayer() methods.
type LayerOptions struct {
	// IDMappingOptions specifies the type of ID mapping which should be
	// used for this layer.  If nothing is specified, the layer will
	// inherit settings from its parent layer or, if it has no parent
	// layer, the Store object.
	types.IDMappingOptions
	// TemplateLayer is the ID of a layer whose contents will be used to
	// initialize this layer.  If set, it should be a child of the layer
	// which we want to use as the parent of the new layer.
	TemplateLayer string
	// OriginalDigest specifies a digest of the (possibly-compressed) tarstream (diff), if one is
	// provided along with these LayerOptions, and reliably known by the caller.
	// The digest might not be exactly the digest of the provided tarstream
	// (e.g. the digest might be of a compressed representation, while providing
	// an uncompressed one); in that case the caller is responsible for the two matching.
	// Use the default "" if this fields is not applicable or the value is not known.
	OriginalDigest digest.Digest
	// OriginalSize specifies a size of the (possibly-compressed) tarstream corresponding
	// to OriginalDigest.
	// If the digest does not match the provided tarstream, OriginalSize must match OriginalDigest,
	// not the tarstream.
	// Use nil if not applicable or not known.
	OriginalSize *int64
	// UncompressedDigest specifies a digest of the uncompressed version (“DiffID”)
	// of the tarstream (diff), if one is provided along with these LayerOptions,
	// and reliably known by the caller.
	// Use the default "" if this fields is not applicable or the value is not known.
	UncompressedDigest digest.Digest
	// True is the layer info can be treated as volatile
	Volatile bool
	// BigData is a set of items which should be stored with the layer.
	BigData []LayerBigDataOption
	// Flags is a set of named flags and their values to store with the layer.
	// Currently these can only be set when the layer record is created, but that
	// could change in the future.
	Flags map[string]any
}

type LayerBigDataOption struct {
	Key  string
	Data io.Reader
}

// ImageOptions is used for passing options to a Store's CreateImage() method.
type ImageOptions struct {
	// CreationDate, if not zero, will override the default behavior of marking the image as having been
	// created when CreateImage() was called, recording CreationDate instead.
	CreationDate time.Time
	// Digest is a hard-coded digest value that we can use to look up the image.  It is optional.
	Digest digest.Digest
	// Digests is a list of digest values of the image's manifests, and
	// possibly a manually-specified value, that we can use to locate the
	// image.  If Digest is set, its value is also in this list.
	Digests []digest.Digest
	// Metadata is caller-specified metadata associated with the layer.
	Metadata string
	// BigData is a set of items which should be stored with the image.
	BigData []ImageBigDataOption
	// NamesHistory is used for guessing for what this image was named when a container was created based
	// on it, but it no longer has any names.
	NamesHistory []string
	// Flags is a set of named flags and their values to store with the image.  Currently these can only
	// be set when the image record is created, but that could change in the future.
	Flags map[string]any
}

type ImageBigDataOption struct {
	Key    string
	Data   []byte
	Digest digest.Digest
}

// ContainerOptions is used for passing options to a Store's CreateContainer() method.
type ContainerOptions struct {
	// IDMappingOptions specifies the type of ID mapping which should be
	// used for this container's layer.  If nothing is specified, the
	// container's layer will inherit settings from the image's top layer
	// or, if it is not being created based on an image, the Store object.
	types.IDMappingOptions
	LabelOpts []string
	// Flags is a set of named flags and their values to store with the container.
	// Currently these can only be set when the container record is created, but that
	// could change in the future.
	Flags      map[string]any
	MountOpts  []string
	Volatile   bool
	StorageOpt map[string]string
	// Metadata is caller-specified metadata associated with the container.
	Metadata string
	// BigData is a set of items which should be stored for the container.
	BigData []ContainerBigDataOption
}

type ContainerBigDataOption struct {
	Key  string
	Data []byte
}

type store struct {
	// # Locking hierarchy:
	// These locks do not all need to be held simultaneously, but if some code does need to lock more than one, it MUST do so in this order:
	// - graphLock
	// - layerStore.start{Reading,Writing}
	// - roLayerStores[].startReading (in the order of the items of the roLayerStores array)
	// - imageStore.start{Reading,Writing}
	// - roImageStores[].startReading (in the order of the items of the roImageStores array)
	// - containerStore.start{Reading,Writing}

	// The following fields are only set when constructing store, and must never be modified afterwards.
	// They are safe to access without any other locking.
	runRoot             string
	graphDriverName     string // Initially set to the user-requested value, possibly ""; updated during store construction, and does not change afterwards.
	graphDriverPriority []string
	// graphLock:
	// - Ensures that we always reload graphDriver, and the primary layer store, after any process does store.Shutdown. This is necessary
	//   because (??) the Shutdown may forcibly unmount and clean up, affecting graph driver state in a way only a graph driver
	//   and layer store reinitialization can notice.
	// - Ensures that store.Shutdown is exclusive with mount operations. This is necessary at because some
	//   graph drivers call mount.MakePrivate() during initialization, the mount operations require that, and the driver’s Cleanup() method
	//   may undo that. So, holding graphLock is required throughout the duration of Shutdown(), and the duration of any mount
	//   (but not unmount) calls.
	// - Within this store object, protects access to some related in-memory state.
	graphLock       *lockfile.LockFile
	usernsLock      *lockfile.LockFile
	graphRoot       string
	graphOptions    []string
	imageStoreDir   string
	pullOptions     map[string]string
	uidMap          []idtools.IDMap
	gidMap          []idtools.IDMap
	autoUsernsUser  string
	autoNsMinSize   uint32
	autoNsMaxSize   uint32
	imageStore      rwImageStore
	rwImageStores   []rwImageStore
	roImageStores   []roImageStore
	containerStore  rwContainerStore
	digestLockRoot  string
	disableVolatile bool
	transientStore  bool

	// The following fields can only be accessed with graphLock held.
	graphLockLastWrite lockfile.LastWrite
	// FIXME: This field is only set when holding graphLock, but locking rules of the driver
	// interface itself are not documented here. It is extensively used without holding graphLock.
	graphDriver             drivers.Driver
	layerStoreUseGetters    rwLayerStore   // Almost all users should use the provided accessors instead of accessing this field directly.
	roLayerStoresUseGetters []roLayerStore // Almost all users should use the provided accessors instead of accessing this field directly.

	// FIXME: The following fields need locking, and don’t have it.
	additionalUIDs *idSet // Set by getAvailableIDs()
	additionalGIDs *idSet // Set by getAvailableIDs()
}

// GetStore attempts to find an already-created Store object matching the
// specified location and graph driver, and if it can't, it creates and
// initializes a new Store object, and the underlying storage that it controls.
//
// If StoreOptions `options` haven't been fully populated, then DefaultStoreOptions are used.
//
// These defaults observe environment variables:
//   - `STORAGE_DRIVER` for the name of the storage driver to attempt to use
//   - `STORAGE_OPTS` for the string of options to pass to the driver
//
// Note that we do some of this work in a child process.  The calling process's
// main() function needs to import our pkg/reexec package and should begin with
// something like this in order to allow us to properly start that child
// process:
//
//	if reexec.Init() {
//	    return
//	}
func GetStore(options types.StoreOptions) (Store, error) {
	defaultOpts, err := types.Options()
	if err != nil {
		return nil, err
	}
	if options.RunRoot == "" && options.GraphRoot == "" && options.GraphDriverName == "" && len(options.GraphDriverOptions) == 0 {
		options = defaultOpts
	}

	if options.GraphRoot != "" {
		dir, err := filepath.Abs(options.GraphRoot)
		if err != nil {
			return nil, err
		}
		options.GraphRoot = dir
	}
	if options.RunRoot != "" {
		dir, err := filepath.Abs(options.RunRoot)
		if err != nil {
			return nil, err
		}
		options.RunRoot = dir
	}

	storesLock.Lock()
	defer storesLock.Unlock()

	// return if BOTH run and graph root are matched, otherwise our run-root can be overridden if the graph is found first
	for _, s := range stores {
		if (s.graphRoot == options.GraphRoot) && (s.runRoot == options.RunRoot) && (options.GraphDriverName == "" || s.graphDriverName == options.GraphDriverName) {
			return s, nil
		}
	}

	// if passed a run-root or graph-root alone, the other should be defaulted only error if we have neither.
	switch {
	case options.RunRoot == "" && options.GraphRoot == "":
		return nil, fmt.Errorf("no storage runroot or graphroot specified: %w", ErrIncompleteOptions)
	case options.GraphRoot == "":
		options.GraphRoot = defaultOpts.GraphRoot
	case options.RunRoot == "":
		options.RunRoot = defaultOpts.RunRoot
	}

	if err := os.MkdirAll(options.RunRoot, 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(options.GraphRoot, 0o700); err != nil {
		return nil, err
	}
	if options.ImageStore != "" {
		if err := os.MkdirAll(options.ImageStore, 0o700); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Join(options.GraphRoot, options.GraphDriverName), 0o700); err != nil {
		return nil, err
	}
	if options.ImageStore != "" {
		if err := os.MkdirAll(filepath.Join(options.ImageStore, options.GraphDriverName), 0o700); err != nil {
			return nil, err
		}
	}

	graphLock, err := lockfile.GetLockFile(filepath.Join(options.GraphRoot, "storage.lock"))
	if err != nil {
		return nil, err
	}

	usernsLock, err := lockfile.GetLockFile(filepath.Join(options.GraphRoot, "userns.lock"))
	if err != nil {
		return nil, err
	}

	autoNsMinSize := options.AutoNsMinSize
	autoNsMaxSize := options.AutoNsMaxSize
	if autoNsMinSize == 0 {
		autoNsMinSize = AutoUserNsMinSize
	}
	if autoNsMaxSize == 0 {
		autoNsMaxSize = AutoUserNsMaxSize
	}
	s := &store{
		runRoot:             options.RunRoot,
		graphDriverName:     options.GraphDriverName,
		graphDriverPriority: options.GraphDriverPriority,
		graphLock:           graphLock,
		usernsLock:          usernsLock,
		graphRoot:           options.GraphRoot,
		graphOptions:        options.GraphDriverOptions,
		imageStoreDir:       options.ImageStore,
		pullOptions:         options.PullOptions,
		uidMap:              copySlicePreferringNil(options.UIDMap),
		gidMap:              copySlicePreferringNil(options.GIDMap),
		autoUsernsUser:      options.RootAutoNsUser,
		autoNsMinSize:       autoNsMinSize,
		autoNsMaxSize:       autoNsMaxSize,
		disableVolatile:     options.DisableVolatile,
		transientStore:      options.TransientStore,

		additionalUIDs: nil,
		additionalGIDs: nil,
	}
	if err := s.load(); err != nil {
		return nil, err
	}

	stores = append(stores, s)

	return s, nil
}

func (s *store) RunRoot() string {
	return s.runRoot
}

func (s *store) GraphDriverName() string {
	return s.graphDriverName
}

func (s *store) GraphRoot() string {
	return s.graphRoot
}

func (s *store) ImageStore() string {
	return s.imageStoreDir
}

func (s *store) TransientStore() bool {
	return s.transientStore
}

func (s *store) GraphOptions() []string {
	return s.graphOptions
}

func (s *store) PullOptions() map[string]string {
	cp := make(map[string]string, len(s.pullOptions))
	maps.Copy(cp, s.pullOptions)
	return cp
}

func (s *store) UIDMap() []idtools.IDMap {
	return copySlicePreferringNil(s.uidMap)
}

func (s *store) GIDMap() []idtools.IDMap {
	return copySlicePreferringNil(s.gidMap)
}

// This must only be called when constructing store; it writes to fields that are assumed to be constant after construction.
func (s *store) load() error {
	var driver drivers.Driver
	if err := func() error { // A scope for defer
		s.graphLock.Lock()
		defer s.graphLock.Unlock()
		lastWrite, err := s.graphLock.GetLastWrite()
		if err != nil {
			return err
		}
		s.graphLockLastWrite = lastWrite
		driver, err = s.createGraphDriverLocked()
		if err != nil {
			return err
		}
		s.graphDriver = driver
		s.graphDriverName = driver.String()
		return nil
	}(); err != nil {
		return err
	}
	driverPrefix := s.graphDriverName + "-"

	imgStoreRoot := s.imageStoreDir
	if imgStoreRoot == "" {
		imgStoreRoot = s.graphRoot
	}
	gipath := filepath.Join(imgStoreRoot, driverPrefix+"images")
	if err := os.MkdirAll(gipath, 0o700); err != nil {
		return err
	}
	imageStore, err := newImageStore(gipath)
	if err != nil {
		return err
	}
	s.imageStore = imageStore

	s.rwImageStores = []rwImageStore{imageStore}

	gcpath := filepath.Join(s.graphRoot, driverPrefix+"containers")
	if err := os.MkdirAll(gcpath, 0o700); err != nil {
		return err
	}
	rcpath := filepath.Join(s.runRoot, driverPrefix+"containers")
	if err := os.MkdirAll(rcpath, 0o700); err != nil {
		return err
	}

	rcs, err := newContainerStore(gcpath, rcpath, s.transientStore)
	if err != nil {
		return err
	}

	s.containerStore = rcs

	additionalImageStores := s.graphDriver.AdditionalImageStores()
	if s.imageStoreDir != "" {
		additionalImageStores = append([]string{s.graphRoot}, additionalImageStores...)
	}

	for _, store := range additionalImageStores {
		gipath := filepath.Join(store, driverPrefix+"images")
		var ris roImageStore
		// both the graphdriver and the imagestore must be used read-write.
		if store == s.imageStoreDir || store == s.graphRoot {
			imageStore, err := newImageStore(gipath)
			if err != nil {
				return err
			}
			s.rwImageStores = append(s.rwImageStores, imageStore)
			ris = imageStore
		} else {
			ris, err = newROImageStore(gipath)
			if err != nil {
				if errors.Is(err, syscall.EROFS) {
					logrus.Debugf("Ignoring creation of lockfiles on read-only file systems %q, %v", gipath, err)
					continue
				}
				return err
			}
		}
		s.roImageStores = append(s.roImageStores, ris)
	}

	s.digestLockRoot = filepath.Join(s.runRoot, driverPrefix+"locks")
	if err := os.MkdirAll(s.digestLockRoot, 0o700); err != nil {
		return err
	}

	return nil
}

// GetDigestLock returns a digest-specific Locker.
func (s *store) GetDigestLock(d digest.Digest) (Locker, error) {
	return lockfile.GetLockFile(filepath.Join(s.digestLockRoot, d.String()))
}

// startUsingGraphDriver obtains s.graphLock and ensures that s.graphDriver is set and fresh.
// It only intended to be used on a fully-constructed store.
// If this succeeds, the caller MUST call stopUsingGraphDriver().
func (s *store) startUsingGraphDriver() error {
	s.graphLock.Lock()
	succeeded := false
	defer func() {
		if !succeeded {
			s.graphLock.Unlock()
		}
	}()

	lastWrite, modified, err := s.graphLock.ModifiedSince(s.graphLockLastWrite)
	if err != nil {
		return err
	}
	if modified {
		driver, err := s.createGraphDriverLocked()
		if err != nil {
			return err
		}
		// Our concurrency design requires s.graphDriverName not to be modified after
		// store is constructed.
		// It’s fine for driver.String() not to match the requested graph driver name
		// (e.g. if the user asks for overlay2 and gets overlay), but it must be an idempotent
		// mapping:
		//	driver1 := drivers.New(userInput, config)
		//	name1 := driver1.String()
		//	name2 := drivers.New(name1, config).String()
		//	assert(name1 == name2)
		if s.graphDriverName != driver.String() {
			return fmt.Errorf("graph driver name changed from %q to %q during reload",
				s.graphDriverName, driver.String())
		}
		s.graphDriver = driver
		s.layerStoreUseGetters = nil
		s.graphLockLastWrite = lastWrite
	}

	succeeded = true
	return nil
}

// stopUsingGraphDriver releases graphLock obtained by startUsingGraphDriver.
func (s *store) stopUsingGraphDriver() {
	s.graphLock.Unlock()
}

// createGraphDriverLocked creates a new instance of graph driver for s, and returns it.
// Almost all users should use startUsingGraphDriver instead.
// The caller must hold s.graphLock.
func (s *store) createGraphDriverLocked() (drivers.Driver, error) {
	config := drivers.Options{
		Root:           s.graphRoot,
		ImageStore:     s.imageStoreDir,
		RunRoot:        s.runRoot,
		DriverPriority: s.graphDriverPriority,
		DriverOptions:  s.graphOptions,
	}
	return drivers.New(s.graphDriverName, config)
}

func (s *store) GraphDriver() (drivers.Driver, error) {
	if err := s.startUsingGraphDriver(); err != nil {
		return nil, err
	}
	defer s.stopUsingGraphDriver()
	return s.graphDriver, nil
}

// getLayerStoreLocked obtains and returns a handle to the writeable layer store object
// used by the Store.
// It must be called with s.graphLock held.
func (s *store) getLayerStoreLocked() (rwLayerStore, error) {
	if s.layerStoreUseGetters != nil {
		return s.layerStoreUseGetters, nil
	}
	driverPrefix := s.graphDriverName + "-"
	rlpath := filepath.Join(s.runRoot, driverPrefix+"layers")
	if err := os.MkdirAll(rlpath, 0o700); err != nil {
		return nil, err
	}
	glpath := filepath.Join(s.graphRoot, driverPrefix+"layers")
	if err := os.MkdirAll(glpath, 0o700); err != nil {
		return nil, err
	}
	ilpath := ""
	if s.imageStoreDir != "" {
		ilpath = filepath.Join(s.imageStoreDir, driverPrefix+"layers")
	}
	rls, err := s.newLayerStore(rlpath, glpath, ilpath, s.graphDriver, s.transientStore)
	if err != nil {
		return nil, err
	}
	s.layerStoreUseGetters = rls
	return s.layerStoreUseGetters, nil
}

// getLayerStore obtains and returns a handle to the writeable layer store object
// used by the store.
// It must be called WITHOUT s.graphLock held.
func (s *store) getLayerStore() (rwLayerStore, error) {
	if err := s.startUsingGraphDriver(); err != nil {
		return nil, err
	}
	defer s.stopUsingGraphDriver()
	return s.getLayerStoreLocked()
}

// getROLayerStoresLocked obtains additional read/only layer store objects used by the
// Store.
// It must be called with s.graphLock held.
func (s *store) getROLayerStoresLocked() ([]roLayerStore, error) {
	if s.roLayerStoresUseGetters != nil {
		return s.roLayerStoresUseGetters, nil
	}
	driverPrefix := s.graphDriverName + "-"
	rlpath := filepath.Join(s.runRoot, driverPrefix+"layers")
	if err := os.MkdirAll(rlpath, 0o700); err != nil {
		return nil, err
	}

	for _, store := range s.graphDriver.AdditionalImageStores() {
		glpath := filepath.Join(store, driverPrefix+"layers")

		rls, err := newROLayerStore(rlpath, glpath, s.graphDriver)
		if err != nil {
			return nil, err
		}
		s.roLayerStoresUseGetters = append(s.roLayerStoresUseGetters, rls)
	}
	return s.roLayerStoresUseGetters, nil
}

// bothLayerStoreKindsLocked returns the primary, and additional read-only, layer store objects used by the store.
// It must be called with s.graphLock held.
func (s *store) bothLayerStoreKindsLocked() (rwLayerStore, []roLayerStore, error) {
	primary, err := s.getLayerStoreLocked()
	if err != nil {
		return nil, nil, fmt.Errorf("loading primary layer store data: %w", err)
	}
	additional, err := s.getROLayerStoresLocked()
	if err != nil {
		return nil, nil, fmt.Errorf("loading additional layer stores: %w", err)
	}
	return primary, additional, nil
}

// bothLayerStoreKinds returns the primary, and additional read-only, layer store objects used by the store.
// It must be called WITHOUT s.graphLock held.
func (s *store) bothLayerStoreKinds() (rwLayerStore, []roLayerStore, error) {
	if err := s.startUsingGraphDriver(); err != nil {
		return nil, nil, err
	}
	defer s.stopUsingGraphDriver()
	return s.bothLayerStoreKindsLocked()
}

// allLayerStores returns a list of all layer store objects used by the Store.
// This is a convenience method for read-only users of the Store.
// It must be called with s.graphLock held.
func (s *store) allLayerStoresLocked() ([]roLayerStore, error) {
	primary, additional, err := s.bothLayerStoreKindsLocked()
	if err != nil {
		return nil, err
	}
	return append([]roLayerStore{primary}, additional...), nil
}

// allLayerStores returns a list of all layer store objects used by the Store.
// This is a convenience method for read-only users of the Store.
// It must be called WITHOUT s.graphLock held.
func (s *store) allLayerStores() ([]roLayerStore, error) {
	if err := s.startUsingGraphDriver(); err != nil {
		return nil, err
	}
	defer s.stopUsingGraphDriver()
	return s.allLayerStoresLocked()
}

// readAllLayerStores processes allLayerStores() in order:
// It locks the store for reading, checks for updates, and calls
//
//	(data, done, err) := fn(store)
//
// until the callback returns done == true, and returns the data from the callback.
//
// If reading any layer store fails, it immediately returns ({}, true, err).
//
// If all layer stores are processed without setting done == true, it returns ({}, false, nil).
//
// Typical usage:
//
//	if res, done, err := s.readAllLayerStores(store, func(…) {
//		…
//	}; done {
//		return res, err
//	}
func readAllLayerStores[T any](s *store, fn func(store roLayerStore) (T, bool, error)) (T, bool, error) {
	var zeroRes T // A zero value of T

	layerStores, err := s.allLayerStores()
	if err != nil {
		return zeroRes, true, err
	}
	for _, s := range layerStores {
		store := s
		if err := store.startReading(); err != nil {
			return zeroRes, true, err
		}
		defer store.stopReading()
		if res, done, err := fn(store); done {
			return res, true, err
		}
	}
	return zeroRes, false, nil
}

// writeToLayerStore is a helper for working with store.getLayerStore():
// It locks the store for writing, checks for updates, and calls fn()
// It returns the return value of fn, or its own error initializing the store.
func writeToLayerStore[T any](s *store, fn func(store rwLayerStore) (T, error)) (T, error) {
	var zeroRes T // A zero value of T

	store, err := s.getLayerStore()
	if err != nil {
		return zeroRes, err
	}

	if err := store.startWriting(); err != nil {
		return zeroRes, err
	}
	defer store.stopWriting()
	return fn(store)
}

// readOrWriteAllLayerStores processes allLayerStores() in order:
// It locks the writeable store for writing and all others for reading, checks
// for updates, and calls
//
//	(data, done, err) := fn(store)
//
// until the callback returns done == true, and returns the data from the callback.
//
// If reading or writing any layer store fails, it immediately returns ({}, true, err).
//
// If all layer stores are processed without setting done == true, it returns ({}, false, nil).
//
// Typical usage:
//
//	if res, done, err := s.readOrWriteAllLayerStores(store, func(…) {
//		…
//	}; done {
//		return res, err
//	}
func readOrWriteAllLayerStores[T any](s *store, fn func(store roLayerStore) (T, bool, error)) (T, bool, error) {
	var zeroRes T // A zero value of T

	rwLayerStore, roLayerStores, err := s.bothLayerStoreKinds()
	if err != nil {
		return zeroRes, true, err
	}

	if err := rwLayerStore.startWriting(); err != nil {
		return zeroRes, true, err
	}
	defer rwLayerStore.stopWriting()
	if res, done, err := fn(rwLayerStore); done {
		return res, true, err
	}

	for _, s := range roLayerStores {
		store := s
		if err := store.startReading(); err != nil {
			return zeroRes, true, err
		}
		defer store.stopReading()
		if res, done, err := fn(store); done {
			return res, true, err
		}
	}
	return zeroRes, false, nil
}

// allImageStores returns a list of all image store objects used by the Store.
// This is a convenience method for read-only users of the Store.
func (s *store) allImageStores() []roImageStore {
	return append([]roImageStore{s.imageStore}, s.roImageStores...)
}

// readAllImageStores processes allImageStores() in order:
// It locks the store for reading, checks for updates, and calls
//
//	(data, done, err) := fn(store)
//
// until the callback returns done == true, and returns the data from the callback.
//
// If reading any Image store fails, it immediately returns ({}, true, err).
//
// If all Image stores are processed without setting done == true, it returns ({}, false, nil).
//
// Typical usage:
//
//	if res, done, err := readAllImageStores(store, func(…) {
//		…
//	}; done {
//		return res, err
//	}
func readAllImageStores[T any](s *store, fn func(store roImageStore) (T, bool, error)) (T, bool, error) {
	var zeroRes T // A zero value of T

	for _, s := range s.allImageStores() {
		store := s
		if err := store.startReading(); err != nil {
			return zeroRes, true, err
		}
		defer store.stopReading()
		if res, done, err := fn(store); done {
			return res, true, err
		}
	}
	return zeroRes, false, nil
}

// writeToImageStore is a convenience helper for working with store.imageStore:
// It locks the store for writing, checks for updates, and calls fn(), which can then access store.imageStore.
// It returns the return value of fn, or its own error initializing the store.
func writeToImageStore[T any](s *store, fn func() (T, error)) (T, error) {
	if err := s.imageStore.startWriting(); err != nil {
		var zeroRes T // A zero value of T
		return zeroRes, err
	}
	defer s.imageStore.stopWriting()
	return fn()
}

// readContainerStore is a convenience helper for working with store.containerStore:
// It locks the store for reading, checks for updates, and calls fn(), which can then access store.containerStore.
// If reading the container store fails, it returns ({}, true, err).
// Returns the return value of fn on success.
func readContainerStore[T any](s *store, fn func() (T, bool, error)) (T, bool, error) {
	if err := s.containerStore.startReading(); err != nil {
		var zeroRes T // A zero value of T
		return zeroRes, true, err
	}
	defer s.containerStore.stopReading()
	return fn()
}

// writeToContainerStore is a convenience helper for working with store.containerStore:
// It locks the store for writing, checks for updates, and calls fn(), which can then access store.containerStore.
// It returns the return value of fn, or its own error initializing the store.
func writeToContainerStore[T any](s *store, fn func() (T, error)) (T, error) {
	if err := s.containerStore.startWriting(); err != nil {
		var zeroRes T // A zero value of T
		return zeroRes, err
	}
	defer s.containerStore.stopWriting()
	return fn()
}

// writeToAllStores is a convenience helper for writing to all three stores:
// It locks the stores for writing, checks for updates, and calls fn(), which can then access the provided layer store,
// s.imageStore and s.containerStore.
// It returns the return value of fn, or its own error initializing the stores.
func (s *store) writeToAllStores(fn func(rlstore rwLayerStore) error) error {
	rlstore, err := s.getLayerStore()
	if err != nil {
		return err
	}

	if err := rlstore.startWriting(); err != nil {
		return err
	}
	defer rlstore.stopWriting()
	if err := s.imageStore.startWriting(); err != nil {
		return err
	}
	defer s.imageStore.stopWriting()
	if err := s.containerStore.startWriting(); err != nil {
		return err
	}
	defer s.containerStore.stopWriting()

	return fn(rlstore)
}

// canUseShifting returns true if we can use mount-time arguments (shifting) to
// avoid having to create a mapped top layer for a base image when we want to
// use it to create a container using ID mappings.
// On entry:
// - rlstore must be locked for writing
func (s *store) canUseShifting(uidmap, gidmap []idtools.IDMap) bool {
	return s.graphDriver.SupportsShifting(uidmap, gidmap)
}

// On entry:
// - rlstore must be locked for writing
// - rlstores MUST NOT be locked
func (s *store) putLayer(rlstore rwLayerStore, rlstores []roLayerStore, id, parent string, names []string, mountLabel string, writeable bool, lOptions *LayerOptions, diff io.Reader, slo *stagedLayerOptions) (*Layer, int64, error) {
	var parentLayer *Layer
	var options LayerOptions
	if lOptions != nil {
		options = *lOptions
		options.BigData = slices.Clone(lOptions.BigData)
		options.Flags = copyMapPreferringNil(lOptions.Flags)
	}
	if options.HostUIDMapping {
		options.UIDMap = nil
	}
	if options.HostGIDMapping {
		options.GIDMap = nil
	}
	uidMap := options.UIDMap
	gidMap := options.GIDMap
	if parent != "" {
		var ilayer *Layer
		for _, l := range append([]roLayerStore{rlstore}, rlstores...) {
			lstore := l
			if lstore != rlstore {
				if err := lstore.startReading(); err != nil {
					return nil, -1, err
				}
				defer lstore.stopReading()
			}
			if l, err := lstore.Get(parent); err == nil && l != nil {
				ilayer = l
				parent = ilayer.ID
				break
			}
		}
		if ilayer == nil {
			return nil, -1, ErrLayerUnknown
		}
		parentLayer = ilayer

		if err := s.containerStore.startWriting(); err != nil {
			return nil, -1, err
		}
		defer s.containerStore.stopWriting()
		containers, err := s.containerStore.Containers()
		if err != nil {
			return nil, -1, err
		}
		for _, container := range containers {
			if container.LayerID == parent {
				return nil, -1, ErrParentIsContainer
			}
		}
		if !options.HostUIDMapping && len(options.UIDMap) == 0 {
			uidMap = ilayer.UIDMap
		}
		if !options.HostGIDMapping && len(options.GIDMap) == 0 {
			gidMap = ilayer.GIDMap
		}
	} else {
		// FIXME? It’s unclear why we are holding containerStore locked here at all
		// (and because we are not modifying it, why it is a write lock, not a read lock).
		if err := s.containerStore.startWriting(); err != nil {
			return nil, -1, err
		}
		defer s.containerStore.stopWriting()

		if !options.HostUIDMapping && len(options.UIDMap) == 0 {
			uidMap = s.uidMap
		}
		if !options.HostGIDMapping && len(options.GIDMap) == 0 {
			gidMap = s.gidMap
		}
	}
	if s.canUseShifting(uidMap, gidMap) {
		options.IDMappingOptions = types.IDMappingOptions{HostUIDMapping: true, HostGIDMapping: true, UIDMap: nil, GIDMap: nil}
	} else {
		options.IDMappingOptions = types.IDMappingOptions{
			HostUIDMapping: options.HostUIDMapping,
			HostGIDMapping: options.HostGIDMapping,
			UIDMap:         copySlicePreferringNil(uidMap),
			GIDMap:         copySlicePreferringNil(gidMap),
		}
	}
	return rlstore.create(id, parentLayer, names, mountLabel, nil, &options, writeable, diff, slo)
}

func (s *store) PutLayer(id, parent string, names []string, mountLabel string, writeable bool, lOptions *LayerOptions, diff io.Reader) (*Layer, int64, error) {
	rlstore, rlstores, err := s.bothLayerStoreKinds()
	if err != nil {
		return nil, -1, err
	}
	if err := rlstore.startWriting(); err != nil {
		return nil, -1, err
	}
	defer rlstore.stopWriting()
	return s.putLayer(rlstore, rlstores, id, parent, names, mountLabel, writeable, lOptions, diff, nil)
}

func (s *store) CreateLayer(id, parent string, names []string, mountLabel string, writeable bool, options *LayerOptions) (*Layer, error) {
	layer, _, err := s.PutLayer(id, parent, names, mountLabel, writeable, options, nil)
	return layer, err
}

func (s *store) CreateImage(id string, names []string, layer, metadata string, iOptions *ImageOptions) (*Image, error) {
	if layer != "" {
		layerStores, err := s.allLayerStores()
		if err != nil {
			return nil, err
		}
		var ilayer *Layer
		for _, s := range layerStores {
			store := s
			if err := store.startReading(); err != nil {
				return nil, err
			}
			defer store.stopReading()
			ilayer, err = store.Get(layer)
			if err == nil {
				break
			}
		}
		if ilayer == nil {
			return nil, ErrLayerUnknown
		}
		layer = ilayer.ID
	}

	return writeToImageStore(s, func() (*Image, error) {
		var options ImageOptions
		var namesToAddAfterCreating []string

		// Check if the ID refers to an image in a read-only store -- we want
		// to allow images in read-only stores to have their names changed, so
		// if we find one, merge the new values in with what we know about the
		// image that's already there.
		if id != "" {
			for _, is := range s.roImageStores {
				store := is
				if err := store.startReading(); err != nil {
					return nil, err
				}
				defer store.stopReading()
				if i, err := store.Get(id); err == nil {
					// set information about this image in "options"
					options = ImageOptions{
						Metadata:     i.Metadata,
						CreationDate: i.Created,
						Digest:       i.Digest,
						Digests:      copySlicePreferringNil(i.Digests),
						NamesHistory: copySlicePreferringNil(i.NamesHistory),
					}
					for _, key := range i.BigDataNames {
						data, err := store.BigData(id, key)
						if err != nil {
							return nil, err
						}
						dataDigest, err := store.BigDataDigest(id, key)
						if err != nil {
							return nil, err
						}
						options.BigData = append(options.BigData, ImageBigDataOption{
							Key:    key,
							Data:   data,
							Digest: dataDigest,
						})
					}
					namesToAddAfterCreating = dedupeStrings(slices.Concat(i.Names, names))
					break
				}
			}
		}

		// merge any passed-in options into "options" as best we can
		if iOptions != nil {
			if !iOptions.CreationDate.IsZero() {
				options.CreationDate = iOptions.CreationDate
			}
			if iOptions.Digest != "" {
				options.Digest = iOptions.Digest
			}
			options.Digests = append(options.Digests, iOptions.Digests...)
			if iOptions.Metadata != "" {
				options.Metadata = iOptions.Metadata
			}
			options.BigData = append(options.BigData, copyImageBigDataOptionSlice(iOptions.BigData)...)
			options.NamesHistory = append(options.NamesHistory, iOptions.NamesHistory...)
			if options.Flags == nil {
				options.Flags = make(map[string]any)
			}
			maps.Copy(options.Flags, iOptions.Flags)
		}

		if options.CreationDate.IsZero() {
			options.CreationDate = time.Now().UTC()
		}
		if metadata != "" {
			options.Metadata = metadata
		}

		res, err := s.imageStore.create(id, names, layer, options)
		if err == nil && len(namesToAddAfterCreating) > 0 {
			// set any names we pulled up from an additional image store, now that we won't be
			// triggering a duplicate names error
			err = s.imageStore.updateNames(res.ID, namesToAddAfterCreating, addNames)
		}
		return res, err
	})
}

// imageTopLayerForMapping locates the layer that can take the place of the
// image's top layer as the shared parent layer for a one or more containers
// which are using ID mappings.
// On entry:
// - ristore must be locked EITHER for reading or writing
// - s.imageStore must be locked for writing; it might be identical to ristore.
// - rlstore must be locked for writing
// - lstores must all be locked for reading
func (s *store) imageTopLayerForMapping(image *Image, ristore roImageStore, rlstore rwLayerStore, lstores []roLayerStore, options types.IDMappingOptions) (*Layer, error) {
	layerMatchesMappingOptions := func(layer *Layer, options types.IDMappingOptions) bool {
		// If the driver supports shifting and the layer has no mappings, we can use it.
		if s.canUseShifting(options.UIDMap, options.GIDMap) && len(layer.UIDMap) == 0 && len(layer.GIDMap) == 0 {
			return true
		}
		// If we want host mapping, and the layer uses mappings, it's not the best match.
		if options.HostUIDMapping && len(layer.UIDMap) != 0 {
			return false
		}
		if options.HostGIDMapping && len(layer.GIDMap) != 0 {
			return false
		}
		// Compare the maps.
		return reflect.DeepEqual(layer.UIDMap, options.UIDMap) && reflect.DeepEqual(layer.GIDMap, options.GIDMap)
	}
	var layer, parentLayer *Layer
	allStores := append([]roLayerStore{rlstore}, lstores...)
	// Locate the image's top layer and its parent, if it has one.
	createMappedLayer := ristore == s.imageStore
	for _, s := range allStores {
		store := s
		// Walk the top layer list.
		for _, candidate := range append([]string{image.TopLayer}, image.MappedTopLayers...) {
			if cLayer, err := store.Get(candidate); err == nil {
				// We want the layer's parent, too, if it has one.
				var cParentLayer *Layer
				if cLayer.Parent != "" {
					// Its parent should be in one of the stores, somewhere.
					for _, ps := range allStores {
						if cParentLayer, err = ps.Get(cLayer.Parent); err == nil {
							break
						}
					}
					if cParentLayer == nil {
						continue
					}
				}
				// If the layer matches the desired mappings, it's a perfect match,
				// so we're actually done here.
				if layerMatchesMappingOptions(cLayer, options) {
					return cLayer, nil
				}
				// Record the first one that we found, even if it's not ideal, so that
				// we have a starting point.
				if layer == nil {
					layer = cLayer
					parentLayer = cParentLayer
					if store != rlstore {
						// The layer is in another store, so we cannot
						// create a mapped version of it to the image.
						createMappedLayer = false
					}
				}
			}
		}
	}
	if layer == nil {
		return nil, ErrLayerUnknown
	}
	// The top layer's mappings don't match the ones we want, but it's in a read-only
	// image store, so we can't create and add a mapped copy of the layer to the image.
	// We'll have to do the mapping for the container itself, elsewhere.
	if !createMappedLayer {
		return layer, nil
	}
	// The top layer's mappings don't match the ones we want, and it's in an image store
	// that lets us edit image metadata, so create a duplicate of the layer with the desired
	// mappings, and register it as an alternate top layer in the image.
	var layerOptions LayerOptions
	if s.canUseShifting(options.UIDMap, options.GIDMap) {
		layerOptions.IDMappingOptions = types.IDMappingOptions{
			HostUIDMapping: true,
			HostGIDMapping: true,
			UIDMap:         nil,
			GIDMap:         nil,
		}
	} else {
		layerOptions.IDMappingOptions = types.IDMappingOptions{
			HostUIDMapping: options.HostUIDMapping,
			HostGIDMapping: options.HostGIDMapping,
			UIDMap:         copySlicePreferringNil(options.UIDMap),
			GIDMap:         copySlicePreferringNil(options.GIDMap),
		}
	}
	layerOptions.TemplateLayer = layer.ID
	mappedLayer, _, err := rlstore.create("", parentLayer, nil, layer.MountLabel, nil, &layerOptions, false, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("creating an ID-mapped copy of layer %q: %w", layer.ID, err)
	}
	// By construction, createMappedLayer can only be true if ristore == s.imageStore.
	if err = s.imageStore.addMappedTopLayer(image.ID, mappedLayer.ID); err != nil {
		if err2 := rlstore.deleteWhileHoldingLock(mappedLayer.ID); err2 != nil {
			err = fmt.Errorf("deleting layer %q: %v: %w", mappedLayer.ID, err2, err)
		}
		return nil, fmt.Errorf("registering ID-mapped layer with image %q: %w", image.ID, err)
	}
	return mappedLayer, nil
}

func (s *store) CreateContainer(id string, names []string, image, layer, metadata string, cOptions *ContainerOptions) (*Container, error) {
	var options ContainerOptions
	if cOptions != nil {
		options = *cOptions
		options.IDMappingOptions.UIDMap = copySlicePreferringNil(cOptions.IDMappingOptions.UIDMap)
		options.IDMappingOptions.GIDMap = copySlicePreferringNil(cOptions.IDMappingOptions.GIDMap)
		options.LabelOpts = copySlicePreferringNil(cOptions.LabelOpts)
		options.Flags = copyMapPreferringNil(cOptions.Flags)
		options.MountOpts = copySlicePreferringNil(cOptions.MountOpts)
		options.StorageOpt = copyMapPreferringNil(cOptions.StorageOpt)
		options.BigData = copyContainerBigDataOptionSlice(cOptions.BigData)
	}
	if options.HostUIDMapping {
		options.UIDMap = nil
	}
	if options.HostGIDMapping {
		options.GIDMap = nil
	}
	options.Metadata = metadata
	rlstore, lstores, err := s.bothLayerStoreKinds() // lstores will be locked read-only if image != ""
	if err != nil {
		return nil, err
	}

	var imageTopLayer *Layer
	imageID := ""

	if options.AutoUserNs || options.UIDMap != nil || options.GIDMap != nil {
		// Prevent multiple instances to retrieve the same range when AutoUserNs
		// are used.
		// It doesn't prevent containers that specify an explicit mapping to overlap
		// with AutoUserNs.
		s.usernsLock.Lock()
		defer s.usernsLock.Unlock()
	}

	var imageHomeStore roImageStore // Set if image != ""
	// s.imageStore is locked read-write, if image != ""
	// s.roImageStores are NOT NECESSARILY ALL locked read-only if image != ""
	var cimage *Image // Set if image != ""
	if image != "" {
		if err := rlstore.startWriting(); err != nil {
			return nil, err
		}
		defer rlstore.stopWriting()
		for _, s := range lstores {
			store := s
			if err := store.startReading(); err != nil {
				return nil, err
			}
			defer store.stopReading()
		}
		if err := s.imageStore.startWriting(); err != nil {
			return nil, err
		}
		defer s.imageStore.stopWriting()
		cimage, err = s.imageStore.Get(image)
		if err == nil {
			imageHomeStore = s.imageStore
		} else {
			for _, s := range s.roImageStores {
				store := s
				if err := store.startReading(); err != nil {
					return nil, err
				}
				defer store.stopReading()
				cimage, err = store.Get(image)
				if err == nil {
					imageHomeStore = store
					break
				}
			}
		}
		if cimage == nil {
			return nil, fmt.Errorf("locating image with ID %q: %w", image, ErrImageUnknown)
		}
		imageID = cimage.ID
	}

	if options.AutoUserNs {
		var err error
		options.UIDMap, options.GIDMap, err = s.getAutoUserNS(&options.AutoUserNsOpts, cimage, rlstore, lstores)
		if err != nil {
			return nil, err
		}
	}

	uidMap := options.UIDMap
	gidMap := options.GIDMap

	idMappingsOptions := options.IDMappingOptions
	if image != "" {
		if cimage.TopLayer != "" {
			ilayer, err := s.imageTopLayerForMapping(cimage, imageHomeStore, rlstore, lstores, idMappingsOptions)
			if err != nil {
				return nil, err
			}
			imageTopLayer = ilayer

			if !options.HostUIDMapping && len(options.UIDMap) == 0 {
				uidMap = ilayer.UIDMap
			}
			if !options.HostGIDMapping && len(options.GIDMap) == 0 {
				gidMap = ilayer.GIDMap
			}
		}
	} else {
		if err := rlstore.startWriting(); err != nil {
			return nil, err
		}
		defer rlstore.stopWriting()
		if !options.HostUIDMapping && len(options.UIDMap) == 0 {
			uidMap = s.uidMap
		}
		if !options.HostGIDMapping && len(options.GIDMap) == 0 {
			gidMap = s.gidMap
		}
	}
	layerOptions := &LayerOptions{
		// Normally layers for containers are volatile only if the container is.
		// But in transient store mode, all container layers are volatile.
		Volatile: options.Volatile || s.transientStore,
	}
	if s.canUseShifting(uidMap, gidMap) {
		layerOptions.IDMappingOptions = types.IDMappingOptions{
			HostUIDMapping: true,
			HostGIDMapping: true,
			UIDMap:         nil,
			GIDMap:         nil,
		}
	} else {
		layerOptions.IDMappingOptions = types.IDMappingOptions{
			HostUIDMapping: idMappingsOptions.HostUIDMapping,
			HostGIDMapping: idMappingsOptions.HostGIDMapping,
			UIDMap:         copySlicePreferringNil(uidMap),
			GIDMap:         copySlicePreferringNil(gidMap),
		}
	}
	if options.Flags == nil {
		options.Flags = make(map[string]any)
	}
	plabel, _ := options.Flags[processLabelFlag].(string)
	mlabel, _ := options.Flags[mountLabelFlag].(string)
	if (plabel == "" && mlabel != "") || (plabel != "" && mlabel == "") {
		return nil, errors.New("ProcessLabel and Mountlabel must either not be specified or both specified")
	}

	if plabel == "" {
		processLabel, mountLabel, err := label.InitLabels(options.LabelOpts)
		if err != nil {
			return nil, err
		}
		mlabel = mountLabel
		options.Flags[processLabelFlag] = processLabel
		options.Flags[mountLabelFlag] = mountLabel
	}

	clayer, _, err := rlstore.create(layer, imageTopLayer, nil, mlabel, options.StorageOpt, layerOptions, true, nil, nil)
	if err != nil {
		return nil, err
	}
	layer = clayer.ID

	// Normally only `--rm` containers are volatile, but in transient store mode all containers are volatile
	if s.transientStore {
		options.Volatile = true
	}

	return writeToContainerStore(s, func() (*Container, error) {
		options.IDMappingOptions = types.IDMappingOptions{
			HostUIDMapping: len(options.UIDMap) == 0,
			HostGIDMapping: len(options.GIDMap) == 0,
			UIDMap:         copySlicePreferringNil(options.UIDMap),
			GIDMap:         copySlicePreferringNil(options.GIDMap),
		}
		container, err := s.containerStore.create(id, names, imageID, layer, &options)
		if err != nil || container == nil {
			if err2 := rlstore.deleteWhileHoldingLock(layer); err2 != nil {
				if err == nil {
					err = fmt.Errorf("deleting layer %#v: %w", layer, err2)
				} else {
					logrus.Errorf("While recovering from a failure to create a container, error deleting layer %#v: %v", layer, err2)
				}
			}
		}
		return container, err
	})
}

func (s *store) SetMetadata(id, metadata string) error {
	return s.writeToAllStores(func(rlstore rwLayerStore) error {
		if rlstore.Exists(id) {
			return rlstore.SetMetadata(id, metadata)
		}
		if s.imageStore.Exists(id) {
			return s.imageStore.SetMetadata(id, metadata)
		}
		if s.containerStore.Exists(id) {
			return s.containerStore.SetMetadata(id, metadata)
		}
		return ErrNotAnID
	})
}

func (s *store) Metadata(id string) (string, error) {
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) (string, bool, error) {
		if store.Exists(id) {
			res, err := store.Metadata(id)
			return res, true, err
		}
		return "", false, nil
	}); done {
		return res, err
	}

	if res, done, err := readAllImageStores(s, func(store roImageStore) (string, bool, error) {
		if store.Exists(id) {
			res, err := store.Metadata(id)
			return res, true, err
		}
		return "", false, nil
	}); done {
		return res, err
	}

	if res, done, err := readContainerStore(s, func() (string, bool, error) {
		if s.containerStore.Exists(id) {
			res, err := s.containerStore.Metadata(id)
			return res, true, err
		}
		return "", false, nil
	}); done {
		return res, err
	}

	return "", ErrNotAnID
}

func (s *store) ListImageBigData(id string) ([]string, error) {
	if res, done, err := readAllImageStores(s, func(store roImageStore) ([]string, bool, error) {
		bigDataNames, err := store.BigDataNames(id)
		if err == nil {
			return bigDataNames, true, nil
		}
		return nil, false, nil
	}); done {
		return res, err
	}
	return nil, fmt.Errorf("locating image with ID %q: %w", id, ErrImageUnknown)
}

func (s *store) ImageBigDataSize(id, key string) (int64, error) {
	if res, done, err := readAllImageStores(s, func(store roImageStore) (int64, bool, error) {
		size, err := store.BigDataSize(id, key)
		if err == nil {
			return size, true, nil
		}
		return -1, false, nil
	}); done {
		if err != nil {
			return -1, err
		}
		return res, nil
	}
	return -1, ErrSizeUnknown
}

func (s *store) ImageBigDataDigest(id, key string) (digest.Digest, error) {
	if res, done, err := readAllImageStores(s, func(ristore roImageStore) (digest.Digest, bool, error) {
		d, err := ristore.BigDataDigest(id, key)
		if err == nil && d.Validate() == nil {
			return d, true, nil
		}
		return "", false, nil
	}); done {
		return res, err
	}
	return "", ErrDigestUnknown
}

func (s *store) ImageBigData(id, key string) ([]byte, error) {
	foundImage := false
	if res, done, err := readAllImageStores(s, func(store roImageStore) ([]byte, bool, error) {
		data, err := store.BigData(id, key)
		if err == nil {
			return data, true, nil
		}
		if store.Exists(id) {
			foundImage = true
		}
		return nil, false, nil
	}); done {
		return res, err
	}
	if foundImage {
		return nil, fmt.Errorf("locating item named %q for image with ID %q (consider removing the image to resolve the issue): %w", key, id, os.ErrNotExist)
	}
	return nil, fmt.Errorf("locating image with ID %q: %w", id, ErrImageUnknown)
}

// ListLayerBigData retrieves a list of the (possibly large) chunks of
// named data associated with an layer.
func (s *store) ListLayerBigData(id string) ([]string, error) {
	foundLayer := false
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) ([]string, bool, error) {
		data, err := store.BigDataNames(id)
		if err == nil {
			return data, true, nil
		}
		if store.Exists(id) {
			foundLayer = true
		}
		return nil, false, nil
	}); done {
		return res, err
	}
	if foundLayer {
		return nil, fmt.Errorf("locating big data for layer with ID %q: %w", id, os.ErrNotExist)
	}
	return nil, fmt.Errorf("locating layer with ID %q: %w", id, ErrLayerUnknown)
}

// LayerBigData retrieves a (possibly large) chunk of named data
// associated with a layer.
func (s *store) LayerBigData(id, key string) (io.ReadCloser, error) {
	foundLayer := false
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) (io.ReadCloser, bool, error) {
		data, err := store.BigData(id, key)
		if err == nil {
			return data, true, nil
		}
		if store.Exists(id) {
			foundLayer = true
		}
		return nil, false, nil
	}); done {
		return res, err
	}
	if foundLayer {
		return nil, fmt.Errorf("locating item named %q for layer with ID %q: %w", key, id, os.ErrNotExist)
	}
	return nil, fmt.Errorf("locating layer with ID %q: %w", id, ErrLayerUnknown)
}

// SetLayerBigData stores a (possibly large) chunk of named data
// associated with a layer.
func (s *store) SetLayerBigData(id, key string, data io.Reader) error {
	_, err := writeToLayerStore(s, func(store rwLayerStore) (struct{}, error) {
		return struct{}{}, store.SetBigData(id, key, data)
	})
	return err
}

func (s *store) SetImageBigData(id, key string, data []byte, digestManifest func([]byte) (digest.Digest, error)) error {
	_, err := writeToImageStore(s, func() (struct{}, error) {
		return struct{}{}, s.imageStore.SetBigData(id, key, data, digestManifest)
	})
	return err
}

func (s *store) ImageSize(id string) (int64, error) {
	layerStores, err := s.allLayerStores()
	if err != nil {
		return -1, err
	}
	for _, s := range layerStores {
		store := s
		if err := store.startReading(); err != nil {
			return -1, err
		}
		defer store.stopReading()
	}

	// Look for the image's record.
	var imageStore roBigDataStore
	var image *Image
	for _, s := range s.allImageStores() {
		store := s
		if err := store.startReading(); err != nil {
			return -1, err
		}
		defer store.stopReading()
		if image, err = store.Get(id); err == nil {
			imageStore = store
			break
		}
	}
	if image == nil {
		return -1, fmt.Errorf("locating image with ID %q: %w", id, ErrImageUnknown)
	}

	// Start with a list of the image's top layers, if it has any.
	queue := make(map[string]struct{})
	for _, layerID := range append([]string{image.TopLayer}, image.MappedTopLayers...) {
		if layerID != "" {
			queue[layerID] = struct{}{}
		}
	}
	visited := make(map[string]struct{})
	// Walk all of the layers.
	var size int64
	for len(visited) < len(queue) {
		for layerID := range queue {
			// Visit each layer only once.
			if _, ok := visited[layerID]; ok {
				continue
			}
			visited[layerID] = struct{}{}
			// Look for the layer and the store that knows about it.
			var layerStore roLayerStore
			var layer *Layer
			for _, store := range layerStores {
				if layer, err = store.Get(layerID); err == nil {
					layerStore = store
					break
				}
			}
			if layer == nil {
				return -1, fmt.Errorf("locating layer with ID %q: %w", layerID, ErrLayerUnknown)
			}
			// The UncompressedSize is only valid if there's a digest to go with it.
			n := layer.UncompressedSize
			if layer.UncompressedDigest == "" || n == -1 {
				// Compute the size.
				n, err = layerStore.DiffSize("", layer.ID)
				if err != nil {
					return -1, fmt.Errorf("size/digest of layer with ID %q could not be calculated: %w", layerID, err)
				}
			}
			// Count this layer.
			size += n
			// Make a note to visit the layer's parent if we haven't already.
			if layer.Parent != "" {
				queue[layer.Parent] = struct{}{}
			}
		}
	}

	// Count big data items.
	names, err := imageStore.BigDataNames(id)
	if err != nil {
		return -1, fmt.Errorf("reading list of big data items for image %q: %w", id, err)
	}
	for _, name := range names {
		n, err := imageStore.BigDataSize(id, name)
		if err != nil {
			return -1, fmt.Errorf("reading size of big data item %q for image %q: %w", name, id, err)
		}
		size += n
	}

	return size, nil
}

func (s *store) ContainerSize(id string) (int64, error) {
	layerStores, err := s.allLayerStores()
	if err != nil {
		return -1, err
	}
	for _, s := range layerStores {
		store := s
		if err := store.startReading(); err != nil {
			return -1, err
		}
		defer store.stopReading()
	}

	// Get the location of the container directory and container run directory.
	// Do it before we lock the container store because they do, too.
	cdir, err := s.ContainerDirectory(id)
	if err != nil {
		return -1, err
	}
	rdir, err := s.ContainerRunDirectory(id)
	if err != nil {
		return -1, err
	}

	return writeToContainerStore(s, func() (int64, error) { // Yes, s.containerStore.BigDataSize requires a write lock.
		// Read the container record.
		container, err := s.containerStore.Get(id)
		if err != nil {
			return -1, err
		}

		// Read the container's layer's size.
		var layer *Layer
		var size int64
		for _, store := range layerStores {
			if layer, err = store.Get(container.LayerID); err == nil {
				size, err = store.DiffSize("", layer.ID)
				if err != nil {
					return -1, fmt.Errorf("determining size of layer with ID %q: %w", layer.ID, err)
				}
				break
			}
		}
		if layer == nil {
			return -1, fmt.Errorf("locating layer with ID %q: %w", container.LayerID, ErrLayerUnknown)
		}

		// Count big data items.
		names, err := s.containerStore.BigDataNames(id)
		if err != nil {
			return -1, fmt.Errorf("reading list of big data items for container %q: %w", container.ID, err)
		}
		for _, name := range names {
			n, err := s.containerStore.BigDataSize(id, name)
			if err != nil {
				return -1, fmt.Errorf("reading size of big data item %q for container %q: %w", name, id, err)
			}
			size += n
		}

		// Count the size of our container directory and container run directory.
		n, err := directory.Size(cdir)
		if err != nil {
			return -1, err
		}
		size += n
		n, err = directory.Size(rdir)
		if err != nil {
			return -1, err
		}
		size += n

		return size, nil
	})
}

func (s *store) ListContainerBigData(id string) ([]string, error) {
	res, _, err := readContainerStore(s, func() ([]string, bool, error) {
		res, err := s.containerStore.BigDataNames(id)
		return res, true, err
	})
	return res, err
}

func (s *store) ContainerBigDataSize(id, key string) (int64, error) {
	return writeToContainerStore(s, func() (int64, error) { // Yes, BigDataSize requires a write lock.
		return s.containerStore.BigDataSize(id, key)
	})
}

func (s *store) ContainerBigDataDigest(id, key string) (digest.Digest, error) {
	return writeToContainerStore(s, func() (digest.Digest, error) { // Yes, BigDataDigest requires a write lock.
		return s.containerStore.BigDataDigest(id, key)
	})
}

func (s *store) ContainerBigData(id, key string) ([]byte, error) {
	res, _, err := readContainerStore(s, func() ([]byte, bool, error) {
		res, err := s.containerStore.BigData(id, key)
		return res, true, err
	})
	return res, err
}

func (s *store) SetContainerBigData(id, key string, data []byte) error {
	_, err := writeToContainerStore(s, func() (struct{}, error) {
		return struct{}{}, s.containerStore.SetBigData(id, key, data)
	})
	return err
}

func (s *store) Exists(id string) bool {
	found, _, err := readAllLayerStores(s, func(store roLayerStore) (bool, bool, error) {
		if store.Exists(id) {
			return true, true, nil
		}
		return false, false, nil
	})
	if err != nil {
		return false
	}
	if found {
		return true
	}

	found, _, err = readAllImageStores(s, func(store roImageStore) (bool, bool, error) {
		if store.Exists(id) {
			return true, true, nil
		}
		return false, false, nil
	})
	if err != nil {
		return false
	}
	if found {
		return true
	}

	found, _, err = readContainerStore(s, func() (bool, bool, error) {
		return s.containerStore.Exists(id), true, nil
	})
	if err != nil {
		return false
	}
	return found
}

func dedupeStrings(names []string) []string {
	seen := make(map[string]struct{})
	deduped := make([]string, 0, len(names))
	for _, name := range names {
		if _, wasSeen := seen[name]; !wasSeen {
			seen[name] = struct{}{}
			deduped = append(deduped, name)
		}
	}
	return deduped
}

func dedupeDigests(digests []digest.Digest) []digest.Digest {
	seen := make(map[digest.Digest]struct{})
	deduped := make([]digest.Digest, 0, len(digests))
	for _, d := range digests {
		if _, wasSeen := seen[d]; !wasSeen {
			seen[d] = struct{}{}
			deduped = append(deduped, d)
		}
	}
	return deduped
}

// Deprecated: Prone to race conditions, suggested alternatives are `AddNames` and `RemoveNames`.
func (s *store) SetNames(id string, names []string) error {
	return s.updateNames(id, names, setNames)
}

func (s *store) AddNames(id string, names []string) error {
	return s.updateNames(id, names, addNames)
}

func (s *store) RemoveNames(id string, names []string) error {
	return s.updateNames(id, names, removeNames)
}

func (s *store) updateNames(id string, names []string, op updateNameOperation) error {
	deduped := dedupeStrings(names)

	if found, err := writeToLayerStore(s, func(rlstore rwLayerStore) (bool, error) {
		if !rlstore.Exists(id) {
			return false, nil
		}
		return true, rlstore.updateNames(id, deduped, op)
	}); err != nil || found {
		return err
	}

	if err := s.imageStore.startWriting(); err != nil {
		return err
	}
	defer s.imageStore.stopWriting()
	if s.imageStore.Exists(id) {
		return s.imageStore.updateNames(id, deduped, op)
	}

	// Check if the id refers to a read-only image store -- we want to allow images in
	// read-only stores to have their names changed.
	for _, is := range s.roImageStores {
		store := is
		if err := store.startReading(); err != nil {
			return err
		}
		defer store.stopReading()
		if i, err := store.Get(id); err == nil {
			// "pull up" the image so that we can change its names list
			options := ImageOptions{
				CreationDate: i.Created,
				Digest:       i.Digest,
				Digests:      copySlicePreferringNil(i.Digests),
				Metadata:     i.Metadata,
				NamesHistory: copySlicePreferringNil(i.NamesHistory),
				Flags:        copyMapPreferringNil(i.Flags),
			}
			for _, key := range i.BigDataNames {
				data, err := store.BigData(id, key)
				if err != nil {
					return err
				}
				dataDigest, err := store.BigDataDigest(id, key)
				if err != nil {
					return err
				}
				options.BigData = append(options.BigData, ImageBigDataOption{
					Key:    key,
					Data:   data,
					Digest: dataDigest,
				})
			}
			_, err = s.imageStore.create(id, i.Names, i.TopLayer, options)
			if err != nil {
				return err
			}
			// now make the changes to the writeable image record's names list
			return s.imageStore.updateNames(id, deduped, op)
		}
	}

	if found, err := writeToContainerStore(s, func() (bool, error) {
		if !s.containerStore.Exists(id) {
			return false, nil
		}
		return true, s.containerStore.updateNames(id, deduped, op)
	}); err != nil || found {
		return err
	}

	return ErrLayerUnknown
}

func (s *store) Names(id string) ([]string, error) {
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) ([]string, bool, error) {
		if l, err := store.Get(id); l != nil && err == nil {
			return l.Names, true, nil
		}
		return nil, false, nil
	}); done {
		return res, err
	}

	if res, done, err := readAllImageStores(s, func(store roImageStore) ([]string, bool, error) {
		if i, err := store.Get(id); i != nil && err == nil {
			return i.Names, true, nil
		}
		return nil, false, nil
	}); done {
		return res, err
	}

	if res, done, err := readContainerStore(s, func() ([]string, bool, error) {
		if c, err := s.containerStore.Get(id); c != nil && err == nil {
			return c.Names, true, nil
		}
		return nil, false, nil
	}); done {
		return res, err
	}

	return nil, ErrLayerUnknown
}

func (s *store) Lookup(name string) (string, error) {
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) (string, bool, error) {
		if l, err := store.Get(name); l != nil && err == nil {
			return l.ID, true, nil
		}
		return "", false, nil
	}); done {
		return res, err
	}

	if res, done, err := readAllImageStores(s, func(store roImageStore) (string, bool, error) {
		if i, err := store.Get(name); i != nil && err == nil {
			return i.ID, true, nil
		}
		return "", false, nil
	}); done {
		return res, err
	}

	if res, done, err := readContainerStore(s, func() (string, bool, error) {
		if c, err := s.containerStore.Get(name); c != nil && err == nil {
			return c.ID, true, nil
		}
		return "", false, nil
	}); done {
		return res, err
	}

	return "", ErrLayerUnknown
}

func (s *store) DeleteLayer(id string) (retErr error) {
	cleanupFunctions := []tempdir.CleanupTempDirFunc{}
	defer func() {
		if cleanupErr := tempdir.CleanupTemporaryDirectories(cleanupFunctions...); cleanupErr != nil {
			retErr = errors.Join(cleanupErr, retErr)
		}
	}()
	return s.writeToAllStores(func(rlstore rwLayerStore) error {
		if rlstore.Exists(id) {
			if l, err := rlstore.Get(id); err != nil {
				id = l.ID
			}
			layers, err := rlstore.Layers()
			if err != nil {
				return err
			}
			for _, layer := range layers {
				if layer.Parent == id {
					return fmt.Errorf("used by layer %v: %w", layer.ID, ErrLayerHasChildren)
				}
			}
			images, err := s.imageStore.Images()
			if err != nil {
				return err
			}

			for _, image := range images {
				if image.TopLayer == id {
					return fmt.Errorf("layer %v used by image %v: %w", id, image.ID, ErrLayerUsedByImage)
				}
			}
			containers, err := s.containerStore.Containers()
			if err != nil {
				return err
			}
			for _, container := range containers {
				if container.LayerID == id {
					return fmt.Errorf("layer %v used by container %v: %w", id, container.ID, ErrLayerUsedByContainer)
				}
			}
			cf, err := rlstore.deferredDelete(id)
			cleanupFunctions = append(cleanupFunctions, cf...)
			if err != nil {
				return fmt.Errorf("delete layer %v: %w", id, err)
			}

			for _, image := range images {
				if stringutils.InSlice(image.MappedTopLayers, id) {
					if err = s.imageStore.removeMappedTopLayer(image.ID, id); err != nil {
						return fmt.Errorf("remove mapped top layer %v from image %v: %w", id, image.ID, err)
					}
				}
			}
			return nil
		}
		return ErrNotALayer
	})
}

func (s *store) DeleteImage(id string, commit bool) (layers []string, retErr error) {
	layersToRemove := []string{}
	cleanupFunctions := []tempdir.CleanupTempDirFunc{}
	defer func() {
		if cleanupErr := tempdir.CleanupTemporaryDirectories(cleanupFunctions...); cleanupErr != nil {
			retErr = errors.Join(cleanupErr, retErr)
		}
	}()
	if err := s.writeToAllStores(func(rlstore rwLayerStore) error {
		// Delete image from all available imagestores configured to be used.
		imageFound := false
		for _, is := range s.rwImageStores {
			if is != s.imageStore {
				// This is an additional writeable image store
				// so we must perform lock
				if err := is.startWriting(); err != nil {
					return err
				}
				defer is.stopWriting()
			}
			if !is.Exists(id) {
				continue
			}
			imageFound = true
			image, err := is.Get(id)
			if err != nil {
				return err
			}
			id = image.ID
			containers, err := s.containerStore.Containers()
			if err != nil {
				return err
			}
			aContainerByImage := make(map[string]string)
			for _, container := range containers {
				aContainerByImage[container.ImageID] = container.ID
			}
			if container, ok := aContainerByImage[id]; ok {
				return fmt.Errorf("image used by %v: %w", container, ErrImageUsedByContainer)
			}
			images, err := is.Images()
			if err != nil {
				return err
			}
			layers, err := rlstore.Layers()
			if err != nil {
				return err
			}
			childrenByParent := make(map[string][]string)
			for _, layer := range layers {
				childrenByParent[layer.Parent] = append(childrenByParent[layer.Parent], layer.ID)
			}
			otherImagesTopLayers := make(map[string]struct{})
			for _, img := range images {
				if img.ID != id {
					otherImagesTopLayers[img.TopLayer] = struct{}{}
					for _, layerID := range img.MappedTopLayers {
						otherImagesTopLayers[layerID] = struct{}{}
					}
				}
			}
			if commit {
				if err = is.Delete(id); err != nil {
					return err
				}
			}
			layer := image.TopLayer
			layersToRemoveMap := make(map[string]struct{})
			layersToRemove = append(layersToRemove, image.MappedTopLayers...)
			for _, mappedTopLayer := range image.MappedTopLayers {
				layersToRemoveMap[mappedTopLayer] = struct{}{}
			}
			for layer != "" {
				if s.containerStore.Exists(layer) {
					break
				}
				if _, used := otherImagesTopLayers[layer]; used {
					break
				}
				parent := ""
				if l, err := rlstore.Get(layer); err == nil {
					parent = l.Parent
				}
				hasChildrenNotBeingRemoved := func() bool {
					layersToCheck := []string{layer}
					if layer == image.TopLayer {
						layersToCheck = append(layersToCheck, image.MappedTopLayers...)
					}
					for _, layer := range layersToCheck {
						if childList := childrenByParent[layer]; len(childList) > 0 {
							for _, child := range childList {
								if _, childIsSlatedForRemoval := layersToRemoveMap[child]; childIsSlatedForRemoval {
									continue
								}
								return true
							}
						}
					}
					return false
				}
				if hasChildrenNotBeingRemoved() {
					break
				}
				layersToRemove = append(layersToRemove, layer)
				layersToRemoveMap[layer] = struct{}{}
				layer = parent
			}
		}
		if !imageFound {
			return ErrNotAnImage
		}
		if commit {
			for _, layer := range layersToRemove {
				cf, err := rlstore.deferredDelete(layer)
				cleanupFunctions = append(cleanupFunctions, cf...)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return layersToRemove, nil
}

func (s *store) DeleteContainer(id string) (retErr error) {
	cleanupFunctions := []tempdir.CleanupTempDirFunc{}
	defer func() {
		if cleanupErr := tempdir.CleanupTemporaryDirectories(cleanupFunctions...); cleanupErr != nil {
			retErr = errors.Join(cleanupErr, retErr)
		}
	}()
	return s.writeToAllStores(func(rlstore rwLayerStore) error {
		if !s.containerStore.Exists(id) {
			return ErrNotAContainer
		}

		container, err := s.containerStore.Get(id)
		if err != nil {
			return ErrNotAContainer
		}

		// delete the layer first, separately, so that if we get an
		// error while trying to do so, we don't go ahead and delete
		// the container record that refers to it, effectively losing
		// track of it
		if rlstore.Exists(container.LayerID) {
			cf, err := rlstore.deferredDelete(container.LayerID)
			cleanupFunctions = append(cleanupFunctions, cf...)
			if err != nil {
				return err
			}
		}

		var wg errgroup.Group

		middleDir := s.graphDriverName + "-containers"

		wg.Go(func() error {
			gcpath := filepath.Join(s.GraphRoot(), middleDir, container.ID)
			return system.EnsureRemoveAll(gcpath)
		})

		wg.Go(func() error {
			rcpath := filepath.Join(s.RunRoot(), middleDir, container.ID)
			return system.EnsureRemoveAll(rcpath)
		})

		if multierr := wg.Wait(); multierr != nil {
			return multierr
		}
		return s.containerStore.Delete(id)
	})
}

func (s *store) Delete(id string) (retErr error) {
	cleanupFunctions := []tempdir.CleanupTempDirFunc{}
	defer func() {
		if cleanupErr := tempdir.CleanupTemporaryDirectories(cleanupFunctions...); cleanupErr != nil {
			retErr = errors.Join(cleanupErr, retErr)
		}
	}()
	return s.writeToAllStores(func(rlstore rwLayerStore) error {
		if s.containerStore.Exists(id) {
			if container, err := s.containerStore.Get(id); err == nil {
				if rlstore.Exists(container.LayerID) {
					cf, err := rlstore.deferredDelete(container.LayerID)
					cleanupFunctions = append(cleanupFunctions, cf...)
					if err != nil {
						return err
					}
					if err = s.containerStore.Delete(id); err != nil {
						return err
					}
					middleDir := s.graphDriverName + "-containers"
					gcpath := filepath.Join(s.GraphRoot(), middleDir, container.ID, "userdata")
					if err = os.RemoveAll(gcpath); err != nil {
						return err
					}
					rcpath := filepath.Join(s.RunRoot(), middleDir, container.ID, "userdata")
					if err = os.RemoveAll(rcpath); err != nil {
						return err
					}
					return nil
				}
				return ErrNotALayer
			}
		}
		if s.imageStore.Exists(id) {
			return s.imageStore.Delete(id)
		}
		if rlstore.Exists(id) {
			cf, err := rlstore.deferredDelete(id)
			cleanupFunctions = append(cleanupFunctions, cf...)
			return err
		}
		return ErrLayerUnknown
	})
}

func (s *store) Wipe() error {
	return s.writeToAllStores(func(rlstore rwLayerStore) error {
		if err := s.containerStore.Wipe(); err != nil {
			return err
		}
		if err := s.imageStore.Wipe(); err != nil {
			return err
		}
		return rlstore.Wipe()
	})
}

func (s *store) Status() ([][2]string, error) {
	rlstore, err := s.getLayerStore()
	if err != nil {
		return nil, err
	}
	return rlstore.Status()
}

//go:embed VERSION
var storageVersion string

func (s *store) Version() ([][2]string, error) {
	if trimmedVersion := strings.TrimSpace(storageVersion); trimmedVersion != "" {
		return [][2]string{{"Version", trimmedVersion}}, nil
	}
	return [][2]string{}, nil
}

func (s *store) MountImage(id string, mountOpts []string, mountLabel string) (string, error) {
	if err := validateMountOptions(mountOpts); err != nil {
		return "", err
	}

	// We need to make sure the home mount is present when the Mount is done, which happens by possibly reinitializing the graph driver
	// in startUsingGraphDriver().
	if err := s.startUsingGraphDriver(); err != nil {
		return "", err
	}
	defer s.stopUsingGraphDriver()

	rlstore, lstores, err := s.bothLayerStoreKindsLocked()
	if err != nil {
		return "", err
	}
	var imageHomeStore roImageStore

	if err := rlstore.startWriting(); err != nil {
		return "", err
	}
	defer rlstore.stopWriting()
	for _, s := range lstores {
		if err := s.startReading(); err != nil {
			return "", err
		}
		defer s.stopReading()
	}
	if err := s.imageStore.startWriting(); err != nil {
		return "", err
	}
	defer s.imageStore.stopWriting()

	cimage, err := s.imageStore.Get(id)
	if err == nil {
		imageHomeStore = s.imageStore
	} else {
		for _, s := range s.roImageStores {
			if err := s.startReading(); err != nil {
				return "", err
			}
			defer s.stopReading()
			cimage, err = s.Get(id)
			if err == nil {
				imageHomeStore = s
				break
			}
		}
	}
	if cimage == nil {
		return "", fmt.Errorf("locating image with ID %q: %w", id, ErrImageUnknown)
	}

	idmappingsOpts := types.IDMappingOptions{
		HostUIDMapping: true,
		HostGIDMapping: true,
	}
	ilayer, err := s.imageTopLayerForMapping(cimage, imageHomeStore, rlstore, lstores, idmappingsOpts)
	if err != nil {
		return "", err
	}

	if len(ilayer.UIDMap) > 0 || len(ilayer.GIDMap) > 0 {
		return "", fmt.Errorf("cannot create an image with canonical UID/GID mappings in a read-only store")
	}

	options := drivers.MountOpts{
		MountLabel: mountLabel,
		Options:    append(mountOpts, "ro"),
	}
	return rlstore.Mount(ilayer.ID, options)
}

func (s *store) Mount(id, mountLabel string) (string, error) {
	options := drivers.MountOpts{
		MountLabel: mountLabel,
	}
	// check if `id` is a container, then grab the LayerID, uidmap and gidmap, along with
	// otherwise we assume the id is a LayerID and attempt to mount it.
	if container, err := s.Container(id); err == nil {
		id = container.LayerID
		options.UidMaps = container.UIDMap
		options.GidMaps = container.GIDMap
		options.Options = container.MountOpts()
		if !s.disableVolatile {
			if v, found := container.Flags[volatileFlag]; found {
				if b, ok := v.(bool); ok {
					options.Volatile = b
				}
			}
		}
	}

	// We need to make sure the home mount is present when the Mount is done, which happens by possibly reinitializing the graph driver
	// in startUsingGraphDriver().
	if err := s.startUsingGraphDriver(); err != nil {
		return "", err
	}
	defer s.stopUsingGraphDriver()

	rlstore, lstores, err := s.bothLayerStoreKindsLocked()
	if err != nil {
		return "", err
	}
	if options.UidMaps != nil || options.GidMaps != nil {
		options.DisableShifting = !s.canUseShifting(options.UidMaps, options.GidMaps)
	}

	if err := rlstore.startWriting(); err != nil {
		return "", err
	}
	defer rlstore.stopWriting()
	if rlstore.Exists(id) {
		return rlstore.Mount(id, options)
	}

	// check if the layer is in a read-only store, and return a better error message
	for _, store := range lstores {
		if err := store.startReading(); err != nil {
			return "", err
		}
		exists := store.Exists(id)
		store.stopReading()
		if exists {
			return "", fmt.Errorf("mounting read/only store images is not allowed: %w", ErrStoreIsReadOnly)
		}
	}

	return "", ErrLayerUnknown
}

func (s *store) Mounted(id string) (int, error) {
	if layerID, err := s.ContainerLayerID(id); err == nil {
		id = layerID
	}
	rlstore, err := s.getLayerStore()
	if err != nil {
		return 0, err
	}
	if err := rlstore.startReading(); err != nil {
		return 0, err
	}
	defer rlstore.stopReading()

	return rlstore.Mounted(id)
}

func (s *store) UnmountImage(id string, force bool) (bool, error) {
	img, err := s.Image(id)
	if err != nil {
		return false, err
	}

	return writeToLayerStore(s, func(lstore rwLayerStore) (bool, error) {
		for _, layerID := range img.MappedTopLayers {
			l, err := lstore.Get(layerID)
			if err != nil {
				if err == ErrLayerUnknown {
					continue
				}
				return false, err
			}
			// check if the layer with the canonical mapping is in the mapped top layers
			if len(l.UIDMap) == 0 && len(l.GIDMap) == 0 {
				return lstore.unmount(l.ID, force, false)
			}
		}
		return lstore.unmount(img.TopLayer, force, false)
	})
}

func (s *store) Unmount(id string, force bool) (bool, error) {
	if layerID, err := s.ContainerLayerID(id); err == nil {
		id = layerID
	}
	return writeToLayerStore(s, func(rlstore rwLayerStore) (bool, error) {
		if rlstore.Exists(id) {
			return rlstore.unmount(id, force, false)
		}
		return false, ErrLayerUnknown
	})
}

func (s *store) Changes(from, to string) ([]archive.Change, error) {
	// NaiveDiff could cause mounts to happen without a lock, so be safe
	// and treat the .Diff operation as a Mount.
	// We need to make sure the home mount is present when the Mount is done, which happens by possibly reinitializing the graph driver
	// in startUsingGraphDriver().
	if err := s.startUsingGraphDriver(); err != nil {
		return nil, err
	}
	defer s.stopUsingGraphDriver()

	rlstore, lstores, err := s.bothLayerStoreKindsLocked()
	if err != nil {
		return nil, err
	}

	// While the general rules require the layer store to only be locked RO (apart from known LOCKING BUGs)
	// the overlay driver requires the primary layer store to be locked RW; see
	// drivers/overlay.Driver.getMergedDir.
	if err := rlstore.startWriting(); err != nil {
		return nil, err
	}
	if rlstore.Exists(to) {
		res, err := rlstore.Changes(from, to)
		rlstore.stopWriting()
		return res, err
	}
	rlstore.stopWriting()

	for _, s := range lstores {
		store := s
		if err := store.startReading(); err != nil {
			return nil, err
		}
		if store.Exists(to) {
			res, err := store.Changes(from, to)
			store.stopReading()
			return res, err
		}
		store.stopReading()
	}
	return nil, ErrLayerUnknown
}

func (s *store) DiffSize(from, to string) (int64, error) {
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) (int64, bool, error) {
		if store.Exists(to) {
			res, err := store.DiffSize(from, to)
			return res, true, err
		}
		return -1, false, nil
	}); done {
		if err != nil {
			return -1, err
		}
		return res, nil
	}
	return -1, ErrLayerUnknown
}

func (s *store) Diff(from, to string, options *DiffOptions) (io.ReadCloser, error) {
	// NaiveDiff could cause mounts to happen without a lock, so be safe
	// and treat the .Diff operation as a Mount.
	// We need to make sure the home mount is present when the Mount is done, which happens by possibly reinitializing the graph driver
	// in startUsingGraphDriver().
	if err := s.startUsingGraphDriver(); err != nil {
		return nil, err
	}
	defer s.stopUsingGraphDriver()

	rlstore, lstores, err := s.bothLayerStoreKindsLocked()
	if err != nil {
		return nil, err
	}

	// While the general rules require the layer store to only be locked RO (apart from known LOCKING BUGs)
	// the overlay driver requires the primary layer store to be locked RW; see
	// drivers/overlay.Driver.getMergedDir.
	if err := rlstore.startWriting(); err != nil {
		return nil, err
	}
	if rlstore.Exists(to) {
		rc, err := rlstore.Diff(from, to, options)
		if rc != nil && err == nil {
			wrapped := ioutils.NewReadCloserWrapper(rc, func() error {
				err := rc.Close()
				rlstore.stopWriting()
				return err
			})
			return wrapped, nil
		}
		rlstore.stopWriting()
		return rc, err
	}
	rlstore.stopWriting()

	for _, s := range lstores {
		store := s
		if err := store.startReading(); err != nil {
			return nil, err
		}
		if store.Exists(to) {
			rc, err := store.Diff(from, to, options)
			if rc != nil && err == nil {
				wrapped := ioutils.NewReadCloserWrapper(rc, func() error {
					err := rc.Close()
					store.stopReading()
					return err
				})
				return wrapped, nil
			}
			store.stopReading()
			return rc, err
		}
		store.stopReading()
	}
	return nil, ErrLayerUnknown
}

func (s *store) ApplyStagedLayer(args ApplyStagedLayerOptions) (*Layer, error) {
	defer func() {
		if args.DiffOutput.TarSplit != nil {
			args.DiffOutput.TarSplit.Close()
			args.DiffOutput.TarSplit = nil
		}
	}()
	rlstore, rlstores, err := s.bothLayerStoreKinds()
	if err != nil {
		return nil, err
	}
	if err := rlstore.startWriting(); err != nil {
		return nil, err
	}
	defer rlstore.stopWriting()

	layer, err := rlstore.Get(args.ID)
	if err != nil && !errors.Is(err, ErrLayerUnknown) {
		return layer, err
	}
	if err == nil {
		// This code path exists only for cmd/containers/storage.applyDiffUsingStagingDirectory; we have tests that
		// assume layer creation and applying a staged layer are separate steps. Production pull code always uses the
		// other path, where layer creation is atomic.
		return layer, rlstore.applyDiffFromStagingDirectory(args.ID, args.DiffOutput, args.DiffOptions)
	}

	// if the layer doesn't exist yet, try to create it.

	slo := stagedLayerOptions{
		DiffOutput:  args.DiffOutput,
		DiffOptions: args.DiffOptions,
	}
	layer, _, err = s.putLayer(rlstore, rlstores, args.ID, args.ParentLayer, args.Names, args.MountLabel, args.Writeable, args.LayerOptions, nil, &slo)
	return layer, err
}

func (s *store) CleanupStagedLayer(diffOutput *drivers.DriverWithDifferOutput) error {
	if diffOutput.TarSplit != nil {
		diffOutput.TarSplit.Close()
		diffOutput.TarSplit = nil
	}
	_, err := writeToLayerStore(s, func(rlstore rwLayerStore) (struct{}, error) {
		return struct{}{}, rlstore.CleanupStagingDirectory(diffOutput.Target)
	})
	return err
}

func (s *store) PrepareStagedLayer(options *drivers.ApplyDiffWithDifferOpts, differ drivers.Differ) (*drivers.DriverWithDifferOutput, error) {
	rlstore, err := s.getLayerStore()
	if err != nil {
		return nil, err
	}
	return rlstore.applyDiffWithDifferNoLock(options, differ)
}

func (s *store) DifferTarget(id string) (string, error) {
	return writeToLayerStore(s, func(rlstore rwLayerStore) (string, error) {
		if rlstore.Exists(id) {
			return rlstore.DifferTarget(id)
		}
		return "", ErrLayerUnknown
	})
}

func (s *store) ApplyDiff(to string, diff io.Reader) (int64, error) {
	return writeToLayerStore(s, func(rlstore rwLayerStore) (int64, error) {
		if rlstore.Exists(to) {
			return rlstore.ApplyDiff(to, diff)
		}
		return -1, ErrLayerUnknown
	})
}

func (s *store) layersByMappedDigest(m func(roLayerStore, digest.Digest) ([]Layer, error), d digest.Digest) ([]Layer, error) {
	var layers []Layer
	if _, _, err := readAllLayerStores(s, func(store roLayerStore) (struct{}, bool, error) {
		storeLayers, err := m(store, d)
		if err != nil {
			if !errors.Is(err, ErrLayerUnknown) {
				return struct{}{}, true, err
			}
			return struct{}{}, false, nil
		}
		layers = append(layers, storeLayers...)
		return struct{}{}, false, nil
	}); err != nil {
		return nil, err
	}
	if len(layers) == 0 {
		return nil, ErrLayerUnknown
	}
	return layers, nil
}

func (s *store) LayersByCompressedDigest(d digest.Digest) ([]Layer, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("looking for compressed layers matching digest %q: %w", d, err)
	}
	return s.layersByMappedDigest(func(r roLayerStore, d digest.Digest) ([]Layer, error) { return r.LayersByCompressedDigest(d) }, d)
}

func (s *store) LayersByUncompressedDigest(d digest.Digest) ([]Layer, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("looking for layers matching digest %q: %w", d, err)
	}
	return s.layersByMappedDigest(func(r roLayerStore, d digest.Digest) ([]Layer, error) { return r.LayersByUncompressedDigest(d) }, d)
}

func (s *store) LayersByTOCDigest(d digest.Digest) ([]Layer, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("looking for TOC matching digest %q: %w", d, err)
	}
	return s.layersByMappedDigest(func(r roLayerStore, d digest.Digest) ([]Layer, error) { return r.LayersByTOCDigest(d) }, d)
}

func (s *store) LayerSize(id string) (int64, error) {
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) (int64, bool, error) {
		if store.Exists(id) {
			res, err := store.Size(id)
			return res, true, err
		}
		return -1, false, nil
	}); done {
		if err != nil {
			return -1, err
		}
		return res, nil
	}
	return -1, ErrLayerUnknown
}

func (s *store) LayerParentOwners(id string) ([]int, []int, error) {
	rlstore, err := s.getLayerStore()
	if err != nil {
		return nil, nil, err
	}
	if err := rlstore.startReading(); err != nil {
		return nil, nil, err
	}
	defer rlstore.stopReading()
	if rlstore.Exists(id) {
		return rlstore.ParentOwners(id)
	}
	return nil, nil, ErrLayerUnknown
}

func (s *store) ContainerParentOwners(id string) ([]int, []int, error) {
	rlstore, err := s.getLayerStore()
	if err != nil {
		return nil, nil, err
	}
	if err := rlstore.startReading(); err != nil {
		return nil, nil, err
	}
	defer rlstore.stopReading()
	if err := s.containerStore.startReading(); err != nil {
		return nil, nil, err
	}
	defer s.containerStore.stopReading()
	container, err := s.containerStore.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if rlstore.Exists(container.LayerID) {
		return rlstore.ParentOwners(container.LayerID)
	}
	return nil, nil, ErrLayerUnknown
}

func (s *store) Layers() ([]Layer, error) {
	var layers []Layer
	if _, done, err := readAllLayerStores(s, func(store roLayerStore) (struct{}, bool, error) {
		storeLayers, err := store.Layers()
		if err != nil {
			return struct{}{}, true, err
		}
		layers = append(layers, storeLayers...)
		return struct{}{}, false, nil
	}); done {
		return nil, err
	}
	return layers, nil
}

func (s *store) Images() ([]Image, error) {
	var images []Image
	if _, _, err := readAllImageStores(s, func(store roImageStore) (struct{}, bool, error) {
		storeImages, err := store.Images()
		if err != nil {
			return struct{}{}, true, err
		}
		images = append(images, storeImages...)
		return struct{}{}, false, nil
	}); err != nil {
		return nil, err
	}
	return images, nil
}

func (s *store) Containers() ([]Container, error) {
	res, _, err := readContainerStore(s, func() ([]Container, bool, error) {
		res, err := s.containerStore.Containers()
		return res, true, err
	})
	return res, err
}

func (s *store) Layer(id string) (*Layer, error) {
	if res, done, err := readAllLayerStores(s, func(store roLayerStore) (*Layer, bool, error) {
		layer, err := store.Get(id)
		if err == nil {
			return layer, true, nil
		}
		return nil, false, nil
	}); done {
		return res, err
	}
	return nil, ErrLayerUnknown
}

func (s *store) LookupAdditionalLayer(tocDigest digest.Digest, imageref string) (AdditionalLayer, error) {
	var adriver drivers.AdditionalLayerStoreDriver
	if err := func() error { // A scope for defer
		if err := s.startUsingGraphDriver(); err != nil {
			return err
		}
		defer s.stopUsingGraphDriver()
		a, ok := s.graphDriver.(drivers.AdditionalLayerStoreDriver)
		if !ok {
			return ErrLayerUnknown
		}
		adriver = a
		return nil
	}(); err != nil {
		return nil, err
	}

	al, err := adriver.LookupAdditionalLayer(tocDigest, imageref)
	if err != nil {
		if errors.Is(err, drivers.ErrLayerUnknown) {
			return nil, ErrLayerUnknown
		}
		return nil, err
	}
	info, err := al.Info()
	if err != nil {
		return nil, err
	}
	defer info.Close()
	var layer Layer
	if err := json.NewDecoder(info).Decode(&layer); err != nil {
		return nil, err
	}
	return &additionalLayer{&layer, al, s}, nil
}

type additionalLayer struct {
	layer   *Layer
	handler drivers.AdditionalLayer
	s       *store
}

func (al *additionalLayer) TOCDigest() digest.Digest {
	return al.layer.TOCDigest
}

func (al *additionalLayer) CompressedSize() int64 {
	return al.layer.CompressedSize
}

func (al *additionalLayer) PutAs(id, parent string, names []string) (*Layer, error) {
	rlstore, rlstores, err := al.s.bothLayerStoreKinds()
	if err != nil {
		return nil, err
	}
	if err := rlstore.startWriting(); err != nil {
		return nil, err
	}
	defer rlstore.stopWriting()

	var parentLayer *Layer
	if parent != "" {
		for _, lstore := range append([]roLayerStore{rlstore}, rlstores...) {
			if lstore != rlstore {
				if err := lstore.startReading(); err != nil {
					return nil, err
				}
				defer lstore.stopReading()
			}
			parentLayer, err = lstore.Get(parent)
			if err == nil {
				break
			}
		}
		if parentLayer == nil {
			return nil, ErrLayerUnknown
		}
	}

	return rlstore.PutAdditionalLayer(id, parentLayer, names, al.handler)
}

func (al *additionalLayer) Release() {
	al.handler.Release()
}

func (s *store) Image(id string) (*Image, error) {
	if res, done, err := readAllImageStores(s, func(store roImageStore) (*Image, bool, error) {
		image, err := store.Get(id)
		if err == nil {
			if store != s.imageStore {
				// found it in a read-only store - readAllImageStores() still has the writeable store locked for reading
				if _, localErr := s.imageStore.Get(image.ID); localErr == nil {
					// if the lookup key was a name, and we found the image in a read-only
					// store, but we have an entry with the same ID in the read-write store,
					// then the name was removed when we duplicated the image's
					// record into writable storage, so we should ignore this entry
					return nil, false, nil
				}
			}
			return image, true, nil
		}
		return nil, false, nil
	}); done {
		return res, err
	}
	return nil, fmt.Errorf("locating image with ID %q: %w", id, ErrImageUnknown)
}

func (s *store) ImagesByTopLayer(id string) ([]*Image, error) {
	layer, err := s.Layer(id)
	if err != nil {
		return nil, err
	}

	images := []*Image{}
	if _, _, err := readAllImageStores(s, func(store roImageStore) (struct{}, bool, error) {
		imageList, err := store.Images()
		if err != nil {
			return struct{}{}, true, err
		}
		for _, image := range imageList {
			if image.TopLayer == layer.ID || stringutils.InSlice(image.MappedTopLayers, layer.ID) {
				images = append(images, &image)
			}
		}
		return struct{}{}, false, nil
	}); err != nil {
		return nil, err
	}
	return images, nil
}

func (s *store) ImagesByDigest(d digest.Digest) ([]*Image, error) {
	images := []*Image{}
	if _, _, err := readAllImageStores(s, func(store roImageStore) (struct{}, bool, error) {
		imageList, err := store.ByDigest(d)
		if err != nil && !errors.Is(err, ErrImageUnknown) {
			return struct{}{}, true, err
		}
		images = append(images, imageList...)
		return struct{}{}, false, nil
	}); err != nil {
		return nil, err
	}
	return images, nil
}

func (s *store) Container(id string) (*Container, error) {
	res, _, err := readContainerStore(s, func() (*Container, bool, error) {
		res, err := s.containerStore.Get(id)
		return res, true, err
	})
	return res, err
}

func (s *store) ContainerLayerID(id string) (string, error) {
	container, _, err := readContainerStore(s, func() (*Container, bool, error) {
		res, err := s.containerStore.Get(id)
		return res, true, err
	})
	if err != nil {
		return "", err
	}
	return container.LayerID, nil
}

func (s *store) ContainerByLayer(id string) (*Container, error) {
	layer, err := s.Layer(id)
	if err != nil {
		return nil, err
	}
	containerList, _, err := readContainerStore(s, func() ([]Container, bool, error) {
		res, err := s.containerStore.Containers()
		return res, true, err
	})
	if err != nil {
		return nil, err
	}
	for _, container := range containerList {
		if container.LayerID == layer.ID {
			return &container, nil
		}
	}

	return nil, ErrContainerUnknown
}

func (s *store) ImageDirectory(id string) (string, error) {
	foundImage := false
	if res, done, err := readAllImageStores(s, func(store roImageStore) (string, bool, error) {
		if store.Exists(id) {
			foundImage = true
		}
		middleDir := s.graphDriverName + "-images"
		gipath := filepath.Join(s.GraphRoot(), middleDir, id, "userdata")
		if err := os.MkdirAll(gipath, 0o700); err != nil {
			return "", true, err
		}
		return gipath, true, nil
	}); done {
		return res, err
	}
	if foundImage {
		return "", fmt.Errorf("locating image with ID %q (consider removing the image to resolve the issue): %w", id, os.ErrNotExist)
	}
	return "", fmt.Errorf("locating image with ID %q: %w", id, ErrImageUnknown)
}

func (s *store) ContainerDirectory(id string) (string, error) {
	res, _, err := readContainerStore(s, func() (string, bool, error) {
		id, err := s.containerStore.Lookup(id)
		if err != nil {
			return "", true, err
		}

		middleDir := s.graphDriverName + "-containers"
		gcpath := filepath.Join(s.GraphRoot(), middleDir, id, "userdata")
		if err := os.MkdirAll(gcpath, 0o700); err != nil {
			return "", true, err
		}
		return gcpath, true, nil
	})
	return res, err
}

func (s *store) ImageRunDirectory(id string) (string, error) {
	foundImage := false
	if res, done, err := readAllImageStores(s, func(store roImageStore) (string, bool, error) {
		if store.Exists(id) {
			foundImage = true
		}

		middleDir := s.graphDriverName + "-images"
		rcpath := filepath.Join(s.RunRoot(), middleDir, id, "userdata")
		if err := os.MkdirAll(rcpath, 0o700); err != nil {
			return "", true, err
		}
		return rcpath, true, nil
	}); done {
		return res, err
	}
	if foundImage {
		return "", fmt.Errorf("locating image with ID %q (consider removing the image to resolve the issue): %w", id, os.ErrNotExist)
	}
	return "", fmt.Errorf("locating image with ID %q: %w", id, ErrImageUnknown)
}

func (s *store) ContainerRunDirectory(id string) (string, error) {
	res, _, err := readContainerStore(s, func() (string, bool, error) {
		id, err := s.containerStore.Lookup(id)
		if err != nil {
			return "", true, err
		}

		middleDir := s.graphDriverName + "-containers"
		rcpath := filepath.Join(s.RunRoot(), middleDir, id, "userdata")
		if err := os.MkdirAll(rcpath, 0o700); err != nil {
			return "", true, err
		}
		return rcpath, true, nil
	})
	return res, err
}

func (s *store) SetContainerDirectoryFile(id, file string, data []byte) error {
	dir, err := s.ContainerDirectory(id)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o700)
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(dir, file), data, 0o600)
}

func (s *store) FromContainerDirectory(id, file string) ([]byte, error) {
	dir, err := s.ContainerDirectory(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, file))
}

func (s *store) SetContainerRunDirectoryFile(id, file string, data []byte) error {
	dir, err := s.ContainerRunDirectory(id)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o700)
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filepath.Join(dir, file), data, 0o600)
}

func (s *store) FromContainerRunDirectory(id, file string) ([]byte, error) {
	dir, err := s.ContainerRunDirectory(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, file))
}

func (s *store) Shutdown(force bool) ([]string, error) {
	mounted := []string{}

	if err := s.startUsingGraphDriver(); err != nil {
		return mounted, err
	}
	defer s.stopUsingGraphDriver()

	rlstore, err := s.getLayerStoreLocked()
	if err != nil {
		return mounted, err
	}
	if err := rlstore.startWriting(); err != nil {
		return nil, err
	}
	defer rlstore.stopWriting()

	layers, err := rlstore.Layers()
	if err != nil {
		return mounted, err
	}
	for _, layer := range layers {
		if layer.MountCount == 0 {
			continue
		}
		mounted = append(mounted, layer.ID)
		if force {
			for {
				_, err2 := rlstore.unmount(layer.ID, force, true)
				if err2 == ErrLayerNotMounted {
					break
				}
				if err2 != nil {
					if err == nil {
						err = err2
					}
					break
				}
			}
		}
	}
	if len(mounted) > 0 && err == nil {
		err = fmt.Errorf("a layer is mounted: %w", ErrLayerUsedByContainer)
	}
	if err == nil {
		// We don’t retain the lastWrite value, and treat this update as if someone else did the .Cleanup(),
		// so that we reload after a .Shutdown() the same way other processes would.
		// Shutdown() is basically an error path, so reliability is more important than performance.
		if _, err2 := s.graphLock.RecordWrite(); err2 != nil {
			err = fmt.Errorf("graphLock.RecordWrite failed: %w", err2)
		}
		// Do the Cleanup() only after we are sure that the change was recorded with RecordWrite(), so that
		// the next user picks it.
		if err == nil {
			err = s.graphDriver.Cleanup()
		}
	}
	return mounted, err
}

// Convert a BigData key name into an acceptable file name.
func makeBigDataBaseName(key string) string {
	reader := strings.NewReader(key)
	for reader.Len() > 0 {
		ch, size, err := reader.ReadRune()
		if err != nil || size != 1 {
			break
		}
		if ch != '.' && (ch < '0' || ch > '9') && (ch < 'a' || ch > 'z') {
			break
		}
	}
	if reader.Len() > 0 {
		return "=" + base64.StdEncoding.EncodeToString([]byte(key))
	}
	return key
}

func stringSliceWithoutValue(slice []string, value string) []string {
	return slices.DeleteFunc(slices.Clone(slice), func(v string) bool {
		return v == value
	})
}

// copySlicePreferringNil returns a copy of the slice.
// If s is empty, a nil is returned.
func copySlicePreferringNil[S ~[]E, E any](s S) S {
	if len(s) == 0 {
		return nil
	}
	return slices.Clone(s)
}

// copyMapPreferringNil returns a shallow clone of map m.
// If m is empty, a nil is returned.
//
// (As of, e.g., Go 1.23, maps.Clone preserves nil, but that’s not a documented promise;
// and this function turns even non-nil empty maps into nil.)
func copyMapPreferringNil[K comparable, V any](m map[K]V) map[K]V {
	if len(m) == 0 {
		return nil
	}
	return maps.Clone(m)
}

// newMapFrom returns a shallow clone of map m.
// If m is empty, an empty map is allocated and returned.
func newMapFrom[K comparable, V any](m map[K]V) map[K]V {
	if len(m) == 0 {
		return make(map[K]V, 0)
	}
	return maps.Clone(m)
}

func copyImageBigDataOptionSlice(slice []ImageBigDataOption) []ImageBigDataOption {
	ret := make([]ImageBigDataOption, len(slice))
	for i := range slice {
		ret[i].Key = slice[i].Key
		ret[i].Data = slices.Clone(slice[i].Data)
		ret[i].Digest = slice[i].Digest
	}
	return ret
}

func copyContainerBigDataOptionSlice(slice []ContainerBigDataOption) []ContainerBigDataOption {
	ret := make([]ContainerBigDataOption, len(slice))
	for i := range slice {
		ret[i].Key = slice[i].Key
		ret[i].Data = slices.Clone(slice[i].Data)
	}
	return ret
}

// AutoUserNsMinSize is the minimum size for automatically created user namespaces
const AutoUserNsMinSize = 1024

// AutoUserNsMaxSize is the maximum size for automatically created user namespaces
const AutoUserNsMaxSize = 65536

// RootAutoUserNsUser is the default user used for root containers when automatically
// creating a user namespace.
const RootAutoUserNsUser = "containers"

// SetDefaultConfigFilePath sets the default configuration to the specified path, and loads the file.
// Deprecated: Use types.SetDefaultConfigFilePath, which can return an error.
func SetDefaultConfigFilePath(path string) {
	_ = types.SetDefaultConfigFilePath(path)
}

// DefaultConfigFile returns the path to the storage config file used
func DefaultConfigFile() (string, error) {
	return types.DefaultConfigFile()
}

// ReloadConfigurationFile parses the specified configuration file and overrides
// the configuration in storeOptions.
// Deprecated: Use types.ReloadConfigurationFile, which can return an error.
func ReloadConfigurationFile(configFile string, storeOptions *types.StoreOptions) {
	_ = types.ReloadConfigurationFile(configFile, storeOptions)
}

// GetDefaultMountOptions returns the default mountoptions defined in container/storage
func GetDefaultMountOptions() ([]string, error) {
	defaultStoreOptions, err := types.Options()
	if err != nil {
		return nil, err
	}
	return GetMountOptions(defaultStoreOptions.GraphDriverName, defaultStoreOptions.GraphDriverOptions)
}

// GetMountOptions returns the mountoptions for the specified driver and graphDriverOptions
func GetMountOptions(driver string, graphDriverOptions []string) ([]string, error) {
	mountOpts := []string{
		".mountopt",
		fmt.Sprintf("%s.mountopt", driver),
	}
	for _, option := range graphDriverOptions {
		key, val, err := parsers.ParseKeyValueOpt(option)
		if err != nil {
			return nil, err
		}
		key = strings.ToLower(key)
		if slices.Contains(mountOpts, key) {
			return strings.Split(val, ","), nil
		}
	}
	return nil, nil
}

// Free removes the store from the list of stores
func (s *store) Free() {
	if i := slices.Index(stores, s); i != -1 {
		stores = slices.Delete(stores, i, i+1)
	}
}

// Tries to clean up old unreferenced container leftovers. returns the first error
// but continues as far as it can
func (s *store) GarbageCollect() error {
	_, firstErr := writeToContainerStore(s, func() (struct{}, error) {
		return struct{}{}, s.containerStore.GarbageCollect()
	})

	_, moreErr := writeToImageStore(s, func() (struct{}, error) {
		return struct{}{}, s.imageStore.GarbageCollect()
	})
	if firstErr == nil {
		firstErr = moreErr
	}

	_, moreErr = writeToLayerStore(s, func(rlstore rwLayerStore) (struct{}, error) {
		return struct{}{}, rlstore.GarbageCollect()
	})
	if firstErr == nil {
		firstErr = moreErr
	}

	return firstErr
}

// List returns a MultiListResult structure that contains layer, image, or container
// extracts according to the values in MultiListOptions.
func (s *store) MultiList(options MultiListOptions) (MultiListResult, error) {
	// TODO: Possible optimization: Deduplicate content from multiple stores.
	out := MultiListResult{}

	if options.Layers {
		layerStores, err := s.allLayerStores()
		if err != nil {
			return MultiListResult{}, err
		}
		for _, roStore := range layerStores {
			if err := roStore.startReading(); err != nil {
				return MultiListResult{}, err
			}
			defer roStore.stopReading()
			layers, err := roStore.Layers()
			if err != nil {
				return MultiListResult{}, err
			}
			out.Layers = append(out.Layers, layers...)
		}
	}

	if options.Images {
		for _, roStore := range s.allImageStores() {
			if err := roStore.startReading(); err != nil {
				return MultiListResult{}, err
			}
			defer roStore.stopReading()

			images, err := roStore.Images()
			if err != nil {
				return MultiListResult{}, err
			}
			out.Images = append(out.Images, images...)
		}
	}

	if options.Containers {
		containers, _, err := readContainerStore(s, func() ([]Container, bool, error) {
			res, err := s.containerStore.Containers()
			return res, true, err
		})
		if err != nil {
			return MultiListResult{}, err
		}
		out.Containers = append(out.Containers, containers...)
	}
	return out, nil
}

// Dedup deduplicates layers in the store.
func (s *store) Dedup(req DedupArgs) (drivers.DedupResult, error) {
	imgs, err := s.Images()
	if err != nil {
		return drivers.DedupResult{}, err
	}
	var topLayers []string
	for _, i := range imgs {
		topLayers = append(topLayers, i.TopLayer)
		topLayers = append(topLayers, i.MappedTopLayers...)
	}
	return writeToLayerStore(s, func(rlstore rwLayerStore) (drivers.DedupResult, error) {
		layers := make(map[string]struct{})
		for _, i := range topLayers {
			cur := i
			for cur != "" {
				if _, visited := layers[cur]; visited {
					break
				}
				l, err := rlstore.Get(cur)
				if err != nil {
					if err == ErrLayerUnknown {
						break
					}
					return drivers.DedupResult{}, err
				}
				layers[cur] = struct{}{}
				cur = l.Parent
			}
		}
		r := drivers.DedupArgs{
			Options: req.Options,
		}
		for l := range layers {
			r.Layers = append(r.Layers, l)
		}
		return rlstore.dedup(r)
	})
}
