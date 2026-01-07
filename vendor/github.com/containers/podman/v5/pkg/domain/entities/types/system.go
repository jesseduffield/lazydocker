package types

import (
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/domain/entities/reports"
)

// ServiceOptions provides the input for starting an API and sidecar pprof services
type ServiceOptions struct {
	CorsHeaders     string        // Cross-Origin Resource Sharing (CORS) headers
	PProfAddr       string        // Network address to bind pprof profiles service
	Timeout         time.Duration // Duration of inactivity the service should wait before shutting down
	URI             string        // Path to unix domain socket service should listen on
	TLSCertFile     string        // Path to serving certificate PEM file
	TLSKeyFile      string        // Path to serving certificate key PEM file
	TLSClientCAFile string        // Path to client certificate authority
}

// SystemCheckOptions provides options for checking storage consistency.
type SystemCheckOptions struct {
	Quick                       bool           // skip the most time-intensive checks
	Repair                      bool           // remove damaged images
	RepairLossy                 bool           // remove damaged containers
	UnreferencedLayerMaximumAge *time.Duration // maximum allowed age for unreferenced layers
}

// SystemCheckReport provides a report of what a storage consistency check
// found, and if we removed anything that was damaged, what we removed.
type SystemCheckReport struct {
	Errors            bool                // any errors were detected
	Layers            map[string][]string // layer ID → what was detected
	ROLayers          map[string][]string // layer ID → what was detected
	RemovedLayers     []string            // layer ID
	Images            map[string][]string // image ID → what was detected
	ROImages          map[string][]string // image ID → what was detected
	RemovedImages     map[string][]string // image ID → names
	Containers        map[string][]string // container ID → what was detected
	RemovedContainers map[string]string   // container ID → name
}

// SystemPruneOptions provides options to prune system.
type SystemPruneOptions struct {
	All      bool
	Volume   bool
	Filters  map[string][]string `json:"filters" schema:"filters"`
	External bool
	Build    bool
}

// SystemPruneReport provides report after system prune is executed.
type SystemPruneReport struct {
	PodPruneReport        []*PodPruneReport
	ContainerPruneReports []*reports.PruneReport
	ImagePruneReports     []*reports.PruneReport
	NetworkPruneReports   []*NetworkPruneReport
	VolumePruneReports    []*reports.PruneReport
	ReclaimedSpace        uint64
}

// SystemMigrateOptions describes the options needed for the
// cli to migrate runtimes of containers
type SystemMigrateOptions struct {
	NewRuntime string
}

// SystemDfOptions describes the options for getting df information
type SystemDfOptions struct {
	Format  string
	Verbose bool
}

// SystemDfReport describes the response for df information
type SystemDfReport struct {
	ImagesSize int64
	Images     []*SystemDfImageReport
	Containers []*SystemDfContainerReport
	Volumes    []*SystemDfVolumeReport
}

// SystemDfImageReport describes an image for use with df
type SystemDfImageReport struct {
	Repository string
	Tag        string
	ImageID    string
	Created    time.Time
	Size       int64
	SharedSize int64
	UniqueSize int64
	Containers int
}

// SystemDfContainerReport describes a container for use with df
type SystemDfContainerReport struct {
	ContainerID  string
	Image        string
	Command      []string
	LocalVolumes int
	Size         int64
	RWSize       int64
	Created      time.Time
	Status       string
	Names        string
}

// SystemDfVolumeReport describes a volume and its size
type SystemDfVolumeReport struct {
	VolumeName      string
	Links           int
	Size            int64
	ReclaimableSize int64
}

// SystemVersionReport describes version information about the running Podman service
type SystemVersionReport struct {
	// Always populated
	Client *define.Version `json:",omitempty"`
	// May be populated, when in tunnel mode
	Server *define.Version `json:",omitempty"`
}

// SystemUnshareOptions describes the options for the unshare command
type SystemUnshareOptions struct {
	RootlessNetNS bool
}

// ListRegistriesReport is the report when querying for a sorted list of
// registries which may be contacted during certain operations.
type ListRegistriesReport struct {
	Registries []string
}

// AuthReport describes the response for authentication check
type AuthReport struct {
	IdentityToken string
	Status        string
}

// LocksReport describes any conflicts in Libpod's lock allocations that could
// lead to deadlocks.
type LocksReport struct {
	LockConflicts map[uint32][]string
	LocksHeld     []uint32
}
