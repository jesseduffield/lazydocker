package volumes

// CreateOptions are optional options for creating volumes
//
//go:generate go run ../generator/generator.go CreateOptions
type CreateOptions struct {
}

// InspectOptions are optional options for inspecting volumes
//
//go:generate go run ../generator/generator.go InspectOptions
type InspectOptions struct {
}

// ListOptions are optional options for listing volumes
//
//go:generate go run ../generator/generator.go ListOptions
type ListOptions struct {
	// Filters applied to the listing of volumes
	Filters map[string][]string
}

// PruneOptions are optional options for pruning volumes
//
//go:generate go run ../generator/generator.go PruneOptions
type PruneOptions struct {
	// Filters applied to the pruning of volumes
	Filters map[string][]string
}

// RemoveOptions are optional options for removing volumes
//
//go:generate go run ../generator/generator.go RemoveOptions
type RemoveOptions struct {
	// Force removes the volume even if it is being used
	Force   *bool
	Timeout *uint
}

// ExistsOptions are optional options for checking
// if a volume exists
//
//go:generate go run ../generator/generator.go ExistsOptions
type ExistsOptions struct {
}
