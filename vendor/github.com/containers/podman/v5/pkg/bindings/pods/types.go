package pods

// CreateOptions are optional options for creating pods
//
//go:generate go run ../generator/generator.go CreateOptions
type CreateOptions struct {
}

// InspectOptions are optional options for inspecting pods
//
//go:generate go run ../generator/generator.go InspectOptions
type InspectOptions struct {
}

// KillOptions are optional options for killing pods
//
//go:generate go run ../generator/generator.go KillOptions
type KillOptions struct {
	Signal *string
}

// PauseOptions are optional options for pausing pods
//
//go:generate go run ../generator/generator.go PauseOptions
type PauseOptions struct {
}

// PruneOptions are optional options for pruning pods
//
//go:generate go run ../generator/generator.go PruneOptions
type PruneOptions struct {
}

// ListOptions are optional options for listing pods
//
//go:generate go run ../generator/generator.go ListOptions
type ListOptions struct {
	Filters map[string][]string
}

// RestartOptions are optional options for restarting pods
//
//go:generate go run ../generator/generator.go RestartOptions
type RestartOptions struct {
}

// StartOptions are optional options for starting pods
//
//go:generate go run ../generator/generator.go StartOptions
type StartOptions struct {
}

// StopOptions are optional options for stopping pods
//
//go:generate go run ../generator/generator.go StopOptions
type StopOptions struct {
	Timeout *int
}

// TopOptions are optional options for getting top on pods
//
//go:generate go run ../generator/generator.go TopOptions
type TopOptions struct {
	Descriptors []string
}

// UnpauseOptions are optional options for unpausinging pods
//
//go:generate go run ../generator/generator.go UnpauseOptions
type UnpauseOptions struct {
}

// StatsOptions are optional options for getting stats of pods
//
//go:generate go run ../generator/generator.go StatsOptions
type StatsOptions struct {
	All *bool
}

// RemoveOptions are optional options for removing pods
//
//go:generate go run ../generator/generator.go RemoveOptions
type RemoveOptions struct {
	Force   *bool
	Timeout *uint
}

// ExistsOptions are optional options for checking if a pod exists
//
//go:generate go run ../generator/generator.go ExistsOptions
type ExistsOptions struct {
}
