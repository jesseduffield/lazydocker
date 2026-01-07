package types

// swagger:model
type ManifestPushReport struct {
	// ID of the pushed manifest
	ID string `json:"Id"`
	// Stream used to provide push progress
	Stream string `json:"stream,omitempty"`
	// Error contains text of errors from pushing
	Error string `json:"error,omitempty"`
}

// swagger:model
type ManifestModifyReport struct {
	// Manifest List ID
	ID string `json:"Id"`
	// Images added to or removed from manifest list, otherwise not provided.
	Images []string `json:"images,omitempty" schema:"images"`
	// Files added to manifest list, otherwise not provided.
	Files []string `json:"files,omitempty" schema:"files"`
	// Errors associated with operation
	Errors []error `json:"errors,omitempty"`
}

// swagger:model
type ManifestRemoveReport struct {
	// Deleted manifest list.
	Deleted []string `json:",omitempty"`
	// Untagged images. Can be longer than Deleted.
	Untagged []string `json:",omitempty"`
	// Errors associated with operation
	Errors []string `json:",omitempty"`
	// ExitCode describes the exit codes as described in the `podman rmi`
	// man page.
	ExitCode int
}
