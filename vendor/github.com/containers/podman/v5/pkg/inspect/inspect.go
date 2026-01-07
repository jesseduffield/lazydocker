package inspect

import (
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/manifest"
)

// ImageData holds the inspect information of an image.
type ImageData struct {
	ID           string                        `json:"Id"`
	Digest       digest.Digest                 `json:"Digest"`
	RepoTags     []string                      `json:"RepoTags"`
	RepoDigests  []string                      `json:"RepoDigests"`
	Parent       string                        `json:"Parent"`
	Comment      string                        `json:"Comment"`
	Created      *time.Time                    `json:"Created"`
	Config       *v1.ImageConfig               `json:"Config"`
	Version      string                        `json:"Version"`
	Author       string                        `json:"Author"`
	Architecture string                        `json:"Architecture"`
	Os           string                        `json:"Os"`
	Size         int64                         `json:"Size"`
	VirtualSize  int64                         `json:"VirtualSize"`
	GraphDriver  *define.DriverData            `json:"GraphDriver"`
	RootFS       *RootFS                       `json:"RootFS"`
	Labels       map[string]string             `json:"Labels"`
	Annotations  map[string]string             `json:"Annotations"`
	ManifestType string                        `json:"ManifestType"`
	User         string                        `json:"User"`
	History      []v1.History                  `json:"History"`
	NamesHistory []string                      `json:"NamesHistory"`
	HealthCheck  *manifest.Schema2HealthConfig `json:"Healthcheck,omitempty"`
}

// RootFS holds the root fs information of an image.
type RootFS struct {
	Type   string          `json:"Type"`
	Layers []digest.Digest `json:"Layers"`
}
