package types

import (
	"time"

	"github.com/containers/podman/v5/pkg/inspect"
	"github.com/containers/podman/v5/pkg/trust"
)

// swagger:model LibpodImageSummary
type ImageSummary struct {
	ID          string `json:"Id"`
	ParentId    string
	RepoTags    []string
	RepoDigests []string
	Created     int64
	Size        int64
	SharedSize  int
	VirtualSize int64 `json:",omitempty"`
	Labels      map[string]string
	Containers  int
	ReadOnly    bool `json:",omitempty"`
	Dangling    bool `json:",omitempty"`

	// Podman extensions
	Arch    string   `json:",omitempty"`
	Digest  string   `json:",omitempty"`
	History []string `json:",omitempty"`
	// IsManifestList is a ptr so we can distinguish between a true
	// json empty response and false.  the docker compat side needs to return
	// empty; where as the libpod side needs a value of true or false
	IsManifestList *bool    `json:",omitempty"`
	Names          []string `json:",omitempty"`
	Os             string   `json:",omitempty"`
}

func (i *ImageSummary) Id() string {
	return i.ID
}

func (i *ImageSummary) IsReadOnly() bool {
	return i.ReadOnly
}

func (i *ImageSummary) IsDangling() bool {
	return i.Dangling
}

type ImageInspectReport struct {
	*inspect.ImageData
}

type ImageTreeReport struct {
	Tree string // TODO: Refactor move presentation work out of server
}

type ImageLoadReport struct {
	Names []string
}

type ImageImportReport struct {
	Id string
}

// ImageSearchReport is the response from searching images.
type ImageSearchReport struct {
	// Index is the image index (e.g., "docker.io" or "quay.io")
	Index string
	// Name is the canonical name of the image (e.g., "docker.io/library/alpine").
	Name string
	// Description of the image.
	Description string
	// Stars is the number of stars of the image.
	Stars int
	// Official indicates if it's an official image.
	Official string
	// Automated indicates if the image was created by an automated build.
	Automated string
	// Tag is the repository tag
	Tag string
}

// ShowTrustReport describes the results of show trust
type ShowTrustReport struct {
	Raw                     []byte
	SystemRegistriesDirPath string
	JSONOutput              []byte
	Policies                []*trust.Policy
}

// ImageMountReport describes the response from image mount
type ImageMountReport struct {
	Id           string
	Name         string
	Repositories []string
	Path         string
}

// ImageUnmountReport describes the response from umounting an image
type ImageUnmountReport struct {
	Err error
	Id  string
}

// FarmInspectReport describes the response from farm inspect
type FarmInspectReport struct {
	NativePlatforms   []string
	EmulatedPlatforms []string
	OS                string
	Arch              string
	Variant           string
}

// ImageRemoveReport is the response for removing one or more image(s) from storage
// and images what was untagged vs actually removed.
type ImageRemoveReport struct {
	// Deleted images.
	Deleted []string `json:",omitempty"`
	// Untagged images. Can be longer than Deleted.
	Untagged []string `json:",omitempty"`
	// ExitCode describes the exit codes as described in the `podman rmi`
	// man page.
	ExitCode int
}

type ImageHistoryLayer struct {
	ID        string    `json:"id"`
	Created   time.Time `json:"created"`
	CreatedBy string    `json:",omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	Size      int64     `json:"size"`
	Comment   string    `json:"comment,omitempty"`
}

type ImageHistoryReport struct {
	Layers []ImageHistoryLayer
}

type ImagePullReport struct {
	// Stream used to provide output from c/image
	Stream string `json:"stream,omitempty"`
	// Error contains text of errors from c/image
	Error string `json:"error,omitempty"`
	// Images contains the ID's of the images pulled
	Images []string `json:"images,omitempty"`
	// ID contains image id (retained for backwards compatibility)
	ID string `json:"id,omitempty"`
}

type ImagePushStream struct {
	// ManifestDigest is the digest of the manifest of the pushed image.
	ManifestDigest string `json:"manifestdigest,omitempty"`
	// Stream used to provide push progress
	Stream string `json:"stream,omitempty"`
	// Error contains text of errors from pushing
	Error string `json:"error,omitempty"`
}
