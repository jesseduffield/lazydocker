//go:build !linux || !cgo

package commands

import (
	"context"
	"errors"
)

// LibpodRuntime is a stub for non-Linux platforms.
// The real implementation requires CGO and is only available on Linux.
type LibpodRuntime struct{}

// ErrLibpodNotAvailable is returned when libpod is not available on the current platform.
var ErrLibpodNotAvailable = errors.New("libpod runtime not available on this platform (requires Linux with CGO)")

// NewLibpodRuntime returns an error on non-Linux platforms.
func NewLibpodRuntime() (*LibpodRuntime, error) {
	return nil, ErrLibpodNotAvailable
}

// Mode returns "libpod".
func (r *LibpodRuntime) Mode() string {
	return "libpod"
}

// Close is a no-op on non-Linux platforms.
func (r *LibpodRuntime) Close() error {
	return nil
}

// Container operations - all return ErrLibpodNotAvailable

func (r *LibpodRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	return nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) InspectContainer(ctx context.Context, id string) (*ContainerDetails, error) {
	return nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) StartContainer(ctx context.Context, id string) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) StopContainer(ctx context.Context, id string, timeout *int) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) PauseContainer(ctx context.Context, id string) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) UnpauseContainer(ctx context.Context, id string) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) RestartContainer(ctx context.Context, id string, timeout *int) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) RemoveContainer(ctx context.Context, id string, force bool, volumes bool) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) ContainerTop(ctx context.Context, id string) ([]string, [][]string, error) {
	return nil, nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) PruneContainers(ctx context.Context) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) ContainerStats(ctx context.Context, id string, stream bool) (<-chan ContainerStatsEntry, <-chan error) {
	errChan := make(chan error, 1)
	errChan <- ErrLibpodNotAvailable
	close(errChan)
	return nil, errChan
}

// Image operations - all return ErrLibpodNotAvailable

func (r *LibpodRuntime) ListImages(ctx context.Context) ([]ImageSummary, error) {
	return nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) InspectImage(ctx context.Context, id string) (*ImageDetails, error) {
	return nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) ImageHistory(ctx context.Context, id string) ([]ImageHistoryEntry, error) {
	return nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) RemoveImage(ctx context.Context, id string, force bool) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) PruneImages(ctx context.Context) error {
	return ErrLibpodNotAvailable
}

// Volume operations - all return ErrLibpodNotAvailable

func (r *LibpodRuntime) ListVolumes(ctx context.Context) ([]VolumeSummary, error) {
	return nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) RemoveVolume(ctx context.Context, name string, force bool) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) PruneVolumes(ctx context.Context) error {
	return ErrLibpodNotAvailable
}

// Network operations - all return ErrLibpodNotAvailable

func (r *LibpodRuntime) ListNetworks(ctx context.Context) ([]NetworkSummary, error) {
	return nil, ErrLibpodNotAvailable
}

func (r *LibpodRuntime) RemoveNetwork(ctx context.Context, name string) error {
	return ErrLibpodNotAvailable
}

func (r *LibpodRuntime) PruneNetworks(ctx context.Context) error {
	return ErrLibpodNotAvailable
}

// Events returns an error channel on non-Linux platforms.
func (r *LibpodRuntime) Events(ctx context.Context) (<-chan Event, <-chan error) {
	errChan := make(chan error, 1)
	errChan <- ErrLibpodNotAvailable
	close(errChan)
	return nil, errChan
}
