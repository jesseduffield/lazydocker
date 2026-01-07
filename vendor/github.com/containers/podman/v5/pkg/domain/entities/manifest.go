package entities

import (
	entitiesTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"go.podman.io/image/v5/types"
)

// ManifestCreateOptions provides model for creating manifest list or image index
type ManifestCreateOptions struct {
	// True when adding lists to include all images
	All bool `schema:"all"`
	// Amend an extant list if there's already one with the desired name
	Amend bool `schema:"amend"`
	// Should TLS registry certificate be verified?
	SkipTLSVerify types.OptionalBool `json:"-" schema:"-"`
	// Annotations to set on the list, which forces it to be OCI format
	Annotations map[string]string `json:"annotations" schema:"annotations"`
}

// ManifestInspectOptions provides model for inspecting manifest
type ManifestInspectOptions struct {
	// Path to an authentication file.
	Authfile string `json:"-" schema:"-"`
	// Should TLS registry certificate be verified?
	SkipTLSVerify types.OptionalBool `json:"-" schema:"-"`
}

// ManifestAddOptions provides model for adding digests to manifest list
//
// swagger:model
type ManifestAddOptions struct {
	ManifestAnnotateOptions
	// True when operating on a list to include all images
	All bool `json:"all" schema:"all"`
	// authfile to use when pushing manifest list
	Authfile string `json:"-" schema:"-"`
	// Home directory for certificates when pushing a manifest list
	CertDir string `json:"-" schema:"-"`
	// Password to authenticate to registry when pushing manifest list
	Password string `json:"-" schema:"-"`
	// Should TLS registry certificate be verified?
	SkipTLSVerify types.OptionalBool `json:"-" schema:"-"`
	// Username to authenticate to registry when pushing manifest list
	Username string `json:"-" schema:"-"`
	// Images is an optional list of image references to add to manifest list
	Images []string `json:"images" schema:"images"`
}

// ManifestAddArtifactOptions provides the model for creating artifact manifests
// for files and adding those manifests to a manifest list
//
// swagger:model
type ManifestAddArtifactOptions struct {
	ManifestAnnotateOptions
	// Note to future maintainers: keep these fields synchronized with ManifestModifyOptions!
	Type          *string           `json:"artifact_type" schema:"artifact_type"`
	LayerType     string            `json:"artifact_layer_type" schema:"artifact_layer_type"`
	ConfigType    string            `json:"artifact_config_type" schema:"artifact_config_type"`
	Config        string            `json:"artifact_config" schema:"artifact_config"`
	ExcludeTitles bool              `json:"artifact_exclude_titles" schema:"artifact_exclude_titles"`
	Annotations   map[string]string `json:"artifact_annotations" schema:"artifact_annotations"`
	Subject       string            `json:"artifact_subject" schema:"artifact_subject"`
	Files         []string          `json:"artifact_files" schema:"-"`
}

// ManifestAnnotateOptions provides model for annotating manifest list
type ManifestAnnotateOptions struct {
	// Annotation to add to the item in the manifest list
	Annotation []string `json:"annotation" schema:"annotation"`
	// Annotations to add to the item in the manifest list by a map which is preferred over Annotation
	Annotations map[string]string `json:"annotations" schema:"annotations"`
	// Arch overrides the architecture for the item in the manifest list
	Arch string `json:"arch" schema:"arch"`
	// Feature list for the item in the manifest list
	Features []string `json:"features" schema:"features"`
	// OS overrides the operating system for the item in the manifest list
	OS string `json:"os" schema:"os"`
	// OS features for the item in the manifest list
	OSFeatures []string `json:"os_features" schema:"os_features"`
	// OSVersion overrides the operating system for the item in the manifest list
	OSVersion string `json:"os_version" schema:"os_version"`
	// Variant for the item in the manifest list
	Variant string `json:"variant" schema:"variant"`
	// IndexAnnotation is a slice of key=value annotations to add to the manifest list itself
	IndexAnnotation []string `json:"index_annotation" schema:"index_annotation"`
	// IndexAnnotations is a map of key:value annotations to add to the manifest list itself, by a map which is preferred over IndexAnnotation
	IndexAnnotations map[string]string `json:"index_annotations" schema:"index_annotations"`
	// IndexSubject is a subject value to set in the manifest list itself
	IndexSubject string `json:"subject" schema:"subject"`
}

// ManifestModifyOptions provides the model for mutating a manifest
//
// swagger 2.0 does not support oneOf for schema validation.
//
// Operation "update" uses all fields.
// Operation "remove" uses fields: Operation and Images
// Operation "annotate" uses fields: Operation and Annotations
//
// swagger:model
type ManifestModifyOptions struct {
	Operation string `json:"operation" schema:"operation"` // Valid values: update, remove, annotate
	ManifestAddOptions
	ManifestRemoveOptions
	// The following are all of the fields from ManifestAddArtifactOptions.
	// We can't just embed the whole structure because it embeds a
	// ManifestAnnotateOptions, which would conflict with the one that
	// ManifestAddOptions embeds.
	ArtifactType          *string           `json:"artifact_type" schema:"artifact_type"`
	ArtifactLayerType     string            `json:"artifact_layer_type" schema:"artifact_layer_type"`
	ArtifactConfigType    string            `json:"artifact_config_type" schema:"artifact_config_type"`
	ArtifactConfig        string            `json:"artifact_config" schema:"artifact_config"`
	ArtifactExcludeTitles bool              `json:"artifact_exclude_titles" schema:"artifact_exclude_titles"`
	ArtifactAnnotations   map[string]string `json:"artifact_annotations" schema:"artifact_annotations"`
	ArtifactSubject       string            `json:"artifact_subject" schema:"artifact_subject"`
	ArtifactFiles         []string          `json:"artifact_files" schema:"-"`
}

// ManifestPushReport provides the model for the pushed manifest
type ManifestPushReport = entitiesTypes.ManifestPushReport

// ManifestRemoveOptions provides the model for removing digests from a manifest
//
// swagger:model
type ManifestRemoveOptions struct {
}

// ManifestRemoveReport provides the model for the removed manifest
type ManifestRemoveReport = entitiesTypes.ManifestRemoveReport

// ManifestModifyReport provides the model for removed digests and changed manifest
type ManifestModifyReport = entitiesTypes.ManifestModifyReport
