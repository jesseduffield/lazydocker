package manifest

import (
	"fmt"

	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	compression "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
)

// ListPublic is a subset of List which is a part of the public API;
// so no methods can be added, removed or changed.
//
// Internal users should usually use List instead.
type ListPublic interface {
	// MIMEType returns the MIME type of this particular manifest list.
	MIMEType() string

	// Instances returns a list of the manifests that this list knows of, other than its own.
	Instances() []digest.Digest

	// Update information about the list's instances.  The length of the passed-in slice must
	// match the length of the list of instances which the list already contains, and every field
	// must be specified.
	UpdateInstances([]ListUpdate) error

	// Instance returns the size and MIME type of a particular instance in the list.
	Instance(digest.Digest) (ListUpdate, error)

	// ChooseInstance selects which manifest is most appropriate for the platform described by the
	// SystemContext, or for the current platform if the SystemContext doesn't specify any details.
	ChooseInstance(ctx *types.SystemContext) (digest.Digest, error)

	// Serialize returns the list in a blob format.
	// NOTE: Serialize() does not in general reproduce the original blob if this object was loaded
	// from, even if no modifications were made!
	Serialize() ([]byte, error)

	// ConvertToMIMEType returns the list rebuilt to the specified MIME type, or an error.
	ConvertToMIMEType(mimeType string) (ListPublic, error)

	// Clone returns a deep copy of this list and its contents.
	Clone() ListPublic
}

// List is an interface for parsing, modifying lists of image manifests.
// Callers can either use this abstract interface without understanding the details of the formats,
// or instantiate a specific implementation (e.g. manifest.OCI1Index) and access the public members
// directly.
type List interface {
	ListPublic
	// CloneInternal returns a deep copy of this list and its contents.
	CloneInternal() List
	// ChooseInstanceInstanceByCompression selects which manifest is most appropriate for the platform and compression described by the
	// SystemContext ( or for the current platform if the SystemContext doesn't specify any detail ) and preferGzip for compression which
	// when configured to OptionalBoolTrue and chooses best available compression when it is OptionalBoolFalse or left OptionalBoolUndefined.
	ChooseInstanceByCompression(ctx *types.SystemContext, preferGzip types.OptionalBool) (digest.Digest, error)
	// Edit information about the list's instances. Contains Slice of ListEdit where each element
	// is responsible for either Modifying or Adding a new instance to the Manifest. Operation is
	// selected on the basis of configured ListOperation field.
	EditInstances([]ListEdit) error
}

// ListUpdate includes the fields which a List's UpdateInstances() method will modify.
// This is publicly visible as c/image/manifest.ListUpdate.
type ListUpdate struct {
	Digest    digest.Digest
	Size      int64
	MediaType string
	// ReadOnly fields: may be set by Instance(), ignored by UpdateInstance()
	ReadOnly struct {
		Platform                  *imgspecv1.Platform
		Annotations               map[string]string
		CompressionAlgorithmNames []string
		ArtifactType              string
	}
}

type ListOp int

const (
	listOpInvalid ListOp = iota
	ListOpAdd
	ListOpUpdate
)

// ListEdit includes the fields which a List's EditInstances() method will modify.
type ListEdit struct {
	ListOperation ListOp

	// if Op == ListEditUpdate (basically the previous UpdateInstances). All fields must be set.
	UpdateOldDigest             digest.Digest
	UpdateDigest                digest.Digest
	UpdateSize                  int64
	UpdateMediaType             string
	UpdateAffectAnnotations     bool
	UpdateAnnotations           map[string]string
	UpdateCompressionAlgorithms []compression.Algorithm

	// If Op = ListEditAdd. All fields must be set.
	AddDigest                digest.Digest
	AddSize                  int64
	AddMediaType             string
	AddArtifactType          string
	AddPlatform              *imgspecv1.Platform
	AddAnnotations           map[string]string
	AddCompressionAlgorithms []compression.Algorithm
}

// ListPublicFromBlob parses a list of manifests.
// This is publicly visible as c/image/manifest.ListFromBlob.
func ListPublicFromBlob(manifest []byte, manifestMIMEType string) (ListPublic, error) {
	list, err := ListFromBlob(manifest, manifestMIMEType)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// ListFromBlob parses a list of manifests.
func ListFromBlob(manifest []byte, manifestMIMEType string) (List, error) {
	normalized := NormalizedMIMEType(manifestMIMEType)
	switch normalized {
	case DockerV2ListMediaType:
		return Schema2ListFromManifest(manifest)
	case imgspecv1.MediaTypeImageIndex:
		return OCI1IndexFromManifest(manifest)
	case DockerV2Schema1MediaType, DockerV2Schema1SignedMediaType, imgspecv1.MediaTypeImageManifest, DockerV2Schema2MediaType:
		return nil, fmt.Errorf("Treating single images as manifest lists is not implemented")
	}
	return nil, fmt.Errorf("Unimplemented manifest list MIME type %q (normalized as %q)", manifestMIMEType, normalized)
}
