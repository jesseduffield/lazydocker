package storage

import (
	"io"
	"time"

	digest "github.com/opencontainers/go-digest"
	drivers "go.podman.io/storage/drivers"
	"go.podman.io/storage/pkg/archive"
)

// The type definitions in this file exist ONLY to maintain formal API compatibility.
// DO NOT ADD ANY NEW METHODS TO THESE INTERFACES.

// ROFileBasedStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ROFileBasedStore interface {
	Locker
	Load() error
	ReloadIfChanged() error
}

// RWFileBasedStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type RWFileBasedStore interface {
	Save() error
}

// FileBasedStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type FileBasedStore interface {
	ROFileBasedStore
	RWFileBasedStore
}

// ROMetadataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ROMetadataStore interface {
	Metadata(id string) (string, error)
}

// RWMetadataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type RWMetadataStore interface {
	SetMetadata(id, metadata string) error
}

// MetadataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type MetadataStore interface {
	ROMetadataStore
	RWMetadataStore
}

// ROBigDataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ROBigDataStore interface {
	BigData(id, key string) ([]byte, error)
	BigDataSize(id, key string) (int64, error)
	BigDataDigest(id, key string) (digest.Digest, error)
	BigDataNames(id string) ([]string, error)
}

// RWImageBigDataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type RWImageBigDataStore interface {
	SetBigData(id, key string, data []byte, digestManifest func([]byte) (digest.Digest, error)) error
}

// ContainerBigDataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ContainerBigDataStore interface {
	ROBigDataStore
	SetBigData(id, key string, data []byte) error
}

// ROLayerBigDataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ROLayerBigDataStore interface {
	BigData(id, key string) (io.ReadCloser, error)
	BigDataNames(id string) ([]string, error)
}

// RWLayerBigDataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type RWLayerBigDataStore interface {
	SetBigData(id, key string, data io.Reader) error
}

// LayerBigDataStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type LayerBigDataStore interface {
	ROLayerBigDataStore
	RWLayerBigDataStore
}

// FlaggableStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type FlaggableStore interface {
	ClearFlag(id string, flag string) error
	SetFlag(id string, flag string, value any) error
}

// ContainerStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ContainerStore interface {
	FileBasedStore
	MetadataStore
	ContainerBigDataStore
	FlaggableStore
	Create(id string, names []string, image, layer, metadata string, options *ContainerOptions) (*Container, error)
	SetNames(id string, names []string) error
	AddNames(id string, names []string) error
	RemoveNames(id string, names []string) error
	Get(id string) (*Container, error)
	Exists(id string) bool
	Delete(id string) error
	Wipe() error
	Lookup(name string) (string, error)
	Containers() ([]Container, error)
}

// ROImageStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ROImageStore interface {
	ROFileBasedStore
	ROMetadataStore
	ROBigDataStore
	Exists(id string) bool
	Get(id string) (*Image, error)
	Lookup(name string) (string, error)
	Images() ([]Image, error)
	ByDigest(d digest.Digest) ([]*Image, error)
}

// ImageStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ImageStore interface {
	ROImageStore
	RWFileBasedStore
	RWMetadataStore
	RWImageBigDataStore
	FlaggableStore
	Create(id string, names []string, layer, metadata string, created time.Time, searchableDigest digest.Digest) (*Image, error)
	SetNames(id string, names []string) error
	AddNames(id string, names []string) error
	RemoveNames(id string, names []string) error
	Delete(id string) error
	Wipe() error
}

// ROLayerStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type ROLayerStore interface {
	ROFileBasedStore
	ROMetadataStore
	ROLayerBigDataStore
	Exists(id string) bool
	Get(id string) (*Layer, error)
	Status() ([][2]string, error)
	Changes(from, to string) ([]archive.Change, error)
	Diff(from, to string, options *DiffOptions) (io.ReadCloser, error)
	DiffSize(from, to string) (int64, error)
	Size(name string) (int64, error)
	Lookup(name string) (string, error)
	LayersByCompressedDigest(d digest.Digest) ([]Layer, error)
	LayersByUncompressedDigest(d digest.Digest) ([]Layer, error)
	Layers() ([]Layer, error)
}

// LayerStore is a deprecated interface with no documented way to use it from callers outside of c/storage.
//
// Deprecated: There is no way to use this from any external user of c/storage to invoke c/storage functionality.
type LayerStore interface {
	ROLayerStore
	RWFileBasedStore
	RWMetadataStore
	FlaggableStore
	RWLayerBigDataStore
	Create(id string, parent *Layer, names []string, mountLabel string, options map[string]string, moreOptions *LayerOptions, writeable bool) (*Layer, error)
	CreateWithFlags(id string, parent *Layer, names []string, mountLabel string, options map[string]string, moreOptions *LayerOptions, writeable bool, flags map[string]any) (layer *Layer, err error)
	Put(id string, parent *Layer, names []string, mountLabel string, options map[string]string, moreOptions *LayerOptions, writeable bool, flags map[string]any, diff io.Reader) (*Layer, int64, error)
	SetNames(id string, names []string) error
	AddNames(id string, names []string) error
	RemoveNames(id string, names []string) error
	Delete(id string) error
	Wipe() error
	Mount(id string, options drivers.MountOpts) (string, error)
	Unmount(id string, force bool) (bool, error)
	Mounted(id string) (int, error)
	ParentOwners(id string) (uids, gids []int, err error)
	ApplyDiff(to string, diff io.Reader) (int64, error)
	DifferTarget(id string) (string, error)
	LoadLocked() error
	PutAdditionalLayer(id string, parentLayer *Layer, names []string, aLayer drivers.AdditionalLayer) (layer *Layer, err error)
}
