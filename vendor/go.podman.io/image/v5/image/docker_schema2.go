package image

import (
	"go.podman.io/image/v5/internal/image"
)

// GzippedEmptyLayer is a gzip-compressed version of an empty tar file (1024 NULL bytes)
// This comes from github.com/docker/distribution/manifest/schema1/config_builder.go; there is
// a non-zero embedded timestamp; we could zero that, but that would just waste storage space
// in registries, so let’s use the same values.
var GzippedEmptyLayer = image.GzippedEmptyLayer

// GzippedEmptyLayerDigest is a digest of GzippedEmptyLayer
const GzippedEmptyLayerDigest = image.GzippedEmptyLayerDigest
