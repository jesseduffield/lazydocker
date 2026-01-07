package system

// EventsOptions are optional options for monitoring events
//
//go:generate go run ../generator/generator.go EventsOptions
type EventsOptions struct {
	Filters map[string][]string
	Since   *string
	Stream  *bool
	Until   *string
}

// PruneOptions are optional options for pruning
//
//go:generate go run ../generator/generator.go PruneOptions
type PruneOptions struct {
	All      *bool
	Filters  map[string][]string
	Volumes  *bool
	External *bool
	Build    *bool
}

// VersionOptions are optional options for getting version info
//
//go:generate go run ../generator/generator.go VersionOptions
type VersionOptions struct {
}

// DiskOptions are optional options for getting storage consumption
//
//go:generate go run ../generator/generator.go DiskOptions
type DiskOptions struct {
}

// InfoOptions are optional options for getting info
// about libpod
//
//go:generate go run ../generator/generator.go InfoOptions
type InfoOptions struct {
}

// CheckOptions are optional options for storage consistency check/repair
//
//go:generate go run ../generator/generator.go CheckOptions
type CheckOptions struct {
	Quick                       *bool   `schema:"quick"`
	Repair                      *bool   `schema:"repair"`
	RepairLossy                 *bool   `schema:"repair_lossy"`
	UnreferencedLayerMaximumAge *string `schema:"unreferenced_layer_max_age"`
}
