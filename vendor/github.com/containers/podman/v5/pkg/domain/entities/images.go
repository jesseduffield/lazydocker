package entities

import (
	"io"
	"net/url"

	encconfig "github.com/containers/ocicrypt/config"
	entitiesTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/signature/signer"
	"go.podman.io/image/v5/types"
)

type ImageSummary = entitiesTypes.ImageSummary

// ImageRemoveOptions can be used to alter image removal.
type ImageRemoveOptions struct {
	// All will remove all images.
	All bool
	// Force will force image removal including containers using the images.
	Force bool
	// Ignore if a specified image does not exist and do not throw an error.
	Ignore bool
	// Confirms if given name is a manifest list and removes it, otherwise returns error.
	LookupManifest bool
	// NoPrune will not remove dangling images
	NoPrune                      bool
	DisableForceRemoveContainers bool
}

// ImageRemoveReport is the response for removing one or more image(s) from storage
// and images what was untagged vs actually removed.
type ImageRemoveReport = entitiesTypes.ImageRemoveReport

type ImageHistoryOptions struct{}

type (
	ImageHistoryLayer  = entitiesTypes.ImageHistoryLayer
	ImageHistoryReport = entitiesTypes.ImageHistoryReport
)

// ImagePullOptions are the arguments for pulling images.
type ImagePullOptions struct {
	// AllTags can be specified to pull all tags of an image. Note
	// that this only works if the image does not include a tag.
	AllTags bool
	// Authfile is the path to the authentication file. Ignored for remote
	// calls.
	Authfile string
	// CertDir is the path to certificate directories.  Ignored for remote
	// calls.
	CertDir string
	// Username for authenticating against the registry.
	Username string
	// Password for authenticating against the registry.
	Password string
	// Arch will overwrite the local architecture for image pulls.
	Arch string
	// OS will overwrite the local operating system (OS) for image
	// pulls.
	OS string
	// Variant will overwrite the local variant for image pulls.
	Variant string
	// Quiet can be specified to suppress pull progress when pulling.  Ignored
	// for remote calls.
	Quiet bool
	// Retry number of times to retry pull in case of failure
	Retry *uint
	// RetryDelay between retries in case of pull failures
	RetryDelay string
	// SignaturePolicy to use when pulling.  Ignored for remote calls.
	SignaturePolicy string
	// SkipTLSVerify to skip HTTPS and certificate verification.
	SkipTLSVerify types.OptionalBool
	// PullPolicy whether to pull new image
	PullPolicy config.PullPolicy
	// Writer is used to display copy information including progress bars.
	Writer io.Writer
	// OciDecryptConfig contains the config that can be used to decrypt an image if it is
	// encrypted if non-nil. If nil, it does not attempt to decrypt an image.
	OciDecryptConfig *encconfig.DecryptConfig
}

// ImagePullReport is the response from pulling one or more images.
type ImagePullReport = entitiesTypes.ImagePullReport

// ImagePushOptions are the arguments for pushing images.
type ImagePushOptions struct {
	// All indicates that all images referenced in a manifest list should be pushed
	All bool
	// Authfile is the path to the authentication file. Ignored for remote
	// calls.
	Authfile string
	// CertDir is the path to certificate directories.  Ignored for remote
	// calls.
	CertDir string
	// Compress tarball image layers when pushing to a directory using the 'dir'
	// transport. Default is same compression type as source. Ignored for remote
	// calls.
	Compress bool
	// Username for authenticating against the registry.
	Username string
	// Password for authenticating against the registry.
	Password string
	// Format is the Manifest type (oci, v2s1, or v2s2) to use when pushing an
	// image. Default is manifest type of source, with fallbacks.
	// Ignored for remote calls.
	Format string
	// Quiet can be specified to suppress push progress when pushing.
	Quiet bool
	// Rm indicates whether to remove the manifest list if push succeeds
	Rm bool
	// RemoveSignatures, discard any pre-existing signatures in the image.
	// Ignored for remote calls.
	RemoveSignatures bool
	// Retry number of times to retry push in case of failure
	Retry *uint
	// RetryDelay between retries in case of push failures
	RetryDelay string
	// SignaturePolicy to use when pulling.  Ignored for remote calls.
	SignaturePolicy string
	// Signers, if non-empty, asks for signatures to be added during the copy
	// using the provided signers.
	// Rejected for remote calls.
	Signers []*signer.Signer
	// SignBy adds a signature at the destination using the specified key.
	// Ignored for remote calls.
	SignBy string
	// SignPassphrase, if non-empty, specifies a passphrase to use when signing
	// with the key ID from SignBy.
	SignPassphrase string
	// SignBySigstorePrivateKeyFile, if non-empty, asks for a signature to be added
	// during the copy, using a sigstore private key file at the provided path.
	// Ignored for remote calls.
	SignBySigstorePrivateKeyFile string
	// SignSigstorePrivateKeyPassphrase is the passphrase to use when signing with
	// SignBySigstorePrivateKeyFile.
	SignSigstorePrivateKeyPassphrase []byte
	// SkipTLSVerify to skip HTTPS and certificate verification.
	SkipTLSVerify types.OptionalBool
	// Progress to get progress notifications
	Progress chan types.ProgressProperties
	// CompressionFormat is the format to use for the compression of the blobs
	CompressionFormat string
	// CompressionLevel is the level to use for the compression of the blobs
	CompressionLevel *int
	// Writer is used to display copy information including progress bars.
	Writer io.Writer
	// OciEncryptConfig when non-nil indicates that an image should be encrypted.
	// The encryption options is derived from the construction of EncryptConfig object.
	OciEncryptConfig *encconfig.EncryptConfig
	// OciEncryptLayers represents the list of layers to encrypt.
	// If nil, don't encrypt any layers.
	// If non-nil and len==0, denotes encrypt all layers.
	// integers in the slice represent 0-indexed layer indices, with support for negative
	// indexing. i.e. 0 is the first layer, -1 is the last (top-most) layer.
	OciEncryptLayers *[]int
	//  If necessary, add clones of existing instances with requested compression algorithms to manifest list
	// Note: Following option is only valid for `manifest push`
	AddCompression []string
	// ForceCompressionFormat ensures that the compression algorithm set in
	// CompressionFormat is used exclusively, and blobs of other compression
	// algorithms are not reused.
	ForceCompressionFormat bool
}

// ImagePushReport is the response from pushing an image.
type ImagePushReport struct {
	// The digest of the manifest of the pushed image.
	ManifestDigest string
}

// ImagePushStream is the response from pushing an image. Only used in the
// remote API.
type ImagePushStream = entitiesTypes.ImagePushStream

// ImageSearchOptions are the arguments for searching images.
type ImageSearchOptions struct {
	// Authfile is the path to the authentication file. Ignored for remote
	// calls.
	Authfile string
	// CertDir is the path to certificate directories.  Ignored for remote
	// calls.
	CertDir string
	// Username for authenticating against the registry.
	Username string
	// Password for authenticating against the registry.
	Password string
	// IdentityToken is used to authenticate the user and get
	// an access token for the registry.
	IdentityToken string
	// Filters for the search results.
	Filters []string
	// Limit the number of results.
	Limit int
	// SkipTLSVerify to skip  HTTPS and certificate verification.
	SkipTLSVerify types.OptionalBool
	// ListTags search the available tags of the repository
	ListTags bool
}

// ImageSearchReport is the response from searching images.
type ImageSearchReport = entitiesTypes.ImageSearchReport

// Image List Options
type ImageListOptions struct {
	All bool
	// ExtendedAttributes is used by the libpod endpoint only to deliver extra information
	// that the compat endpoint does not
	ExtendedAttributes bool
	Filter             []string
}

type ImagePruneOptions struct {
	All        bool     `json:"all" schema:"all"`
	External   bool     `json:"external" schema:"external"`
	BuildCache bool     `json:"buildcache" schema:"buildcache"`
	Filter     []string `json:"filter" schema:"filter"`
}

type (
	ImageTagOptions   struct{}
	ImageUntagOptions struct{}
)

// ImageInspectReport is the data when inspecting an image.
type ImageInspectReport = entitiesTypes.ImageInspectReport

type ImageLoadOptions struct {
	Input           string
	Quiet           bool
	SignaturePolicy string
}

type ImageLoadReport = entitiesTypes.ImageLoadReport

type ImageImportOptions struct {
	Architecture    string
	Variant         string
	Changes         []string
	Message         string
	OS              string
	Quiet           bool
	Reference       string
	SignaturePolicy string
	Source          string
	SourceIsURL     bool
}

type ImageImportReport = entitiesTypes.ImageImportReport

// ImageSaveOptions provide options for saving images.
type ImageSaveOptions struct {
	// Compress layers when saving to a directory.
	Compress bool
	// Format of saving the image: oci-archive, oci-dir (directory with oci
	// manifest type), docker-archive, docker-dir (directory with v2s2
	// manifest type).
	Format string
	// MultiImageArchive denotes if the created archive shall include more
	// than one image.  Additional tags will be interpreted as references
	// to images which are added to the archive.
	MultiImageArchive bool
	// Accept uncompressed layers when copying OCI images.
	OciAcceptUncompressedLayers bool
	// Output - write image to the specified path.
	Output string
	// Quiet - suppress output when copying images
	Quiet           bool
	SignaturePolicy string
}

// ImageScpOptions provides options for ImageEngine.Scp()
type ImageScpOptions struct {
	ScpExecuteTransferOptions
}

// ImageScpReport provides results from ImageEngine.Scp()
type ImageScpReport struct{}

// ImageScpConnections provides the ssh related information used in remote image transfer
type ImageScpConnections struct {
	// Connections holds the raw string values for connections (ssh or unix)
	Connections []string
	// URI contains the ssh connection URLs to be used by the client
	URI []*url.URL
	// Identities contains ssh identity keys to be used by the client
	Identities []string
}

// ImageTreeOptions provides options for ImageEngine.Tree()
type ImageTreeOptions struct {
	WhatRequires bool // Show all child images and layers of the specified image
}

// ImageTreeReport provides results from ImageEngine.Tree()
type ImageTreeReport = entitiesTypes.ImageTreeReport

// ShowTrustOptions are the cli options for showing trust
type ShowTrustOptions struct {
	JSON         bool
	PolicyPath   string
	Raw          bool
	RegistryPath string
}

// ShowTrustReport describes the results of show trust
type ShowTrustReport = entitiesTypes.ShowTrustReport

// SetTrustOptions describes the CLI options for setting trust
type SetTrustOptions struct {
	PolicyPath  string
	PubKeysFile []string
	Type        string
}

// SignOptions describes input options for the CLI signing
type SignOptions struct {
	Directory string
	SignBy    string
	CertDir   string
	Authfile  string
	All       bool
}

// SignReport describes the result of signing
type SignReport struct{}

// ImageMountOptions describes the input values for mounting images
// in the CLI
type ImageMountOptions struct {
	All    bool
	Format string
}

// ImageUnmountOptions are the options from the cli for unmounting
type ImageUnmountOptions struct {
	All   bool
	Force bool
}

// ImageMountReport describes the response from image mount
type ImageMountReport = entitiesTypes.ImageMountReport

// ImageUnmountReport describes the response from umounting an image
type ImageUnmountReport = entitiesTypes.ImageUnmountReport

const (
	LocalFarmImageBuilderName   = "(local)"
	LocalFarmImageBuilderDriver = "local"
)

// FarmInspectReport describes the response from farm inspect
type FarmInspectReport = entitiesTypes.FarmInspectReport
