//go:build !remote

package libpod

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/containers/podman/v5/libpod/define"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/resize"
)

var (
	// Only create each missing runtime once.
	// Creation makes error messages we don't want to duplicate.
	missingRuntimes map[string]*MissingRuntime
	// We need a lock for this
	missingRuntimesLock sync.Mutex
)

// MissingRuntime is used when the OCI runtime requested by the container is
// missing (not installed or not in the configuration file).
type MissingRuntime struct {
	// Name is the name of the missing runtime. Will be used in errors.
	name string
	// exitsDir is the directory for exit files.
	exitsDir string
	// persistDir is the directory for exit and oom files.
	persistDir string
}

// Get a new MissingRuntime for the given name.
// Requires a libpod Runtime so we can make a sane path for the exits dir.
func getMissingRuntime(name string, r *Runtime) OCIRuntime {
	missingRuntimesLock.Lock()
	defer missingRuntimesLock.Unlock()

	if missingRuntimes == nil {
		missingRuntimes = make(map[string]*MissingRuntime)
	}

	runtime, ok := missingRuntimes[name]
	if ok {
		return runtime
	}

	// Once for each missing runtime, we want to error.
	logrus.Errorf("OCI Runtime %s is in use by a container, but is not available (not in configuration file or not installed)", name)

	newRuntime := new(MissingRuntime)
	newRuntime.name = name
	newRuntime.exitsDir = filepath.Join(r.config.Engine.TmpDir, "exits")
	newRuntime.persistDir = filepath.Join(r.config.Engine.TmpDir, "persist")

	missingRuntimes[name] = newRuntime

	return newRuntime
}

// Name is the name of the missing runtime
func (r *MissingRuntime) Name() string {
	return fmt.Sprintf("%s (missing/not available)", r.name)
}

// Path is not available as the runtime is missing
func (r *MissingRuntime) Path() string {
	return "(missing/not available)"
}

// CreateContainer is not available as the runtime is missing
func (r *MissingRuntime) CreateContainer(_ *Container, _ *ContainerCheckpointOptions) (int64, error) {
	return 0, r.printError()
}

// StartContainer is not available as the runtime is missing
func (r *MissingRuntime) StartContainer(_ *Container) error {
	return r.printError()
}

// UpdateContainer is not available as the runtime is missing
func (r *MissingRuntime) UpdateContainer(_ *Container, _ *spec.LinuxResources) error {
	return r.printError()
}

// KillContainer is not available as the runtime is missing
// TODO: We could attempt to unix.Kill() the PID as recorded in the state if we
// really want to smooth things out? Won't be perfect, but if the container has
// a PID namespace it could be enough?
func (r *MissingRuntime) KillContainer(_ *Container, _ uint, _ bool) error {
	return r.printError()
}

// StopContainer is not available as the runtime is missing
func (r *MissingRuntime) StopContainer(_ *Container, _ uint, _ bool) error {
	return r.printError()
}

// DeleteContainer is not available as the runtime is missing
func (r *MissingRuntime) DeleteContainer(_ *Container) error {
	return r.printError()
}

// PauseContainer is not available as the runtime is missing
func (r *MissingRuntime) PauseContainer(_ *Container) error {
	return r.printError()
}

// UnpauseContainer is not available as the runtime is missing
func (r *MissingRuntime) UnpauseContainer(_ *Container) error {
	return r.printError()
}

// Attach is not available as the runtime is missing
func (r *MissingRuntime) Attach(_ *Container, _ *AttachOptions) error {
	return r.printError()
}

// HTTPAttach is not available as the runtime is missing
func (r *MissingRuntime) HTTPAttach(_ *Container, _ *http.Request, _ http.ResponseWriter, _ *HTTPAttachStreams, _ *string, _ <-chan bool, _ chan<- bool, _, _ bool) error {
	return r.printError()
}

// AttachResize is not available as the runtime is missing
func (r *MissingRuntime) AttachResize(_ *Container, _ resize.TerminalSize) error {
	return r.printError()
}

// ExecContainer is not available as the runtime is missing
func (r *MissingRuntime) ExecContainer(_ *Container, _ string, _ *ExecOptions, _ *define.AttachStreams, _ *resize.TerminalSize) (int, chan error, error) {
	return -1, nil, r.printError()
}

// ExecContainerHTTP is not available as the runtime is missing
func (r *MissingRuntime) ExecContainerHTTP(_ *Container, _ string, _ *ExecOptions, _ *http.Request, _ http.ResponseWriter,
	_ *HTTPAttachStreams, _ <-chan bool, _ chan<- bool, _ <-chan bool, _ *resize.TerminalSize) (int, chan error, error) {
	return -1, nil, r.printError()
}

// ExecContainerDetached is not available as the runtime is missing
func (r *MissingRuntime) ExecContainerDetached(_ *Container, _ string, _ *ExecOptions, _ bool) (int, error) {
	return -1, r.printError()
}

// ExecAttachResize is not available as the runtime is missing.
func (r *MissingRuntime) ExecAttachResize(_ *Container, _ string, _ resize.TerminalSize) error {
	return r.printError()
}

// ExecStopContainer is not available as the runtime is missing.
// TODO: We can also investigate using unix.Kill() on the PID of the exec
// session here if we want to make stopping containers possible. Won't be
// perfect, though.
func (r *MissingRuntime) ExecStopContainer(_ *Container, _ string, _ uint) error {
	return r.printError()
}

// ExecUpdateStatus is not available as the runtime is missing.
func (r *MissingRuntime) ExecUpdateStatus(_ *Container, _ string) (bool, error) {
	return false, r.printError()
}

// CheckpointContainer is not available as the runtime is missing
func (r *MissingRuntime) CheckpointContainer(_ *Container, _ ContainerCheckpointOptions) (int64, error) {
	return 0, r.printError()
}

// CheckConmonRunning is not available as the runtime is missing
func (r *MissingRuntime) CheckConmonRunning(_ *Container) (bool, error) {
	return false, r.printError()
}

// SupportsCheckpoint returns false as checkpointing requires a working runtime
func (r *MissingRuntime) SupportsCheckpoint() bool {
	return false
}

// SupportsJSONErrors returns false as there is no runtime to give errors
func (r *MissingRuntime) SupportsJSONErrors() bool {
	return false
}

// SupportsNoCgroups returns false as there is no runtime to create containers
func (r *MissingRuntime) SupportsNoCgroups() bool {
	return false
}

// SupportsKVM checks if the OCI runtime supports running containers
// without KVM separation
func (r *MissingRuntime) SupportsKVM() bool {
	return false
}

// AttachSocketPath does not work as there is no runtime to attach to.
// (Theoretically we could follow ExitFilePath but there is no guarantee the
// container is running and thus has an attach socket...)
func (r *MissingRuntime) AttachSocketPath(_ *Container) (string, error) {
	return "", r.printError()
}

// ExecAttachSocketPath does not work as there is no runtime to attach to.
// (Again, we could follow ExitFilePath, but no guarantee there is an existing
// and running exec session)
func (r *MissingRuntime) ExecAttachSocketPath(_ *Container, _ string) (string, error) {
	return "", r.printError()
}

// ExitFilePath returns the exit file path for containers.
// Here, we mimic what ConmonOCIRuntime does, because there is a chance that the
// container in question is still running happily (config file modified to
// remove a runtime, for example). We can't find the runtime to do anything to
// the container, but Conmon should still place an exit file for it.
func (r *MissingRuntime) ExitFilePath(ctr *Container) (string, error) {
	if ctr == nil {
		return "", fmt.Errorf("must provide a valid container to get exit file path: %w", define.ErrInvalidArg)
	}
	return filepath.Join(r.exitsDir, ctr.ID()), nil
}

// OOMFilePath returns the oom file path for a container.
// The oom file will only exist if the container was oom killed.
func (r *MissingRuntime) OOMFilePath(ctr *Container) (string, error) {
	return filepath.Join(r.persistDir, ctr.ID(), "oom"), nil
}

// PersistDirectoryPath is the path to the container's persist directory.
// It may include files like the exit file and OOM file.
func (r *MissingRuntime) PersistDirectoryPath(ctr *Container) (string, error) {
	return filepath.Join(r.persistDir, ctr.ID()), nil
}

// RuntimeInfo returns information on the missing runtime
func (r *MissingRuntime) RuntimeInfo() (*define.ConmonInfo, *define.OCIRuntimeInfo, error) {
	ocirt := define.OCIRuntimeInfo{
		Name:    r.name,
		Path:    "missing",
		Package: "missing",
		Version: "missing",
	}
	return nil, &ocirt, nil
}

// Return an error indicating the runtime is missing
func (r *MissingRuntime) printError() error {
	return fmt.Errorf("runtime %s is missing: %w", r.name, define.ErrOCIRuntimeNotFound)
}
