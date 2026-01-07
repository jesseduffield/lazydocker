package types

import "io"

// GetArtifactOptions is a struct containing options that for obtaining artifacts.
// It is meant for future growth or changes required without wacking the API.
type GetArtifactOptions struct{}

// AddOptions are additional descriptors of an artifact file.
type AddOptions struct {
	Annotations      map[string]string `json:"annotations,omitempty"`
	ArtifactMIMEType string            `json:",omitempty"`
	// append option is not compatible with ArtifactMIMEType option
	Append bool `json:",omitempty"`
	// FileType describes the media type for the layer.  It is an override
	// for the standard detection
	FileMIMEType string `json:",omitempty"`
	// Replace option removes existing artifact before adding new one
	Replace bool `json:",omitempty"`
}

// FilterBlobOptions options used to filter for a single blob in an artifact.
type FilterBlobOptions struct {
	// Title annotation value to extract only a single blob matching that name.
	// Optional. Conflicts with Digest.
	Title string
	// Digest of the blob to extract.
	// Optional. Conflicts with Title.
	Digest string
}

type ExtractOptions struct {
	FilterBlobOptions
	// ExcludeTitle option allows single blobs to be exported
	// with their title/filename empty. Optional.
	// Default: False
	ExcludeTitle bool
}

type BlobMountPathOptions struct {
	FilterBlobOptions
}

// BlobMountPath contains the info on how the artifact must be mounted.
type BlobMountPath struct {
	// Source path of the blob, i.e. full path in the blob dir.
	SourcePath string
	// Name of the file in the container.
	Name string
}

type ArtifactBlob struct {
	BlobReader   io.Reader
	BlobFilePath string
	FileName     string
}
