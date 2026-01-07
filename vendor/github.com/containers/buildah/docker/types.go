package docker

//
//  Types extracted from Docker
//

import (
	"time"

	digest "github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/pkg/strslice"
)

// github.com/moby/moby/image/rootfs.go
const TypeLayers = "layers"

// github.com/docker/distribution/manifest/schema2/manifest.go
const V2S2MediaTypeUncompressedLayer = "application/vnd.docker.image.rootfs.diff.tar"

// github.com/moby/moby/image/rootfs.go
// V2S2RootFS describes images root filesystem
// This is currently a placeholder that only supports layers. In the future
// this can be made into an interface that supports different implementations.
type V2S2RootFS struct {
	Type    string          `json:"type"`
	DiffIDs []digest.Digest `json:"diff_ids,omitempty"`
}

// github.com/moby/moby/image/image.go
// V2S2History stores build commands that were used to create an image
type V2S2History struct {
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

// github.com/moby/moby/image/image.go
// ID is the content-addressable ID of an image.
type ID digest.Digest

// github.com/moby/moby/api/types/container/config.go
// HealthConfig holds configuration settings for the HEALTHCHECK feature.
type HealthConfig struct {
	// Test is the test to perform to check that the container is healthy.
	// An empty slice means to inherit the default.
	// The options are:
	// {} : inherit healthcheck
	// {"NONE"} : disable healthcheck
	// {"CMD", args...} : exec arguments directly
	// {"CMD-SHELL", command} : run command with system's default shell
	Test []string `json:",omitempty"`

	// Zero means to inherit. Durations are expressed as integer nanoseconds.
	Interval      time.Duration `json:",omitempty"` // Interval is the time to wait between checks.
	Timeout       time.Duration `json:",omitempty"` // Timeout is the time to wait before considering the check to have hung.
	StartPeriod   time.Duration `json:",omitempty"` // Time to wait after the container starts before running the first check.
	StartInterval time.Duration `json:",omitempty"` // Time to wait between checks during the StartPeriod.

	// Retries is the number of consecutive failures needed to consider a container as unhealthy.
	// Zero means inherit.
	Retries int `json:",omitempty"`
}

// github.com/docker/go-connections/nat/nat.go
// PortSet is a collection of structs indexed by Port
type PortSet map[Port]struct{}

// github.com/docker/go-connections/nat/nat.go
// Port is a string containing port number and protocol in the format "80/tcp"
type Port string

// github.com/moby/moby/api/types/container/config.go
// Config contains the configuration data about a container.
// It should hold only portable information about the container.
// Here, "portable" means "independent from the host we are running on".
// Non-portable information *should* appear in HostConfig.
// All fields added to this struct must be marked `omitempty` to keep getting
// predictable hashes from the old `v1Compatibility` configuration.
type Config struct {
	Hostname        string              // Hostname
	Domainname      string              // Domainname
	User            string              // User that will run the command(s) inside the container, also support user:group
	AttachStdin     bool                // Attach the standard input, makes possible user interaction
	AttachStdout    bool                // Attach the standard output
	AttachStderr    bool                // Attach the standard error
	ExposedPorts    PortSet             `json:",omitempty"` // List of exposed ports
	Tty             bool                // Attach standard streams to a tty, including stdin if it is not closed.
	OpenStdin       bool                // Open stdin
	StdinOnce       bool                // If true, close stdin after the 1 attached client disconnects.
	Env             []string            // List of environment variable to set in the container
	Cmd             strslice.StrSlice   // Command to run when starting the container
	Healthcheck     *HealthConfig       `json:",omitempty"` // Healthcheck describes how to check the container is healthy
	ArgsEscaped     bool                `json:",omitempty"` // True if command is already escaped (Windows specific)
	Image           string              // Name of the image as it was passed by the operator (e.g. could be symbolic)
	Volumes         map[string]struct{} // List of volumes (mounts) used for the container
	WorkingDir      string              // Current directory (PWD) in the command will be launched
	Entrypoint      strslice.StrSlice   // Entrypoint to run when starting the container
	NetworkDisabled bool                `json:",omitempty"` // Is network disabled
	MacAddress      string              `json:",omitempty"` // Mac Address of the container
	OnBuild         []string            // ONBUILD metadata that were defined on the image Dockerfile
	Labels          map[string]string   // List of labels set to this container
	StopSignal      string              `json:",omitempty"` // Signal to stop a container
	StopTimeout     *int                `json:",omitempty"` // Timeout (in seconds) to stop a container
	Shell           strslice.StrSlice   `json:",omitempty"` // Shell for shell-form of RUN, CMD, ENTRYPOINT
}

// github.com/docker/distribution/manifest/schema1/config_builder.go
// For non-top-level layers, create fake V1Compatibility strings that
// fit the format and don't collide with anything else, but don't
// result in runnable images on their own.
type V1Compatibility struct {
	ID              string    `json:"id"`
	Parent          string    `json:"parent,omitempty"`
	Comment         string    `json:"comment,omitempty"`
	Created         time.Time `json:"created"`
	ContainerConfig struct {
		Cmd []string
	} `json:"container_config"`
	Author    string `json:"author,omitempty"`
	ThrowAway bool   `json:"throwaway,omitempty"`
}

// github.com/moby/moby/image/image.go
// V1Image stores the V1 image configuration.
type V1Image struct {
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
	ContainerConfig Config `json:"container_config"`
	// DockerVersion specifies the version of Docker that was used to build the image
	DockerVersion string `json:"docker_version,omitempty"`
	// Author is the name of the author that was specified when committing the image
	Author string `json:"author,omitempty"`
	// Config is the configuration of the container received from the client
	Config *Config `json:"config,omitempty"`
	// Architecture is the hardware that the image is build and runs on
	Architecture string `json:"architecture,omitempty"`
	// Variant is a variant of the CPU that the image is built and runs on
	Variant string `json:"variant,omitempty"`
	// OS is the operating system used to build and run the image
	OS string `json:"os,omitempty"`
	// Size is the total size of the image including all layers it is composed of
	Size int64 `json:",omitempty"`
}

// github.com/moby/moby/image/image.go
// V2Image stores the image configuration
type V2Image struct {
	V1Image
	Parent     ID            `json:"parent,omitempty"`
	RootFS     *V2S2RootFS   `json:"rootfs,omitempty"`
	History    []V2S2History `json:"history,omitempty"`
	OSVersion  string        `json:"os.version,omitempty"`
	OSFeatures []string      `json:"os.features,omitempty"`
}

// github.com/docker/distribution/manifest/versioned.go
// V2Versioned provides a struct with the manifest schemaVersion and mediaType.
// Incoming content with unknown schema version can be decoded against this
// struct to check the version.
type V2Versioned struct {
	// SchemaVersion is the image manifest schema that this image follows
	SchemaVersion int `json:"schemaVersion"`

	// MediaType is the media type of this schema.
	MediaType string `json:"mediaType,omitempty"`
}

// github.com/docker/distribution/manifest/schema1/manifest.go
// V2S1FSLayer is a container struct for BlobSums defined in an image manifest
type V2S1FSLayer struct {
	// BlobSum is the tarsum of the referenced filesystem image layer
	BlobSum digest.Digest `json:"blobSum"`
}

// github.com/docker/distribution/manifest/schema1/manifest.go
// V2S1History stores unstructured v1 compatibility information
type V2S1History struct {
	// V1Compatibility is the raw v1 compatibility information
	V1Compatibility string `json:"v1Compatibility"`
}

// github.com/docker/distribution/manifest/schema1/manifest.go
// V2S1Manifest provides the base accessible fields for working with V2 image
// format in the registry.
type V2S1Manifest struct {
	V2Versioned

	// Name is the name of the image's repository
	Name string `json:"name"`

	// Tag is the tag of the image specified by this manifest
	Tag string `json:"tag"`

	// Architecture is the host architecture on which this image is intended to
	// run
	Architecture string `json:"architecture"`

	// FSLayers is a list of filesystem layer blobSums contained in this image
	FSLayers []V2S1FSLayer `json:"fsLayers"`

	// History is a list of unstructured historical data for v1 compatibility
	History []V2S1History `json:"history"`
}

// github.com/docker/distribution/blobs.go
// V2S2Descriptor describes targeted content. Used in conjunction with a blob
// store, a descriptor can be used to fetch, store and target any kind of
// blob. The struct also describes the wire protocol format. Fields should
// only be added but never changed.
type V2S2Descriptor struct {
	// MediaType describe the type of the content. All text based formats are
	// encoded as utf-8.
	MediaType string `json:"mediaType,omitempty"`

	// Size in bytes of content.
	Size int64 `json:"size,omitempty"`

	// Digest uniquely identifies the content. A byte stream can be verified
	// against against this digest.
	Digest digest.Digest `json:"digest,omitempty"`

	// URLs contains the source URLs of this content.
	URLs []string `json:"urls,omitempty"`

	// NOTE: Before adding a field here, please ensure that all
	// other options have been exhausted. Much of the type relationships
	// depend on the simplicity of this type.
}

// github.com/docker/distribution/manifest/schema2/manifest.go
// V2S2Manifest defines a schema2 manifest.
type V2S2Manifest struct {
	V2Versioned

	// Config references the image configuration as a blob.
	Config V2S2Descriptor `json:"config"`

	// Layers lists descriptors for the layers referenced by the
	// configuration.
	Layers []V2S2Descriptor `json:"layers"`
}
