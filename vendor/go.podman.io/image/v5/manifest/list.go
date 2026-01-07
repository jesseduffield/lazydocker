package manifest

import (
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/internal/manifest"
)

var (
	// SupportedListMIMETypes is a list of the manifest list types that we know how to
	// read/manipulate/write.
	SupportedListMIMETypes = []string{
		DockerV2ListMediaType,
		imgspecv1.MediaTypeImageIndex,
	}
)

// List is an interface for parsing, modifying lists of image manifests.
// Callers can either use this abstract interface without understanding the details of the formats,
// or instantiate a specific implementation (e.g. manifest.OCI1Index) and access the public members
// directly.
type List = manifest.ListPublic

// ListUpdate includes the fields which a List's UpdateInstances() method will modify.
type ListUpdate = manifest.ListUpdate

// ListFromBlob parses a list of manifests.
func ListFromBlob(manifestBlob []byte, manifestMIMEType string) (List, error) {
	return manifest.ListPublicFromBlob(manifestBlob, manifestMIMEType)
}

// ConvertListToMIMEType converts the passed-in manifest list to a manifest
// list of the specified type.
func ConvertListToMIMEType(list List, manifestMIMEType string) (List, error) {
	return list.ConvertToMIMEType(manifestMIMEType)
}
