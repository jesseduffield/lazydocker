package manifest

import (
	"github.com/opencontainers/go-digest"
)

// Schema2Descriptor is a “descriptor” in docker/distribution schema 2.
//
// This is publicly visible as c/image/manifest.Schema2Descriptor.
type Schema2Descriptor struct {
	MediaType string        `json:"mediaType"`
	Size      int64         `json:"size"`
	Digest    digest.Digest `json:"digest"`
	URLs      []string      `json:"urls,omitempty"`
}
