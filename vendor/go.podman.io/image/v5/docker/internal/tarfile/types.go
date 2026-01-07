package tarfile

import (
	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/manifest"
)

// Various data structures.

// Based on github.com/docker/docker/image/tarexport/tarexport.go
const (
	manifestFileName           = "manifest.json"
	legacyLayerFileName        = "layer.tar"
	legacyConfigFileName       = "json"
	legacyVersionFileName      = "VERSION"
	legacyRepositoriesFileName = "repositories"
)

// ManifestItem is an element of the array stored in the top-level manifest.json file.
type ManifestItem struct { // NOTE: This is visible as docker/tarfile.ManifestItem, and a part of the stable API.
	Config       string
	RepoTags     []string
	Layers       []string
	Parent       imageID                                      `json:",omitempty"`
	LayerSources map[digest.Digest]manifest.Schema2Descriptor `json:",omitempty"`
}

type imageID string
