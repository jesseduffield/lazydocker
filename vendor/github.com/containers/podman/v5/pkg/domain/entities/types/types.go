// copied from github.com/docker/docker/api/types
package types

import (
	"os"

	buildahDefine "github.com/containers/buildah/define"
)

// ComponentVersion describes the version information for a specific component.
type ComponentVersion struct {
	Name    string
	Version string
	Details map[string]string `json:",omitempty"`
}

// Version contains response of Engine API:
// GET "/version"
type Version struct {
	Platform   struct{ Name string }
	Components []ComponentVersion `json:",omitempty"`

	// The following fields are deprecated, they relate to the Engine component and are kept for backwards compatibility

	Version       string
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion,omitempty"`
	GitCommit     string
	GoVersion     string
	Os            string
	Arch          string
	KernelVersion string `json:",omitempty"`
	Experimental  bool   `json:",omitempty"`
	BuildTime     string `json:",omitempty"`
}

// SystemComponentVersion is the type used by pkg/domain/entities
type SystemComponentVersion struct {
	Version
}

// ContainerCreateResponse is the response struct for creating a container
type ContainerCreateResponse struct {
	// ID of the container created
	// required: true
	ID string `json:"Id"`
	// Warnings during container creation
	// required: true
	Warnings []string `json:"Warnings"`
}

// FarmBuildOptions describes the options for building container images on farm nodes
type FarmBuildOptions struct {
	// Cleanup removes built images from farm nodes on success
	Cleanup bool
	// Authfile is the path to the file holding registry credentials
	Authfile string
	// SkipTLSVerify skips tls verification when set to true
	SkipTLSVerify *bool
}

// BuildOptions describe the options for building container images.
type BuildOptions struct {
	buildahDefine.BuildOptions
	ContainerFiles []string
	FarmBuildOptions
	// Files that need to be closed after the build
	// so need to pass this to the main build functions
	LogFileToClose *os.File
	TmpDirToClose  string
}

// BuildReport is the image-build report.
type BuildReport struct {
	// ID of the image.
	ID string
	// Format to save the image in
	SaveFormat string
}

// swagger:model
type IDResponse struct {

	// The id of the newly created object.
	// Required: true
	ID string `json:"Id"`
}
