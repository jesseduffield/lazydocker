//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/pkg/domain/entities"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/resize"
	"go.podman.io/storage/pkg/archive"
	"golang.org/x/sys/unix"
)

// Init creates a container in the OCI runtime, moving a container from
// ContainerStateConfigured, ContainerStateStopped, or ContainerStateExited to
// ContainerStateCreated. Once in Created state, Conmon will be running, which
// allows the container to be attached to. The container can subsequently
// transition to ContainerStateRunning via Start(), or be transitioned back to
// ContainerStateConfigured by Cleanup() (which will stop conmon and unmount the
// container).
// Init requires that all dependency containers be started (e.g. pod infra
// containers). The `recursive` parameter will, if set to true, start these
// dependency containers before initializing this container.
func (c *Container) Init(ctx context.Context, recursive bool) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}
	return c.initUnlocked(ctx, recursive)
}

func (c *Container) initUnlocked(ctx context.Context, recursive bool) error {
	if !c.ensureState(define.ContainerStateConfigured, define.ContainerStateStopped, define.ContainerStateExited) {
		return fmt.Errorf("container %s has already been created in runtime: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	if !recursive {
		if err := c.checkDependenciesAndHandleError(); err != nil {
			return err
		}
	} else {
		if err := c.startDependencies(ctx); err != nil {
			return err
		}
	}

	if err := c.prepare(); err != nil {
		if err2 := c.cleanup(ctx); err2 != nil {
			logrus.Errorf("Cleaning up container %s: %v", c.ID(), err2)
		}
		return err
	}

	if c.state.State == define.ContainerStateStopped {
		// Reinitialize the container
		return c.reinit(ctx, false)
	}

	// Initialize the container for the first time
	return c.init(ctx, false)
}

// Start starts the given container.
// Start will accept container in ContainerStateConfigured,
// ContainerStateCreated, ContainerStateStopped, and ContainerStateExited, and
// transition them to ContainerStateRunning (all containers not in
// ContainerStateCreated will make an intermediate stop there via the Init API).
// Once in ContainerStateRunning, the container can be transitioned to
// ContainerStatePaused via Pause(), or to ContainerStateStopped by the process
// stopping (either due to exit, or being forced to stop by the Kill or Stop API
// calls).
// Start requires that all dependency containers (e.g. pod infra containers) are
// running before starting the container. The recursive parameter, if set, will start all
// dependencies before starting this container.
func (c *Container) Start(ctx context.Context, recursive bool) error {
	// Have to lock the pod the container is a part of.
	// This prevents running `podman start` at the same time a
	// `podman pod stop` is running, which could lead to weird races.
	// Pod locks come before container locks, so do this first.
	if c.config.Pod != "" {
		// If we get an error, the pod was probably removed.
		// So we get an expected ErrCtrRemoved instead of ErrPodRemoved,
		// just ignore this and move on to syncing the container.
		pod, _ := c.runtime.state.Pod(c.config.Pod)
		if pod != nil {
			pod.lock.Lock()
			defer pod.lock.Unlock()
		}
	}

	return c.startNoPodLock(ctx, recursive)
}

// Update updates the given container.
// Either resource limits, restart policies, or HealthCheck configuration can be updated.
// Either resources, restartPolicy or changedHealthCheckConfiguration must not be nil in the updateOptions.
// If restartRetries is not nil, restartPolicy must be set and must be "on-failure".
// Nil values of changedHealthCheckConfiguration are not updated.
func (c *Container) Update(updateOptions *entities.ContainerUpdateOptions) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if c.ensureState(define.ContainerStateRemoving) {
		return fmt.Errorf("container %s is being removed, cannot update: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	healthCheckConfig, changedHealthCheck, err := GetNewHealthCheckConfig(&HealthCheckConfig{Schema2HealthConfig: c.HealthCheckConfig()}, *updateOptions.ChangedHealthCheckConfiguration)
	if err != nil {
		return err
	}
	if changedHealthCheck {
		if err := c.updateHealthCheck(
			healthCheckConfig,
			&HealthCheckConfig{Schema2HealthConfig: c.config.HealthCheckConfig},
		); err != nil {
			return err
		}
	}

	startupHealthCheckConfig, changedStartupHealthCheck, err := GetNewHealthCheckConfig(&StartupHealthCheckConfig{StartupHealthCheck: c.Config().StartupHealthCheckConfig}, *updateOptions.ChangedHealthCheckConfiguration)
	if err != nil {
		return err
	}
	if changedStartupHealthCheck {
		if err := c.updateHealthCheck(
			startupHealthCheckConfig,
			&StartupHealthCheckConfig{StartupHealthCheck: c.config.StartupHealthCheckConfig},
		); err != nil {
			return err
		}
	}

	globalHealthCheckOptions, err := updateOptions.ChangedHealthCheckConfiguration.GetNewGlobalHealthCheck()
	if err != nil {
		return err
	}
	if err := c.updateGlobalHealthCheckConfiguration(globalHealthCheckOptions); err != nil {
		return err
	}

	defer c.newContainerEvent(events.Update)
	return c.update(updateOptions)
}

// Attach to a container.
// The last parameter "start" can be used to also start the container.
// This will then Start and Attach APIs, ensuring proper
// ordering of the two such that no output from the container is lost (e.g. the
// Attach call occurs before Start).
func (c *Container) Attach(ctx context.Context, streams *define.AttachStreams, keys string, resize <-chan resize.TerminalSize, start bool) (retChan <-chan error, finalErr error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		// defer's are executed LIFO so we are locked here
		// as long as we call this after the defer unlock()
		defer func() {
			if finalErr != nil {
				if err := saveContainerError(c, finalErr); err != nil {
					logrus.Debug(err)
				}
			}
		}()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	if c.state.State != define.ContainerStateRunning {
		if !start {
			return nil, errors.New("you can only attach to running containers")
		}

		if err := c.prepareToStart(ctx, true); err != nil {
			return nil, err
		}
	}

	if !start {
		// A bit awkward, technically passthrough never supports attach. We only pretend
		// it does as we leak the stdio fds down into the container but that of course only
		// works if we are the process that started conmon with the right fds.
		if c.LogDriver() == define.PassthroughLogging {
			return nil, fmt.Errorf("this container is using the 'passthrough' log driver, cannot attach: %w", define.ErrNoLogs)
		} else if c.LogDriver() == define.PassthroughTTYLogging {
			return nil, fmt.Errorf("this container is using the 'passthrough-tty' log driver, cannot attach: %w", define.ErrNoLogs)
		}
	}

	attachChan := make(chan error)

	// We need to ensure that we don't return until start() fired in attach.
	// Use a channel to sync
	startedChan := make(chan bool)

	// Attach to the container before starting it
	go func() {
		// Start resizing
		if c.LogDriver() != define.PassthroughLogging && c.LogDriver() != define.PassthroughTTYLogging {
			registerResizeFunc(resize, c.bundlePath())
		}

		opts := new(AttachOptions)
		opts.Streams = streams
		opts.DetachKeys = &keys
		opts.Start = start
		opts.Started = startedChan

		// attach and start the container on a different thread.  waitForHealthy must
		// be done later, as it requires to run on the same thread that holds the lock
		// for the container.
		if err := c.ociRuntime.Attach(c, opts); err != nil {
			attachChan <- err
		}
		close(attachChan)
	}()

	select {
	case err := <-attachChan:
		return nil, err
	case <-startedChan:
		c.newContainerEvent(events.Attach)
	}

	if start {
		if err := c.waitForHealthy(ctx); err != nil {
			return nil, err
		}
	}

	return attachChan, nil
}

// RestartWithTimeout restarts a running container and takes a given timeout in uint
func (c *Container) RestartWithTimeout(ctx context.Context, timeout uint) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if err := c.checkDependenciesAndHandleError(); err != nil {
		return err
	}

	return c.restartWithTimeout(ctx, timeout)
}

// Stop uses the container's stop signal (or SIGTERM if no signal was specified)
// to stop the container, and if it has not stopped after container's stop
// timeout, SIGKILL is used to attempt to forcibly stop the container
// Default stop timeout is 10 seconds, but can be overridden when the container
// is created
func (c *Container) Stop() error {
	// Stop with the container's given timeout
	return c.StopWithTimeout(c.config.StopTimeout)
}

// StopWithTimeout is a version of Stop that allows a timeout to be specified
// manually. If timeout is 0, SIGKILL will be used immediately to kill the
// container.
func (c *Container) StopWithTimeout(timeout uint) (finalErr error) {
	// Have to lock the pod the container is a part of.
	// This prevents running `podman stop` at the same time a
	// `podman pod start` is running, which could lead to weird races.
	// Pod locks come before container locks, so do this first.
	if c.config.Pod != "" {
		// If we get an error, the pod was probably removed.
		// So we get an expected ErrCtrRemoved instead of ErrPodRemoved,
		// just ignore this and move on to syncing the container.
		pod, _ := c.runtime.state.Pod(c.config.Pod)
		if pod != nil {
			pod.lock.Lock()
			defer pod.lock.Unlock()
		}
	}

	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		// defer's are executed LIFO so we are locked here
		// as long as we call this after the defer unlock()
		defer func() {
			// The podman stop command is idempotent while the internal function here is not.
			// As such we do not want to log these errors in the state because they are not
			// actually user visible errors.
			if finalErr != nil && !errors.Is(finalErr, define.ErrCtrStopped) &&
				!errors.Is(finalErr, define.ErrCtrStateInvalid) {
				if err := saveContainerError(c, finalErr); err != nil {
					logrus.Debug(err)
				}
			}
		}()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	return c.stop(timeout)
}

// Kill sends a signal to a container
func (c *Container) Kill(signal uint) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	switch c.state.State {
	case define.ContainerStateRunning, define.ContainerStateStopping, define.ContainerStatePaused:
		// Note that killing containers in "stopping" state is okay.
		// In that state, the Podman is waiting for the runtime to
		// stop the container and if that is taking too long, a user
		// may have decided to kill the container after all.
	default:
		return fmt.Errorf("can only kill running containers. %s is in state %s: %w", c.ID(), c.state.State.String(), define.ErrCtrStateInvalid)
	}

	// Hardcode all = false, we only use all when removing.
	if err := c.ociRuntime.KillContainer(c, signal, false); err != nil {
		return err
	}

	c.state.StoppedByUser = true

	c.newContainerEvent(events.Kill)

	// Make sure to wait for the container to exit in case of SIGKILL.
	if signal == uint(unix.SIGKILL) {
		return c.waitForConmonToExitAndSave()
	}

	return c.save()
}

// HTTPAttach forwards an attach session over a hijacked HTTP session.
// HTTPAttach will consume and close the included httpCon, which is expected to
// be sourced from a hijacked HTTP connection.
// The cancel channel is optional, and can be used to asynchronously cancel the
// attach session.
// The streams variable is only supported if the container was not a terminal,
// and allows specifying which of the container's standard streams will be
// forwarded to the client.
// This function returns when the attach finishes. It does not hold the lock for
// the duration of its runtime, only using it at the beginning to verify state.
// The streamLogs parameter indicates that all the container's logs until present
// will be streamed at the beginning of the attach.
// The streamAttach parameter indicates that the attach itself will be streamed
// over the socket; if this is not set, but streamLogs is, only the logs will be
// sent.
// At least one of streamAttach and streamLogs must be set.
func (c *Container) HTTPAttach(r *http.Request, w http.ResponseWriter, streams *HTTPAttachStreams, detachKeys *string, cancel <-chan bool, streamAttach, streamLogs bool, hijackDone chan<- bool) error {
	// Ensure we don't leak a goroutine if we exit before hijack completes.
	defer func() {
		close(hijackDone)
	}()

	locked := false
	if !c.batched {
		locked = true
		c.lock.Lock()
		defer func() {
			if locked {
				c.lock.Unlock()
			}
		}()
		if err := c.syncContainer(); err != nil {
			return err
		}
	}
	// For Docker compatibility, we need to re-initialize containers in these states.
	if c.ensureState(define.ContainerStateConfigured, define.ContainerStateExited, define.ContainerStateStopped) {
		if err := c.initUnlocked(r.Context(), c.config.Pod != ""); err != nil {
			return fmt.Errorf("preparing container %s for attach: %w", c.ID(), err)
		}
	} else if !c.ensureState(define.ContainerStateCreated, define.ContainerStateRunning) {
		return fmt.Errorf("can only attach to created or running containers - currently in state %s: %w", c.state.State.String(), define.ErrCtrStateInvalid)
	}

	if !streamAttach && !streamLogs {
		return fmt.Errorf("must specify at least one of stream or logs: %w", define.ErrInvalidArg)
	}

	// We are NOT holding the lock for the duration of the function.
	locked = false
	c.lock.Unlock()

	logrus.Infof("Performing HTTP Hijack attach to container %s", c.ID())

	c.newContainerEvent(events.Attach)
	return c.ociRuntime.HTTPAttach(c, r, w, streams, detachKeys, cancel, hijackDone, streamAttach, streamLogs)
}

// AttachResize resizes the container's terminal, which is displayed by Attach
// and HTTPAttach.
func (c *Container) AttachResize(newSize resize.TerminalSize) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if !c.ensureState(define.ContainerStateCreated, define.ContainerStateRunning) {
		return fmt.Errorf("can only resize created or running containers: %w", define.ErrCtrStateInvalid)
	}

	logrus.Infof("Resizing TTY of container %s", c.ID())

	return c.ociRuntime.AttachResize(c, newSize)
}

// Mount mounts a container's filesystem on the host
// The path where the container has been mounted is returned
func (c *Container) Mount() (string, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return "", err
		}
	}

	defer c.newContainerEvent(events.Mount)
	return c.mount()
}

// Unmount unmounts a container's filesystem on the host
func (c *Container) Unmount(force bool) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}
	if c.state.Mounted {
		mounted, err := c.runtime.storageService.MountedContainerImage(c.ID())
		if err != nil {
			return fmt.Errorf("can't determine how many times %s is mounted, refusing to unmount: %w", c.ID(), err)
		}
		if mounted == 1 {
			if c.ensureState(define.ContainerStateRunning, define.ContainerStatePaused) {
				return fmt.Errorf("cannot unmount storage for container %s as it is running or paused: %w", c.ID(), define.ErrCtrStateInvalid)
			}
			execSessions, err := c.getActiveExecSessions()
			if err != nil {
				return err
			}
			if len(execSessions) != 0 {
				return fmt.Errorf("container %s has active exec sessions, refusing to unmount: %w", c.ID(), define.ErrCtrStateInvalid)
			}
			return fmt.Errorf("can't unmount %s last mount, it is still in use: %w", c.ID(), define.ErrInternal)
		}
	}
	defer c.newContainerEvent(events.Unmount)
	return c.unmount(force)
}

// Pause pauses a container
func (c *Container) Pause() error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if c.state.State == define.ContainerStatePaused {
		return fmt.Errorf("%q is already paused: %w", c.ID(), define.ErrCtrStateInvalid)
	}
	if c.state.State != define.ContainerStateRunning {
		return fmt.Errorf("%q is not running, can't pause: %w", c.state.State, define.ErrCtrStateInvalid)
	}
	defer c.newContainerEvent(events.Pause)
	return c.pause()
}

// Unpause unpauses a container
func (c *Container) Unpause() error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if c.state.State != define.ContainerStatePaused {
		return fmt.Errorf("%q is not paused, can't unpause: %w", c.ID(), define.ErrCtrStateInvalid)
	}
	defer c.newContainerEvent(events.Unpause)
	return c.unpause()
}

// Export exports a container's root filesystem as a tar archive
// The archive will be saved as a file at the given path
func (c *Container) Export(out io.Writer) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if c.state.State == define.ContainerStateRemoving {
		return fmt.Errorf("cannot mount container %s as it is being removed: %w", c.ID(), define.ErrCtrStateInvalid)
	}

	defer c.newContainerEvent(events.Mount)
	return c.export(out)
}

// AddArtifact creates and writes to an artifact file for the container
func (c *Container) AddArtifact(name string, data []byte) error {
	if !c.valid {
		return define.ErrCtrRemoved
	}

	return os.WriteFile(c.getArtifactPath(name), data, 0o740)
}

// GetArtifact reads the specified artifact file from the container
func (c *Container) GetArtifact(name string) ([]byte, error) {
	if !c.valid {
		return nil, define.ErrCtrRemoved
	}

	return os.ReadFile(c.getArtifactPath(name))
}

// RemoveArtifact deletes the specified artifacts file
func (c *Container) RemoveArtifact(name string) error {
	if !c.valid {
		return define.ErrCtrRemoved
	}

	return os.Remove(c.getArtifactPath(name))
}

// Wait blocks until the container exits and returns its exit code.
func (c *Container) Wait(ctx context.Context) (int32, error) {
	return c.WaitForExit(ctx, DefaultWaitInterval)
}

// WaitForExit blocks until the container exits and returns its exit code. The
// argument is the interval at which checks the container's status.
// If the argument is less than or equal to 0 Nanoseconds a default interval is
// used.
func (c *Container) WaitForExit(ctx context.Context, pollInterval time.Duration) (int32, error) {
	id := c.ID()
	if !c.valid {
		// if the container is not valid at this point as it was deleted,
		// check if the exit code was recorded in the db.
		exitCode, err := c.runtime.state.GetContainerExitCode(id)
		if err == nil {
			return exitCode, nil
		}
		return -1, define.ErrCtrRemoved
	}

	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
				// if the container is not valid at this point as it was deleted,
				// check if the exit code was recorded in the db.
				exitCode, err := c.runtime.state.GetContainerExitCode(id)
				if err == nil {
					return exitCode, nil
				}
			}
			return -1, err
		}
	}

	conmonPID := c.state.ConmonPID
	// conmonPID == 0 means container is not running
	if conmonPID == 0 {
		exitCode, err := c.runtime.state.GetContainerExitCode(id)
		if err != nil {
			if errors.Is(err, define.ErrNoSuchExitCode) {
				// If the container is configured or created, we must assume it never ran.
				if c.ensureState(define.ContainerStateConfigured, define.ContainerStateCreated) {
					return 0, nil
				}
			}
			return -1, err
		}
		return exitCode, nil
	}

	conmonPidFd := c.getConmonPidFd()
	if conmonPidFd > -1 {
		defer unix.Close(conmonPidFd)
	}

	if pollInterval <= 0 {
		pollInterval = DefaultWaitInterval
	}

	// we cannot wait locked as we would hold the lock forever, so we unlock and then lock again
	c.lock.Unlock()
	err := waitForConmonExit(ctx, conmonPID, conmonPidFd, pollInterval)
	c.lock.Lock()
	if err != nil {
		return -1, fmt.Errorf("failed to wait for conmon to exit: %w", err)
	}

	// we locked again so we must sync the state
	if err := c.syncContainer(); err != nil {
		if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
			// if the container is not valid at this point as it was deleted,
			// check if the exit code was recorded in the db.
			exitCode, err := c.runtime.state.GetContainerExitCode(id)
			if err == nil {
				return exitCode, nil
			}
		}
		return -1, err
	}

	// syncContainer already did the exit file read so nothing extra for us to do here
	return c.runtime.state.GetContainerExitCode(id)
}

func waitForConmonExit(ctx context.Context, conmonPID, conmonPidFd int, pollInterval time.Duration) error {
	if conmonPidFd > -1 {
		for {
			fds := []unix.PollFd{{Fd: int32(conmonPidFd), Events: unix.POLLIN}}
			if n, err := unix.Poll(fds, int(pollInterval.Milliseconds())); err != nil {
				if err == unix.EINTR {
					continue
				}
				return err
			} else if n == 0 {
				// n == 0 means timeout
				select {
				case <-ctx.Done():
					return define.ErrCanceled
				default:
					// context not done, wait again
					continue
				}
			}
			return nil
		}
	}
	// no pidfd support, we must poll the pid
	for {
		if err := unix.Kill(conmonPID, 0); err != nil {
			if err == unix.ESRCH {
				break
			}
			return err
		}
		select {
		case <-ctx.Done():
			return define.ErrCanceled
		case <-time.After(pollInterval):
		}
	}
	return nil
}

type waitResult struct {
	code int32
	err  error
}

func (c *Container) WaitForConditionWithInterval(ctx context.Context, waitTimeout time.Duration, conditions ...string) (int32, error) {
	if !c.valid {
		return -1, define.ErrCtrRemoved
	}

	if len(conditions) == 0 {
		panic("at least one condition should be passed")
	}

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	resultChan := make(chan waitResult)
	waitForExit := false
	wantedStates := make(map[define.ContainerStatus]bool, len(conditions))
	wantedHealthStates := make(map[string]bool)

	for _, rawCondition := range conditions {
		switch rawCondition {
		case define.HealthCheckHealthy, define.HealthCheckUnhealthy:
			if !c.HasHealthCheck() {
				return -1, fmt.Errorf("cannot use condition %q: container %s has no healthcheck", rawCondition, c.ID())
			}
			wantedHealthStates[rawCondition] = true
		default:
			condition, err := define.StringToContainerStatus(rawCondition)
			if err != nil {
				return -1, err
			}
			switch condition {
			case define.ContainerStateExited, define.ContainerStateStopped:
				waitForExit = true
			default:
				wantedStates[condition] = true
			}
		}
	}

	trySend := func(code int32, err error) {
		select {
		case resultChan <- waitResult{code, err}:
		case <-ctx.Done():
		}
	}

	if waitForExit {
		go func() {
			code, err := c.WaitForExit(ctx, waitTimeout)
			trySend(code, err)
		}()
	}

	if len(wantedStates) > 0 || len(wantedHealthStates) > 0 {
		go func() {
			stoppedCount := 0
			for {
				if len(wantedStates) > 0 {
					state, err := c.State()
					if err != nil {
						// If the we wait for removing and the container is removed do not return this as error.
						// This allows callers to actually wait for the ctr to be removed.
						if wantedStates[define.ContainerStateRemoving] &&
							(errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved)) {
							// check if the exit code was recorded in the db to return it
							exitCode, err := c.runtime.state.GetContainerExitCode(c.ID())
							if err == nil {
								trySend(exitCode, nil)
								return
							}

							trySend(-1, nil)
							return
						}
						trySend(-1, err)
						return
					}
					if _, found := wantedStates[state]; found {
						trySend(-1, nil)
						return
					}
				}
				if len(wantedHealthStates) > 0 {
					// even if we are interested only in the health check
					// check that the container is still running to avoid
					// waiting until the timeout expires.
					if stoppedCount > 0 {
						stoppedCount++
					} else {
						state, err := c.State()
						if err != nil {
							trySend(-1, err)
							return
						}
						if state != define.ContainerStateCreated && state != define.ContainerStateRunning && state != define.ContainerStatePaused {
							stoppedCount++
						}
					}
					status, err := c.HealthCheckStatus()
					if err != nil {
						trySend(-1, err)
						return
					}
					if _, found := wantedHealthStates[status]; found {
						trySend(-1, nil)
						return
					}
					// wait for another waitTimeout interval to give the health check process some time
					// to record the healthy status.
					if stoppedCount > 1 {
						trySend(-1, define.ErrCtrStopped)
						return
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(waitTimeout):
					continue
				}
			}
		}()
	}

	var result waitResult
	select {
	case result = <-resultChan:
		cancelFn()
	case <-ctx.Done():
		result = waitResult{-1, define.ErrCanceled}
	}
	return result.code, result.err
}

// Cleanup unmounts all mount points in container and cleans up container storage
// It also cleans up the network stack.
// onlyStopped is set by the podman container cleanup to ensure we only cleanup a stopped container,
// all other states mean another process already called cleanup before us which is fine in such cases.
func (c *Container) Cleanup(ctx context.Context, onlyStopped bool) error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			// When the container has already been removed, the OCI runtime directory remains.
			if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
				if err := c.cleanupRuntime(ctx); err != nil {
					return fmt.Errorf("cleaning up container %s from OCI runtime: %w", c.ID(), err)
				}
				return nil
			}
			logrus.Errorf("Syncing container %s status: %v", c.ID(), err)
			return err
		}
	}

	return c.fullCleanup(ctx, onlyStopped)
}

// Batch starts a batch operation on the given container
// All commands in the passed function will execute under the same lock and
// without synchronizing state after each operation
// This will result in substantial performance benefits when running numerous
// commands on the same container
// Note that the container passed into the Batch function cannot be removed
// during batched operations. runtime.RemoveContainer can only be called outside
// of Batch
// Any error returned by the given batch function will be returned unmodified by
// Batch
// As Batch normally disables updating the current state of the container, the
// Sync() function is provided to enable container state to be updated and
// checked within Batch.
func (c *Container) Batch(batchFunc func(*Container) error) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	newCtr := new(Container)
	newCtr.config = c.config
	newCtr.state = c.state
	newCtr.runtime = c.runtime
	newCtr.ociRuntime = c.ociRuntime
	newCtr.lock = c.lock
	newCtr.valid = true

	newCtr.batched = true
	err := batchFunc(newCtr)
	newCtr.batched = false

	return err
}

// Sync updates the status of a container by querying the OCI runtime.
// If the container has not been created inside the OCI runtime, nothing will be
// done.
// Most of the time, Podman does not explicitly query the OCI runtime for
// container status, and instead relies upon exit files created by conmon.
// This can cause a disconnect between running state and what Podman sees in
// cases where Conmon was killed unexpectedly, or runc was upgraded.
// Running a manual Sync() ensures that container state will be correct in
// such situations.
func (c *Container) Sync() error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
	}

	if err := c.syncContainer(); err != nil {
		return err
	}

	defer c.newContainerEvent(events.Sync)
	return nil
}

// ReloadNetwork reconfigures the container's network.
// Technically speaking, it will tear down and then reconfigure the container's
// network namespace, which will result in all firewall rules being recreated.
// It is mostly intended to be used in cases where the system firewall has been
// reloaded, and existing rules have been wiped out. It is expected that some
// downtime will result, as the rules are destroyed as part of this process.
// At present, this only works on root containers; it may be expanded to restart
// slirp4netns in the future to work with rootless containers as well.
// Requires that the container must be running or created.
func (c *Container) ReloadNetwork() error {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return err
		}
	}

	if !c.ensureState(define.ContainerStateCreated, define.ContainerStateRunning) {
		return fmt.Errorf("cannot reload network unless container network has been configured: %w", define.ErrCtrStateInvalid)
	}

	return c.reloadNetwork()
}

// Refresh is DEPRECATED and REMOVED.
func (c *Container) Refresh(_ context.Context) error {
	// This has been deprecated for a long while, and is in the process of
	// being removed.
	return define.ErrNotImplemented
}

// ContainerCheckpointOptions is a struct used to pass the parameters
// for checkpointing (and restoring) to the corresponding functions
type ContainerCheckpointOptions struct {
	// Keep tells the API to not delete checkpoint artifacts
	Keep bool
	// KeepRunning tells the API to keep the container running
	// after writing the checkpoint to disk
	KeepRunning bool
	// TCPEstablished tells the API to checkpoint a container
	// even if it contains established TCP connections
	TCPEstablished bool
	// TCPClose tells the API to close all TCP connections during restore
	TCPClose bool
	// TargetFile tells the API to read (or write) the checkpoint image
	// from (or to) the filename set in TargetFile
	TargetFile string
	// CheckpointImageID tells the API to restore the container from
	// checkpoint image with ID set in CheckpointImageID
	CheckpointImageID string
	// Name tells the API that during restore from an exported
	// checkpoint archive a new name should be used for the
	// restored container
	Name string
	// IgnoreRootfs tells the API to not export changes to
	// the container's root file-system (or to not import)
	IgnoreRootfs bool
	// IgnoreStaticIP tells the API to ignore the IP set
	// during 'podman run' with '--ip'. This is especially
	// important to be able to restore a container multiple
	// times with '--import --name'.
	IgnoreStaticIP bool
	// IgnoreStaticMAC tells the API to ignore the MAC set
	// during 'podman run' with '--mac-address'. This is especially
	// important to be able to restore a container multiple
	// times with '--import --name'.
	IgnoreStaticMAC bool
	// IgnoreVolumes tells the API to not export or not to import
	// the content of volumes associated with the container
	IgnoreVolumes bool
	// Pre Checkpoint container and leave container running
	PreCheckPoint bool
	// Dump container with Pre Checkpoint images
	WithPrevious bool
	// ImportPrevious tells the API to restore container with two
	// images. One is TargetFile, the other is ImportPrevious.
	ImportPrevious string
	// CreateImage tells Podman to create an OCI image from container
	// checkpoint in the local image store.
	CreateImage string
	// Compression tells the API which compression to use for
	// the exported checkpoint archive.
	Compression archive.Compression
	// If Pod is set the container should be restored into the
	// given Pod. If Pod is empty it is a restore without a Pod.
	// Restoring a non Pod container into a Pod or a Pod container
	// without a Pod is theoretically possible, but will
	// probably not work if a PID namespace is shared.
	// A shared PID namespace means that a Pod container has PID 1
	// in the infrastructure container, but without the infrastructure
	// container no PID 1 will be in the namespace and that is not
	// possible.
	Pod string
	// PrintStats tells the API to fill out the statistics about
	// how much time each component in the stack requires to
	// checkpoint a container.
	PrintStats bool
	// FileLocks tells the API to checkpoint/restore a container
	// with file-locks
	FileLocks bool
}

// Checkpoint checkpoints a container
// The return values *define.CRIUCheckpointRestoreStatistics and int64 (time
// the runtime needs to checkpoint the container) are only set if
// options.PrintStats is set to true. Not setting options.PrintStats to true
// will return nil and 0.
func (c *Container) Checkpoint(ctx context.Context, options ContainerCheckpointOptions) (*define.CRIUCheckpointRestoreStatistics, int64, error) {
	logrus.Debugf("Trying to checkpoint container %s", c.ID())

	if options.TargetFile != "" {
		if err := c.prepareCheckpointExport(); err != nil {
			return nil, 0, err
		}
	}

	if options.WithPrevious {
		if err := c.canWithPrevious(); err != nil {
			return nil, 0, err
		}
	}

	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, 0, err
		}
	}
	return c.checkpoint(ctx, options)
}

// Restore restores a container
// The return values *define.CRIUCheckpointRestoreStatistics and int64 (time
// the runtime needs to restore the container) are only set if
// options.PrintStats is set to true. Not setting options.PrintStats to true
// will return nil and 0.
func (c *Container) Restore(ctx context.Context, options ContainerCheckpointOptions) (*define.CRIUCheckpointRestoreStatistics, int64, error) {
	if options.Pod == "" {
		logrus.Debugf("Trying to restore container %s", c.ID())
	} else {
		logrus.Debugf("Trying to restore container %s into pod %s", c.ID(), options.Pod)
	}
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, 0, err
		}
	}
	defer c.newContainerEvent(events.Restore)
	return c.restore(ctx, options)
}

// Indicate whether or not the container should restart
func (c *Container) ShouldRestart(_ context.Context) bool {
	logrus.Debugf("Checking if container %s should restart", c.ID())
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return false
		}
	}
	return c.shouldRestart()
}

// CopyFromArchive copies the contents from the specified tarStream to path
// *inside* the container.
func (c *Container) CopyFromArchive(_ context.Context, containerPath string, chown, noOverwriteDirNonDir bool, rename map[string]string, tarStream io.Reader) (func() error, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	return c.copyFromArchive(containerPath, chown, noOverwriteDirNonDir, rename, tarStream)
}

// CopyToArchive copies the contents from the specified path *inside* the
// container to the tarStream.
func (c *Container) CopyToArchive(_ context.Context, containerPath string, tarStream io.Writer) (func() error, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	return c.copyToArchive(containerPath, tarStream)
}

// Stat the specified path *inside* the container and return a file info.
func (c *Container) Stat(_ context.Context, containerPath string) (*define.FileInfo, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	var mountPoint string
	var err error
	if c.state.Mounted {
		mountPoint = c.state.Mountpoint
	} else {
		mountPoint, err = c.mount()
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := c.unmount(false); err != nil {
				logrus.Errorf("Unmounting container %s: %v", c.ID(), err)
			}
		}()
	}

	info, _, _, err := c.stat(mountPoint, containerPath)
	return info, err
}

func saveContainerError(c *Container, err error) error {
	c.state.Error = err.Error()
	return c.save()
}
