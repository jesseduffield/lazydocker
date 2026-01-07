package entities

import (
	"io"

	encconfig "github.com/containers/ocicrypt/config"
	entitiesTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	libartifactTypes "go.podman.io/common/pkg/libartifact/types"
	"go.podman.io/image/v5/types"
)

type ArtifactAddOptions struct {
	Annotations      map[string]string
	ArtifactMIMEType string
	Append           bool
	FileMIMEType     string
	Replace          bool
}

type ArtifactAddReport = entitiesTypes.ArtifactAddReport

type ArtifactExtractOptions struct {
	// Title annotation value to extract only a single blob matching that name.
	// Conflicts with Digest. Optional.
	Title string
	// Digest of the blob to extract.
	// Conflicts with Title. Optional.
	Digest string
	// ExcludeTitle option allows single blobs to be exported
	// with their title/filename empty. Optional.
	// Default: False
	ExcludeTitle bool
}

type ArtifactBlob = libartifactTypes.ArtifactBlob

type ArtifactInspectOptions struct {
	// Note: Remote is not currently implemented but will be used for
	// remote inspect of artifacts on registries
	Remote bool
}

type ArtifactListOptions struct{}

type ArtifactListReport = entitiesTypes.ArtifactListReport

type ArtifactPullOptions struct {
	// containers-auth.json(5) file to use when authenticating against
	// container registries.
	AuthFilePath string
	// Path to the certificates directory.
	CertDirPath string
	// Allow contacting registries over HTTP, or HTTPS with failed TLS
	// verification. Note that this does not affect other TLS connections.
	InsecureSkipTLSVerify types.OptionalBool
	// Maximum number of retries with exponential backoff when facing
	// transient network errors.
	// Default 3.
	MaxRetries *uint
	// RetryDelay used for the exponential back off of MaxRetries.
	// Default 1 time.Second.
	RetryDelay string
	// OciDecryptConfig contains the config that can be used to decrypt an image if it is
	// encrypted if non-nil. If nil, it does not attempt to decrypt an image.
	OciDecryptConfig *encconfig.DecryptConfig
	// Quiet can be specified to suppress pull progress when pulling.  Ignored
	// for remote calls. //TODO: Verify that claim
	Quiet bool
	// SignaturePolicyPath to overwrite the default one.
	SignaturePolicyPath string
	// Writer is used to display copy information including progress bars.
	Writer io.Writer

	// ----- credentials --------------------------------------------------

	// Username to use when authenticating at a container registry.
	Username string
	// Password to use when authenticating at a container registry.
	Password string
	// IdentityToken is used to authenticate the user and get
	// an access token for the registry.
	IdentityToken string `json:"identitytoken,omitempty"`
}

type ArtifactPullReport = entitiesTypes.ArtifactPullReport

type ArtifactPushOptions struct {
	ImagePushOptions
	DigestFile     string
	EncryptLayers  []int
	EncryptionKeys []string
}

type ArtifactPushReport = entitiesTypes.ArtifactPushReport

type ArtifactRemoveOptions struct {
	// Remove all artifacts
	All bool
	// Artifacts is a list of Artifact IDs or names to remove
	Artifacts []string
	// Ignore if a specified artifact does not exist and do not throw any error.
	Ignore bool
}

type ArtifactRemoveReport = entitiesTypes.ArtifactRemoveReport

type ArtifactInspectReport = entitiesTypes.ArtifactInspectReport
