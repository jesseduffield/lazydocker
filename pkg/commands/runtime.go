package commands

import "context"

// ContainerRuntime abstracts Podman operations for both socket and socket-less modes.
// This interface allows the application to work with either:
// - Socket mode: using pkg/bindings (stable REST API)
// - Socket-less mode: using libpod directly (requires Linux + CGO)
type ContainerRuntime interface {
	// Container operations
	ListContainers(ctx context.Context) ([]ContainerSummary, error)
	InspectContainer(ctx context.Context, id string) (*ContainerDetails, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout *int) error
	PauseContainer(ctx context.Context, id string) error
	UnpauseContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string, timeout *int) error
	RemoveContainer(ctx context.Context, id string, force bool, volumes bool) error
	ContainerTop(ctx context.Context, id string) ([]string, [][]string, error)
	PruneContainers(ctx context.Context) error
	ContainerStats(ctx context.Context, id string, stream bool) (<-chan ContainerStatsEntry, <-chan error)

	// Image operations
	ListImages(ctx context.Context) ([]ImageSummary, error)
	InspectImage(ctx context.Context, id string) (*ImageDetails, error)
	ImageHistory(ctx context.Context, id string) ([]ImageHistoryEntry, error)
	RemoveImage(ctx context.Context, id string, force bool) error
	PruneImages(ctx context.Context) error

	// Volume operations
	ListVolumes(ctx context.Context) ([]VolumeSummary, error)
	RemoveVolume(ctx context.Context, name string, force bool) error
	PruneVolumes(ctx context.Context) error

	// Network operations
	ListNetworks(ctx context.Context) ([]NetworkSummary, error)
	RemoveNetwork(ctx context.Context, name string) error
	PruneNetworks(ctx context.Context) error

	// Pod operations
	ListPods(ctx context.Context) ([]PodSummary, error)
	PodStats(ctx context.Context, id string, stream bool) (<-chan PodStatsEntry, <-chan error)
	RestartPod(ctx context.Context, id string, timeout *int) error

	// Events streams container/image/volume/network events
	Events(ctx context.Context) (<-chan Event, <-chan error)

	// Lifecycle
	Close() error

	// Mode returns "socket" or "libpod" to indicate which runtime mode is active
	Mode() string
}
