package commands

import (
	"context"
	"errors"
)

// MockRuntime implements ContainerRuntime for testing purposes.
// Each method can be customized by setting the corresponding function field.
// If a function is not set, the method returns sensible defaults or errors.
type MockRuntime struct {
	// Container operation mocks
	ListContainersFunc    func(ctx context.Context) ([]ContainerSummary, error)
	InspectContainerFunc  func(ctx context.Context, id string) (*ContainerDetails, error)
	StartContainerFunc    func(ctx context.Context, id string) error
	StopContainerFunc     func(ctx context.Context, id string, timeout *int) error
	PauseContainerFunc    func(ctx context.Context, id string) error
	UnpauseContainerFunc  func(ctx context.Context, id string) error
	RestartContainerFunc  func(ctx context.Context, id string, timeout *int) error
	RemoveContainerFunc   func(ctx context.Context, id string, force bool, volumes bool) error
	ContainerTopFunc      func(ctx context.Context, id string) ([]string, [][]string, error)
	PruneContainersFunc   func(ctx context.Context) error
	ContainerStatsFunc    func(ctx context.Context, id string, stream bool) (<-chan ContainerStatsEntry, <-chan error)

	// Image operation mocks
	ListImagesFunc    func(ctx context.Context) ([]ImageSummary, error)
	InspectImageFunc  func(ctx context.Context, id string) (*ImageDetails, error)
	ImageHistoryFunc  func(ctx context.Context, id string) ([]ImageHistoryEntry, error)
	RemoveImageFunc   func(ctx context.Context, id string, force bool) error
	PruneImagesFunc   func(ctx context.Context) error

	// Volume operation mocks
	ListVolumesFunc   func(ctx context.Context) ([]VolumeSummary, error)
	RemoveVolumeFunc  func(ctx context.Context, name string, force bool) error
	PruneVolumesFunc  func(ctx context.Context) error

	// Network operation mocks
	ListNetworksFunc   func(ctx context.Context) ([]NetworkSummary, error)
	RemoveNetworkFunc  func(ctx context.Context, name string) error
	PruneNetworksFunc  func(ctx context.Context) error

	// Event mock
	EventsFunc func(ctx context.Context) (<-chan Event, <-chan error)

	// Lifecycle mocks
	CloseFunc func() error
	ModeFunc  func() string

	// Track method calls for assertions
	Calls []MockCall
}

// MockCall records a method invocation for verification in tests.
type MockCall struct {
	Method string
	Args   []interface{}
}

// ErrMockNotImplemented is returned when a mock function is not set.
var ErrMockNotImplemented = errors.New("mock function not implemented")

// recordCall records a method call for later verification.
func (m *MockRuntime) recordCall(method string, args ...interface{}) {
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

// Container operations

func (m *MockRuntime) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	m.recordCall("ListContainers")
	if m.ListContainersFunc != nil {
		return m.ListContainersFunc(ctx)
	}
	return nil, ErrMockNotImplemented
}

func (m *MockRuntime) InspectContainer(ctx context.Context, id string) (*ContainerDetails, error) {
	m.recordCall("InspectContainer", id)
	if m.InspectContainerFunc != nil {
		return m.InspectContainerFunc(ctx, id)
	}
	return nil, ErrMockNotImplemented
}

func (m *MockRuntime) StartContainer(ctx context.Context, id string) error {
	m.recordCall("StartContainer", id)
	if m.StartContainerFunc != nil {
		return m.StartContainerFunc(ctx, id)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) StopContainer(ctx context.Context, id string, timeout *int) error {
	m.recordCall("StopContainer", id, timeout)
	if m.StopContainerFunc != nil {
		return m.StopContainerFunc(ctx, id, timeout)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) PauseContainer(ctx context.Context, id string) error {
	m.recordCall("PauseContainer", id)
	if m.PauseContainerFunc != nil {
		return m.PauseContainerFunc(ctx, id)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) UnpauseContainer(ctx context.Context, id string) error {
	m.recordCall("UnpauseContainer", id)
	if m.UnpauseContainerFunc != nil {
		return m.UnpauseContainerFunc(ctx, id)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) RestartContainer(ctx context.Context, id string, timeout *int) error {
	m.recordCall("RestartContainer", id, timeout)
	if m.RestartContainerFunc != nil {
		return m.RestartContainerFunc(ctx, id, timeout)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) RemoveContainer(ctx context.Context, id string, force bool, volumes bool) error {
	m.recordCall("RemoveContainer", id, force, volumes)
	if m.RemoveContainerFunc != nil {
		return m.RemoveContainerFunc(ctx, id, force, volumes)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) ContainerTop(ctx context.Context, id string) ([]string, [][]string, error) {
	m.recordCall("ContainerTop", id)
	if m.ContainerTopFunc != nil {
		return m.ContainerTopFunc(ctx, id)
	}
	return nil, nil, ErrMockNotImplemented
}

func (m *MockRuntime) PruneContainers(ctx context.Context) error {
	m.recordCall("PruneContainers")
	if m.PruneContainersFunc != nil {
		return m.PruneContainersFunc(ctx)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) ContainerStats(ctx context.Context, id string, stream bool) (<-chan ContainerStatsEntry, <-chan error) {
	m.recordCall("ContainerStats", id, stream)
	if m.ContainerStatsFunc != nil {
		return m.ContainerStatsFunc(ctx, id, stream)
	}
	errCh := make(chan error, 1)
	errCh <- ErrMockNotImplemented
	close(errCh)
	return nil, errCh
}

// Image operations

func (m *MockRuntime) ListImages(ctx context.Context) ([]ImageSummary, error) {
	m.recordCall("ListImages")
	if m.ListImagesFunc != nil {
		return m.ListImagesFunc(ctx)
	}
	return nil, ErrMockNotImplemented
}

func (m *MockRuntime) InspectImage(ctx context.Context, id string) (*ImageDetails, error) {
	m.recordCall("InspectImage", id)
	if m.InspectImageFunc != nil {
		return m.InspectImageFunc(ctx, id)
	}
	return nil, ErrMockNotImplemented
}

func (m *MockRuntime) ImageHistory(ctx context.Context, id string) ([]ImageHistoryEntry, error) {
	m.recordCall("ImageHistory", id)
	if m.ImageHistoryFunc != nil {
		return m.ImageHistoryFunc(ctx, id)
	}
	return nil, ErrMockNotImplemented
}

func (m *MockRuntime) RemoveImage(ctx context.Context, id string, force bool) error {
	m.recordCall("RemoveImage", id, force)
	if m.RemoveImageFunc != nil {
		return m.RemoveImageFunc(ctx, id, force)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) PruneImages(ctx context.Context) error {
	m.recordCall("PruneImages")
	if m.PruneImagesFunc != nil {
		return m.PruneImagesFunc(ctx)
	}
	return ErrMockNotImplemented
}

// Volume operations

func (m *MockRuntime) ListVolumes(ctx context.Context) ([]VolumeSummary, error) {
	m.recordCall("ListVolumes")
	if m.ListVolumesFunc != nil {
		return m.ListVolumesFunc(ctx)
	}
	return nil, ErrMockNotImplemented
}

func (m *MockRuntime) RemoveVolume(ctx context.Context, name string, force bool) error {
	m.recordCall("RemoveVolume", name, force)
	if m.RemoveVolumeFunc != nil {
		return m.RemoveVolumeFunc(ctx, name, force)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) PruneVolumes(ctx context.Context) error {
	m.recordCall("PruneVolumes")
	if m.PruneVolumesFunc != nil {
		return m.PruneVolumesFunc(ctx)
	}
	return ErrMockNotImplemented
}

// Network operations

func (m *MockRuntime) ListNetworks(ctx context.Context) ([]NetworkSummary, error) {
	m.recordCall("ListNetworks")
	if m.ListNetworksFunc != nil {
		return m.ListNetworksFunc(ctx)
	}
	return nil, ErrMockNotImplemented
}

func (m *MockRuntime) RemoveNetwork(ctx context.Context, name string) error {
	m.recordCall("RemoveNetwork", name)
	if m.RemoveNetworkFunc != nil {
		return m.RemoveNetworkFunc(ctx, name)
	}
	return ErrMockNotImplemented
}

func (m *MockRuntime) PruneNetworks(ctx context.Context) error {
	m.recordCall("PruneNetworks")
	if m.PruneNetworksFunc != nil {
		return m.PruneNetworksFunc(ctx)
	}
	return ErrMockNotImplemented
}

// Events

func (m *MockRuntime) Events(ctx context.Context) (<-chan Event, <-chan error) {
	m.recordCall("Events")
	if m.EventsFunc != nil {
		return m.EventsFunc(ctx)
	}
	errCh := make(chan error, 1)
	errCh <- ErrMockNotImplemented
	close(errCh)
	return nil, errCh
}

// Lifecycle

func (m *MockRuntime) Close() error {
	m.recordCall("Close")
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockRuntime) Mode() string {
	m.recordCall("Mode")
	if m.ModeFunc != nil {
		return m.ModeFunc()
	}
	return "mock"
}

// Helper methods for test assertions

// CallCount returns the number of times a method was called.
func (m *MockRuntime) CallCount(method string) int {
	count := 0
	for _, call := range m.Calls {
		if call.Method == method {
			count++
		}
	}
	return count
}

// WasCalled returns true if the method was called at least once.
func (m *MockRuntime) WasCalled(method string) bool {
	return m.CallCount(method) > 0
}

// Reset clears all recorded calls.
func (m *MockRuntime) Reset() {
	m.Calls = nil
}

// Verify that MockRuntime implements ContainerRuntime at compile time.
var _ ContainerRuntime = (*MockRuntime)(nil)
