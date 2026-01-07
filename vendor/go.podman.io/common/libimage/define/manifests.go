package define

import (
	"go.podman.io/image/v5/manifest"
)

// ManifestListDescriptor describes a manifest that is mentioned in an
// image index or manifest list.
// Contains a subset of the fields which are present in both the OCI spec and
// the Docker spec, along with some which are unique to one or the other.
type ManifestListDescriptor struct {
	manifest.Schema2Descriptor
	Platform     manifest.Schema2PlatformSpec `json:"platform,omitempty"`
	Annotations  map[string]string            `json:"annotations,omitempty"`
	ArtifactType string                       `json:"artifactType,omitempty"`
	Data         []byte                       `json:"data,omitempty"`
	Files        []string                     `json:"files,omitempty"`
}

// ManifestListData is a list of platform-specific manifests, specifically used to
// generate output struct for `podman manifest inspect`. Reason for maintaining and
// having this type is to ensure we can have a single type which contains exclusive
// fields from both Docker manifest format and OCI manifest format.
type ManifestListData struct {
	SchemaVersion int                      `json:"schemaVersion"`
	MediaType     string                   `json:"mediaType"`
	ArtifactType  string                   `json:"artifactType,omitempty"`
	Manifests     []ManifestListDescriptor `json:"manifests"`
	Subject       *ManifestListDescriptor  `json:"subject,omitempty"`
	Annotations   map[string]string        `json:"annotations,omitempty"`
}
