package manifest

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/internal/manifest"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/pkg/strslice"
	"go.podman.io/image/v5/types"
)

// Schema2Descriptor is a “descriptor” in docker/distribution schema 2.
type Schema2Descriptor = manifest.Schema2Descriptor

// BlobInfoFromSchema2Descriptor returns a types.BlobInfo based on the input schema 2 descriptor.
func BlobInfoFromSchema2Descriptor(desc Schema2Descriptor) types.BlobInfo {
	return types.BlobInfo{
		Digest:    desc.Digest,
		Size:      desc.Size,
		URLs:      desc.URLs,
		MediaType: desc.MediaType,
	}
}

// Schema2 is a manifest in docker/distribution schema 2.
type Schema2 struct {
	SchemaVersion     int                 `json:"schemaVersion"`
	MediaType         string              `json:"mediaType"`
	ConfigDescriptor  Schema2Descriptor   `json:"config"`
	LayersDescriptors []Schema2Descriptor `json:"layers"`
}

// Schema2Port is a Port, a string containing port number and protocol in the
// format "80/tcp", from docker/go-connections/nat.
type Schema2Port string

// Schema2PortSet is a PortSet, a collection of structs indexed by Port, from
// docker/go-connections/nat.
type Schema2PortSet map[Schema2Port]struct{}

// Schema2HealthConfig is a HealthConfig, which holds configuration settings
// for the HEALTHCHECK feature, from docker/docker/api/types/container.
type Schema2HealthConfig struct {
	// Test is the test to perform to check that the container is healthy.
	// An empty slice means to inherit the default.
	// The options are:
	// {} : inherit healthcheck
	// {"NONE"} : disable healthcheck
	// {"CMD", args...} : exec arguments directly
	// {"CMD-SHELL", command} : run command with system's default shell
	Test []string `json:",omitempty"`

	// Zero means to inherit. Durations are expressed as integer nanoseconds.
	StartPeriod   time.Duration `json:",omitempty"` // StartPeriod is the time to wait after starting before running the first check.
	StartInterval time.Duration `json:",omitempty"` // StartInterval is the time to wait between checks during the start period.
	Interval      time.Duration `json:",omitempty"` // Interval is the time to wait between checks.
	Timeout       time.Duration `json:",omitempty"` // Timeout is the time to wait before considering the check to have hung.

	// Retries is the number of consecutive failures needed to consider a container as unhealthy.
	// Zero means inherit.
	Retries int `json:",omitempty"`
}

// Schema2Config is a Config in docker/docker/api/types/container.
type Schema2Config struct {
	Hostname        string               // Hostname
	Domainname      string               // Domainname
	User            string               // User that will run the command(s) inside the container, also support user:group
	AttachStdin     bool                 // Attach the standard input, makes possible user interaction
	AttachStdout    bool                 // Attach the standard output
	AttachStderr    bool                 // Attach the standard error
	ExposedPorts    Schema2PortSet       `json:",omitempty"` // List of exposed ports
	Tty             bool                 // Attach standard streams to a tty, including stdin if it is not closed.
	OpenStdin       bool                 // Open stdin
	StdinOnce       bool                 // If true, close stdin after the 1 attached client disconnects.
	Env             []string             // List of environment variable to set in the container
	Cmd             strslice.StrSlice    // Command to run when starting the container
	Healthcheck     *Schema2HealthConfig `json:",omitempty"` // Healthcheck describes how to check the container is healthy
	ArgsEscaped     bool                 `json:",omitempty"` // True if command is already escaped (Windows specific)
	Image           string               // Name of the image as it was passed by the operator (e.g. could be symbolic)
	Volumes         map[string]struct{}  // List of volumes (mounts) used for the container
	WorkingDir      string               // Current directory (PWD) in the command will be launched
	Entrypoint      strslice.StrSlice    // Entrypoint to run when starting the container
	NetworkDisabled bool                 `json:",omitempty"` // Is network disabled
	MacAddress      string               `json:",omitempty"` // Mac Address of the container
	OnBuild         []string             // ONBUILD metadata that were defined on the image Dockerfile
	Labels          map[string]string    // List of labels set to this container
	StopSignal      string               `json:",omitempty"` // Signal to stop a container
	StopTimeout     *int                 `json:",omitempty"` // Timeout (in seconds) to stop a container
	Shell           strslice.StrSlice    `json:",omitempty"` // Shell for shell-form of RUN, CMD, ENTRYPOINT
}

// Schema2V1Image is a V1Image in docker/docker/image.
type Schema2V1Image struct {
	// ID is a unique 64 character identifier of the image
	ID string `json:"id,omitempty"`
	// Parent is the ID of the parent image
	Parent string `json:"parent,omitempty"`
	// Comment is the commit message that was set when committing the image
	Comment string `json:"comment,omitempty"`
	// Created is the timestamp at which the image was created
	Created time.Time `json:"created"`
	// Container is the id of the container used to commit
	Container string `json:"container,omitempty"`
	// ContainerConfig is the configuration of the container that is committed into the image
	ContainerConfig Schema2Config `json:"container_config,omitempty"`
	// DockerVersion specifies the version of Docker that was used to build the image
	DockerVersion string `json:"docker_version,omitempty"`
	// Author is the name of the author that was specified when committing the image
	Author string `json:"author,omitempty"`
	// Config is the configuration of the container received from the client
	Config *Schema2Config `json:"config,omitempty"`
	// Architecture is the hardware that the image is built and runs on
	Architecture string `json:"architecture,omitempty"`
	// Variant is a variant of the CPU that the image is built and runs on
	Variant string `json:"variant,omitempty"`
	// OS is the operating system used to built and run the image
	OS string `json:"os,omitempty"`
	// Size is the total size of the image including all layers it is composed of
	Size int64 `json:",omitempty"`
}

// Schema2RootFS is a description of how to build up an image's root filesystem, from docker/docker/image.
type Schema2RootFS struct {
	Type    string          `json:"type"`
	DiffIDs []digest.Digest `json:"diff_ids,omitempty"`
}

// Schema2History stores build commands that were used to create an image, from docker/docker/image.
type Schema2History struct {
	// Created is the timestamp at which the image was created
	Created time.Time `json:"created"`
	// Author is the name of the author that was specified when committing the image
	Author string `json:"author,omitempty"`
	// CreatedBy keeps the Dockerfile command used while building the image
	CreatedBy string `json:"created_by,omitempty"`
	// Comment is the commit message that was set when committing the image
	Comment string `json:"comment,omitempty"`
	// EmptyLayer is set to true if this history item did not generate a
	// layer. Otherwise, the history item is associated with the next
	// layer in the RootFS section.
	EmptyLayer bool `json:"empty_layer,omitempty"`
}

// Schema2Image is an Image in docker/docker/image.
type Schema2Image struct {
	Schema2V1Image
	Parent     digest.Digest    `json:"parent,omitempty"`
	RootFS     *Schema2RootFS   `json:"rootfs,omitempty"`
	History    []Schema2History `json:"history,omitempty"`
	OSVersion  string           `json:"os.version,omitempty"`
	OSFeatures []string         `json:"os.features,omitempty"`
}

// Schema2FromManifest creates a Schema2 manifest instance from a manifest blob.
func Schema2FromManifest(manifestBlob []byte) (*Schema2, error) {
	s2 := Schema2{}
	if err := json.Unmarshal(manifestBlob, &s2); err != nil {
		return nil, err
	}
	if err := manifest.ValidateUnambiguousManifestFormat(manifestBlob, DockerV2Schema2MediaType,
		manifest.AllowedFieldConfig|manifest.AllowedFieldLayers); err != nil {
		return nil, err
	}
	// Check manifest's and layers' media types.
	if err := SupportedSchema2MediaType(s2.MediaType); err != nil {
		return nil, err
	}
	for _, layer := range s2.LayersDescriptors {
		if err := SupportedSchema2MediaType(layer.MediaType); err != nil {
			return nil, err
		}
	}
	return &s2, nil
}

// Schema2FromComponents creates an Schema2 manifest instance from the supplied data.
func Schema2FromComponents(config Schema2Descriptor, layers []Schema2Descriptor) *Schema2 {
	return &Schema2{
		SchemaVersion:     2,
		MediaType:         DockerV2Schema2MediaType,
		ConfigDescriptor:  config,
		LayersDescriptors: layers,
	}
}

// Schema2Clone creates a copy of the supplied Schema2 manifest.
func Schema2Clone(src *Schema2) *Schema2 {
	copy := *src
	return &copy
}

// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
func (m *Schema2) ConfigInfo() types.BlobInfo {
	return BlobInfoFromSchema2Descriptor(m.ConfigDescriptor)
}

// LayerInfos returns a list of LayerInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (m *Schema2) LayerInfos() []LayerInfo {
	blobs := make([]LayerInfo, 0, len(m.LayersDescriptors))
	for _, layer := range m.LayersDescriptors {
		blobs = append(blobs, LayerInfo{
			BlobInfo:   BlobInfoFromSchema2Descriptor(layer),
			EmptyLayer: false,
		})
	}
	return blobs
}

var schema2CompressionMIMETypeSets = []compressionMIMETypeSet{
	{
		mtsUncompressed:                    DockerV2Schema2ForeignLayerMediaType,
		compressiontypes.GzipAlgorithmName: DockerV2Schema2ForeignLayerMediaTypeGzip,
		compressiontypes.ZstdAlgorithmName: mtsUnsupportedMIMEType,
	},
	{
		mtsUncompressed:                    DockerV2SchemaLayerMediaTypeUncompressed,
		compressiontypes.GzipAlgorithmName: DockerV2Schema2LayerMediaType,
		compressiontypes.ZstdAlgorithmName: mtsUnsupportedMIMEType,
	},
}

// UpdateLayerInfos replaces the original layers with the specified BlobInfos (size+digest+urls), in order (the root layer first, and then successive layered layers)
// The returned error will be a manifest.ManifestLayerCompressionIncompatibilityError if any of the layerInfos includes a combination of CompressionOperation and
// CompressionAlgorithm that would result in anything other than gzip compression.
func (m *Schema2) UpdateLayerInfos(layerInfos []types.BlobInfo) error {
	if len(m.LayersDescriptors) != len(layerInfos) {
		return fmt.Errorf("Error preparing updated manifest: layer count changed from %d to %d", len(m.LayersDescriptors), len(layerInfos))
	}
	original := m.LayersDescriptors
	m.LayersDescriptors = make([]Schema2Descriptor, len(layerInfos))
	for i, info := range layerInfos {
		mimeType := original[i].MediaType
		// First make sure we support the media type of the original layer.
		if err := SupportedSchema2MediaType(mimeType); err != nil {
			return fmt.Errorf("Error preparing updated manifest: unknown media type of original layer %q: %q", info.Digest, mimeType)
		}
		mimeType, err := updatedMIMEType(schema2CompressionMIMETypeSets, mimeType, info)
		if err != nil {
			return fmt.Errorf("preparing updated manifest, layer %q: %w", info.Digest, err)
		}
		m.LayersDescriptors[i].MediaType = mimeType
		m.LayersDescriptors[i].Digest = info.Digest
		m.LayersDescriptors[i].Size = info.Size
		m.LayersDescriptors[i].URLs = info.URLs
		if info.CryptoOperation != types.PreserveOriginalCrypto {
			return fmt.Errorf("encryption change (for layer %q) is not supported in schema2 manifests", info.Digest)
		}
	}
	return nil
}

// Serialize returns the manifest in a blob format.
// NOTE: Serialize() does not in general reproduce the original blob if this object was loaded from one, even if no modifications were made!
func (m *Schema2) Serialize() ([]byte, error) {
	return json.Marshal(*m)
}

// Inspect returns various information for (skopeo inspect) parsed from the manifest and configuration.
func (m *Schema2) Inspect(configGetter func(types.BlobInfo) ([]byte, error)) (*types.ImageInspectInfo, error) {
	config, err := configGetter(m.ConfigInfo())
	if err != nil {
		return nil, err
	}
	s2 := &Schema2Image{}
	if err := json.Unmarshal(config, s2); err != nil {
		return nil, err
	}
	layerInfos := m.LayerInfos()
	i := &types.ImageInspectInfo{
		Tag:           "",
		Created:       &s2.Created,
		DockerVersion: s2.DockerVersion,
		Architecture:  s2.Architecture,
		Variant:       s2.Variant,
		Os:            s2.OS,
		Layers:        layerInfosToStrings(layerInfos),
		LayersData:    imgInspectLayersFromLayerInfos(layerInfos),
		Author:        s2.Author,
	}
	if s2.Config != nil {
		i.Labels = s2.Config.Labels
		i.Env = s2.Config.Env
	}
	return i, nil
}

// ImageID computes an ID which can uniquely identify this image by its contents.
func (m *Schema2) ImageID([]digest.Digest) (string, error) {
	if err := m.ConfigDescriptor.Digest.Validate(); err != nil {
		return "", err
	}
	return m.ConfigDescriptor.Digest.Encoded(), nil
}

// CanChangeLayerCompression returns true if we can compress/decompress layers with mimeType in the current image
// (and the code can handle that).
// NOTE: Even if this returns true, the relevant format might not accept all compression algorithms; the set of accepted
// algorithms depends not on the current format, but possibly on the target of a conversion.
func (m *Schema2) CanChangeLayerCompression(mimeType string) bool {
	return compressionVariantsRecognizeMIMEType(schema2CompressionMIMETypeSets, mimeType)
}
