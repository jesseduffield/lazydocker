//go:build !remote

package libimage

import (
	"context"
	"time"

	"github.com/opencontainers/go-digest"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/types"
)

// ImageData contains the inspected data of an image.
type ImageData struct {
	ID           string                        `json:"Id"`
	Digest       digest.Digest                 `json:"Digest"`
	RepoTags     []string                      `json:"RepoTags"`
	RepoDigests  []string                      `json:"RepoDigests"`
	Parent       string                        `json:"Parent"`
	Comment      string                        `json:"Comment"`
	Created      *time.Time                    `json:"Created"`
	Config       *ociv1.ImageConfig            `json:"Config"`
	Version      string                        `json:"Version"`
	Author       string                        `json:"Author"`
	Architecture string                        `json:"Architecture"`
	Os           string                        `json:"Os"`
	Size         int64                         `json:"Size"`
	VirtualSize  int64                         `json:"VirtualSize"`
	GraphDriver  *DriverData                   `json:"GraphDriver"`
	RootFS       *RootFS                       `json:"RootFS"`
	Labels       map[string]string             `json:"Labels"`
	Annotations  map[string]string             `json:"Annotations"`
	ManifestType string                        `json:"ManifestType"`
	User         string                        `json:"User"`
	History      []ociv1.History               `json:"History"`
	NamesHistory []string                      `json:"NamesHistory"`
	HealthCheck  *manifest.Schema2HealthConfig `json:"Healthcheck,omitempty"`
}

// DriverData includes data on the storage driver of the image.
type DriverData struct {
	Name string            `json:"Name"`
	Data map[string]string `json:"Data"`
}

// RootFS includes data on the root filesystem of the image.
type RootFS struct {
	Type   string          `json:"Type"`
	Layers []digest.Digest `json:"Layers"`
}

// InspectOptions allow for customizing inspecting images.
type InspectOptions struct {
	// Compute the size of the image (expensive).
	WithSize bool
	// Compute the parent of the image (expensive).
	WithParent bool
}

// Inspect inspects the image.
func (i *Image) Inspect(ctx context.Context, options *InspectOptions) (*ImageData, error) {
	logrus.Debugf("Inspecting image %s", i.ID())

	if options == nil {
		options = &InspectOptions{}
	}

	if i.cached.completeInspectData != nil {
		if options.WithSize && i.cached.completeInspectData.Size == int64(-1) {
			size, err := i.Size()
			if err != nil {
				return nil, err
			}
			i.cached.completeInspectData.Size = size
		}
		if options.WithParent && i.cached.completeInspectData.Parent == "" {
			parentImage, err := i.Parent(ctx)
			if err != nil {
				return nil, err
			}
			if parentImage != nil {
				i.cached.completeInspectData.Parent = parentImage.ID()
			}
		}
		return i.cached.completeInspectData, nil
	}

	// First assemble data that does not depend on the format of the image.
	info, err := i.inspectInfo(ctx)
	if err != nil {
		return nil, err
	}
	ociImage, err := i.toOCI(ctx)
	if err != nil {
		return nil, err
	}

	repoTags, err := i.RepoTags()
	if err != nil {
		return nil, err
	}
	repoDigests, err := i.RepoDigests()
	if err != nil {
		return nil, err
	}
	driverData, err := i.driverData()
	if err != nil {
		return nil, err
	}

	size := int64(-1)
	if options.WithSize {
		size, err = i.Size()
		if err != nil {
			return nil, err
		}
	}

	data := &ImageData{
		ID:           i.ID(),
		RepoTags:     repoTags,
		RepoDigests:  repoDigests,
		Created:      ociImage.Created,
		Author:       ociImage.Author,
		Architecture: ociImage.Architecture,
		Os:           ociImage.OS,
		Config:       &ociImage.Config,
		Version:      info.DockerVersion,
		Size:         size,
		VirtualSize:  size, // NOTE: same as size. Inherited from Docker where it's scheduled for deprecation.
		Digest:       i.Digest(),
		Labels:       info.Labels,
		RootFS: &RootFS{
			Type:   ociImage.RootFS.Type,
			Layers: ociImage.RootFS.DiffIDs,
		},
		GraphDriver:  driverData,
		User:         ociImage.Config.User,
		History:      ociImage.History,
		NamesHistory: i.NamesHistory(),
	}

	if options.WithParent {
		parentImage, err := i.Parent(ctx)
		if err != nil {
			return nil, err
		}
		if parentImage != nil {
			data.Parent = parentImage.ID()
		}
	}

	// Determine the format of the image.  How we determine certain data
	// depends on the format (e.g., Docker v2s2, OCI v1).
	src, err := i.source(ctx)
	if err != nil {
		return nil, err
	}
	manifestRaw, manifestType, err := image.UnparsedInstance(src, nil).Manifest(ctx)
	if err != nil {
		return nil, err
	}

	data.ManifestType = manifestType

	switch manifestType {
	// OCI image
	case ociv1.MediaTypeImageManifest:
		var ociManifest ociv1.Manifest
		if err := json.Unmarshal(manifestRaw, &ociManifest); err != nil {
			return nil, err
		}
		data.Annotations = ociManifest.Annotations
		if len(ociImage.History) > 0 {
			data.Comment = ociImage.History[0].Comment
		}

	// Docker image
	case manifest.DockerV2Schema2MediaType:
		rawConfig, err := i.rawConfigBlob(ctx)
		if err != nil {
			return nil, err
		}
		var dockerConfig manifest.Schema2V1Image
		if err := json.Unmarshal(rawConfig, &dockerConfig); err != nil {
			return nil, err
		}
		data.Comment = dockerConfig.Comment
		// NOTE: Health checks may be listed in the container config or
		// the config.
		data.HealthCheck = dockerConfig.ContainerConfig.Healthcheck
		if data.HealthCheck == nil && dockerConfig.Config != nil {
			data.HealthCheck = dockerConfig.Config.Healthcheck
		}

	case manifest.DockerV2Schema1MediaType, manifest.DockerV2Schema1SignedMediaType:
		// There seem to be at least _some_ images with .Healthcheck set in schema1 (possibly just as an artifact
		// of testing format conversion?), so this could plausibly read these values.
	}

	if data.Annotations == nil {
		// Podman compat
		data.Annotations = make(map[string]string)
	}

	i.cached.completeInspectData = data

	return data, nil
}

// inspectInfo returns the image inspect info.
func (i *Image) inspectInfo(ctx context.Context) (*types.ImageInspectInfo, error) {
	if i.cached.partialInspectData != nil {
		return i.cached.partialInspectData, nil
	}

	ref, err := i.StorageReference()
	if err != nil {
		return nil, err
	}

	img, err := ref.NewImage(ctx, &i.runtime.systemContext)
	if err != nil {
		return nil, err
	}
	defer img.Close()

	data, err := img.Inspect(ctx)
	if err != nil {
		return nil, err
	}

	i.cached.partialInspectData = data
	return data, nil
}
