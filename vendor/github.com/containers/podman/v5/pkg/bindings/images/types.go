package images

import (
	"io"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
)

// RemoveOptions are optional options for image removal
//
//go:generate go run ../generator/generator.go RemoveOptions
type RemoveOptions struct {
	// All removes all images
	All *bool
	// Forces removes all containers based on the image
	Force *bool
	// Ignore if a specified image does not exist and do not throw an error.
	Ignore *bool
	// Confirms if given name is a manifest list and removes it, otherwise returns error.
	LookupManifest *bool
	// Does not remove dangling parent images
	NoPrune *bool
}

// DiffOptions are optional options image diffs
//
//go:generate go run ../generator/generator.go DiffOptions
type DiffOptions struct {
	// By the default diff will compare against the parent layer. Change the Parent if you want to compare against something else.
	Parent *string
	// Change the type the backend should match. This can be set to "all", "container" or "image".
	DiffType *string
}

// ListOptions are optional options for listing images
//
//go:generate go run ../generator/generator.go ListOptions
type ListOptions struct {
	// All lists all image in the image store including dangling images
	All *bool
	// filters that can be used to get a more specific list of images
	Filters map[string][]string
}

// GetOptions are optional options for inspecting an image
//
//go:generate go run ../generator/generator.go GetOptions
type GetOptions struct {
	// Size computes the amount of storage the image consumes
	Size *bool
}

// TreeOptions are optional options for a tree-based representation
// of the image
//
//go:generate go run ../generator/generator.go TreeOptions
type TreeOptions struct {
	// WhatRequires ...
	WhatRequires *bool
}

// HistoryOptions are optional options image history
//
//go:generate go run ../generator/generator.go HistoryOptions
type HistoryOptions struct {
}

// LoadOptions are optional options for loading an image
//
//go:generate go run ../generator/generator.go LoadOptions
type LoadOptions struct {
	// Reference is the name of the loaded image
	Reference *string
}

// ExportOptions are optional options for exporting images
//
//go:generate go run ../generator/generator.go ExportOptions
type ExportOptions struct {
	// Compress the image
	Compress *bool
	// Format of the output
	Format *string
	// Accept uncompressed layers when copying OCI images.
	OciAcceptUncompressedLayers *bool
}

// PruneOptions are optional options for pruning images
//
//go:generate go run ../generator/generator.go PruneOptions
type PruneOptions struct {
	// Prune all images
	All *bool
	// Prune images even when they're used by external containers
	External *bool
	// Prune persistent build cache
	BuildCache *bool
	// Filters to apply when pruning images
	Filters map[string][]string
}

// TagOptions are optional options for tagging images
//
//go:generate go run ../generator/generator.go TagOptions
type TagOptions struct {
}

// UntagOptions are optional options for untagging images
//
//go:generate go run ../generator/generator.go UntagOptions
type UntagOptions struct {
}

// ImportOptions are optional options for importing images
//
//go:generate go run ../generator/generator.go ImportOptions
type ImportOptions struct {
	// Changes to be applied to the image
	Changes *[]string
	// Message to be applied to the image
	Message *string
	// Reference is a tag to be applied to the image
	Reference *string
	// Url to option image to import. Cannot be used with the reader
	URL *string
	// OS for the imported image
	OS *string
	// Architecture for the imported image
	Architecture *string
	// Variant for the imported image
	Variant *string
}

// PushOptions are optional options for importing images
//
//go:generate go run ../generator/generator.go PushOptions
type PushOptions struct {
	// All indicates whether to push all images related to the image list
	All *bool
	// Authfile is the path to the authentication file. Ignored for remote
	// calls.
	Authfile *string
	// Compress tarball image layers when pushing to a directory using the 'dir' transport.
	Compress *bool
	// CompressionFormat is the format to use for the compression of the blobs
	CompressionFormat *string
	// CompressionLevel is the level to use for the compression of the blobs
	CompressionLevel *int
	// ForceCompressionFormat ensures that the compression algorithm set in
	// CompressionFormat is used exclusively, and blobs of other compression
	// algorithms are not reused.
	ForceCompressionFormat *bool
	// Add existing instances with requested compression algorithms to manifest list
	AddCompression []string
	// Manifest type of the pushed image
	Format *string
	// Password for authenticating against the registry.
	Password *string `schema:"-"`
	// ProgressWriter is a writer where push progress are sent.
	// Since API handler for image push is quiet by default, WithQuiet(false) is necessary for
	// the writer to receive progress messages.
	ProgressWriter *io.Writer `schema:"-"`
	// SkipTLSVerify to skip HTTPS and certificate verification.
	SkipTLSVerify *bool `schema:"-"`
	// RemoveSignatures Discard any pre-existing signatures in the image.
	RemoveSignatures *bool
	// Retry number of times to retry push in case of failure
	Retry *uint
	// RetryDelay between retries in case of push failures
	RetryDelay *string
	// Username for authenticating against the registry.
	Username *string `schema:"-"`
	// Quiet can be specified to suppress progress when pushing.
	Quiet *bool

	// Manifest of the pushed image.  Set by images.Push.
	ManifestDigest *string
}

// SearchOptions are optional options for searching images on registries
//
//go:generate go run ../generator/generator.go SearchOptions
type SearchOptions struct {
	// Authfile is the path to the authentication file. Ignored for remote
	// calls.
	Authfile *string
	// Filters for the search results.
	Filters map[string][]string
	// Limit the number of results.
	Limit *int
	// SkipTLSVerify to skip  HTTPS and certificate verification.
	SkipTLSVerify *bool `schema:"-"`
	// ListTags search the available tags of the repository
	ListTags *bool
	// Username for authenticating against the registry.
	Username *string `schema:"-"`
	// Password for authenticating against the registry.
	Password *string `schema:"-"`
}

// PullOptions are optional options for pulling images
//
//go:generate go run ../generator/generator.go PullOptions
type PullOptions struct {
	// AllTags can be specified to pull all tags of an image. Note
	// that this only works if the image does not include a tag.
	AllTags *bool
	// Arch will overwrite the local architecture for image pulls.
	Arch *string
	// Authfile is the path to the authentication file. Ignored for remote
	// calls.
	Authfile *string
	// OS will overwrite the local operating system (OS) for image
	// pulls.
	OS *string
	// Policy is the pull policy. Supported values are "missing", "never",
	// "newer", "always". An empty string defaults to "always".
	Policy *string
	// Password for authenticating against the registry.
	Password *string `schema:"-"`
	// ProgressWriter is a writer where pull progress are sent.
	ProgressWriter *io.Writer `schema:"-"`
	// Quiet can be specified to suppress pull progress when pulling.  Ignored
	// for remote calls.
	Quiet *bool
	// Retry number of times to retry pull in case of failure
	Retry *uint
	// RetryDelay between retries in case of pull failures
	RetryDelay *string
	// SkipTLSVerify to skip HTTPS and certificate verification.
	SkipTLSVerify *bool `schema:"-"`
	// Username for authenticating against the registry.
	Username *string `schema:"-"`
	// Variant will overwrite the local variant for image pulls.
	Variant *string
}

// BuildOptions are optional options for building images
type BuildOptions = types.BuildOptions

// ExistsOptions are optional options for checking if an image exists
//
//go:generate go run ../generator/generator.go ExistsOptions
type ExistsOptions struct {
}

type ScpOptions struct {
	Quiet       *bool
	Destination *string
}
