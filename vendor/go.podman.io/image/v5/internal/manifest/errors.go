package manifest

import (
	"fmt"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// FIXME: This is a duplicate of c/image/manifestDockerV2Schema2ConfigMediaType.
// Deduplicate that, depending on outcome of https://github.com/containers/image/pull/1791 .
const dockerV2Schema2ConfigMediaType = "application/vnd.docker.container.image.v1+json"

// NonImageArtifactError (detected via errors.As) is used when asking for an image-specific operation
// on an object which is not a “container image” in the standard sense (e.g. an OCI artifact)
//
// This is publicly visible as c/image/manifest.NonImageArtifactError (but we don’t provide a public constructor)
type NonImageArtifactError struct {
	// Callers should not be blindly calling image-specific operations and only checking MIME types
	// on failure; if they care about the artifact type, they should check before using it.
	// If they blindly assume an image, they don’t really need this value; just a type check
	// is sufficient for basic "we can only pull images" UI.
	//
	// Also, there are fairly widespread “artifacts” which nevertheless use imgspecv1.MediaTypeImageConfig,
	// e.g. https://github.com/sigstore/cosign/blob/main/specs/SIGNATURE_SPEC.md , which could cause the callers
	// to complain about a non-image artifact with the correct MIME type; we should probably add some other kind of
	// type discrimination, _and_ somehow make it available in the API, if we expect API callers to make decisions
	// based on that kind of data.
	//
	// So, let’s not expose this until a specific need is identified.
	mimeType string
}

// NewNonImageArtifactError returns a NonImageArtifactError about an artifact manifest.
//
// This is typically called if manifest.Config.MediaType != imgspecv1.MediaTypeImageConfig .
func NewNonImageArtifactError(manifest *imgspecv1.Manifest) error {
	// Callers decide based on manifest.Config.MediaType that this is not an image;
	// in that case manifest.ArtifactType can be optionally defined, and if it is, it is typically
	// more relevant because config may be ~absent with imgspecv1.MediaTypeEmptyJSON.
	//
	// If ArtifactType and Config.MediaType are both defined and non-trivial, presumably
	// ArtifactType is the “top-level” one, although that’s not defined by the spec.
	mimeType := manifest.ArtifactType
	if mimeType == "" {
		mimeType = manifest.Config.MediaType
	}
	return NonImageArtifactError{mimeType: mimeType}
}

func (e NonImageArtifactError) Error() string {
	// Special-case these invalid mixed images, which show up from time to time:
	if e.mimeType == dockerV2Schema2ConfigMediaType {
		return fmt.Sprintf("invalid mixed OCI image with Docker v2s2 config (%q)", e.mimeType)
	}
	return fmt.Sprintf("unsupported image-specific operation on artifact with type %q", e.mimeType)
}
