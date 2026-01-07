package docker

import (
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/types"
)

// bicTransportScope returns a BICTransportScope appropriate for ref.
func bicTransportScope(ref dockerReference) types.BICTransportScope {
	// Blobs can be reused across the whole registry.
	return types.BICTransportScope{Opaque: reference.Domain(ref.ref)}
}

// newBICLocationReference returns a BICLocationReference appropriate for ref.
func newBICLocationReference(ref dockerReference) types.BICLocationReference {
	// Blobs are scoped to repositories (the tag/digest are not necessary to reuse a blob).
	return types.BICLocationReference{Opaque: ref.ref.Name()}
}

// parseBICLocationReference returns a repository for encoded lr.
func parseBICLocationReference(lr types.BICLocationReference) (reference.Named, error) {
	return reference.ParseNormalizedNamed(lr.Opaque)
}
