//go:build !remote

package libpod

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/shutdown"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

// HealthCheck verifies the state and validity of the healthcheck configuration
// on the container and then executes the healthcheck
func (r *Runtime) HealthCheck(ctx context.Context, name string) (define.HealthCheckStatus, error) {
	container, err := r.LookupContainer(name)
	if err != nil {
		return define.HealthCheckContainerNotFound, fmt.Errorf("unable to look up %s to perform a health check: %w", name, err)
	}

	hcStatus, err := checkHealthCheckCanBeRun(container)
	if err != nil {
		return hcStatus, err
	}

	isStartupHC := false
	if container.config.StartupHealthCheckConfig != nil {
		passed, err := container.StartupHCPassed()
		if err != nil {
			return define.HealthCheckInternalError, err
		}
		isStartupHC = !passed
	}

	hcStatus, logStatus, err := container.runHealthCheck(ctx, isStartupHC)
	if !isStartupHC {
		if err := container.processHealthCheckStatus(logStatus); err != nil {
			return hcStatus, err
		}
	}
	return hcStatus, err
}

func (c *Container) runHealthCheck(ctx context.Context, isStartup bool) (define.HealthCheckStatus, string, error) {
	var (
		newCommand    []string
		returnCode    int
		inStartPeriod bool
	)

	hcCommand := c.HealthCheckConfig().Test
	if isStartup {
		logrus.Debugf("Running startup healthcheck for container %s", c.ID())
		hcCommand = c.config.StartupHealthCheckConfig.Test
	}
	if len(hcCommand) < 1 {
		return define.HealthCheckNotDefined, "", fmt.Errorf("container %s has no defined healthcheck", c.ID())
	}
	switch hcCommand[0] {
	case "", define.HealthConfigTestNone:
		return define.HealthCheckNotDefined, "", fmt.Errorf("container %s has no defined healthcheck", c.ID())
	case define.HealthConfigTestCmd:
		newCommand = hcCommand[1:]
	case define.HealthConfigTestCmdShell:
		// TODO: SHELL command from image not available in Container - use Docker default
		newCommand = []string{"/bin/sh", "-c", strings.Join(hcCommand[1:], " ")}
	default:
		// command supplied on command line - pass as-is
		newCommand = hcCommand
	}
	if len(newCommand) < 1 || newCommand[0] == "" {
		return define.HealthCheckNotDefined, "", fmt.Errorf("container %s has no defined healthcheck", c.ID())
	}

	streams := new(define.AttachStreams)
	output := &bytes.Buffer{}

	streams.InputStream = bufio.NewReader(os.Stdin)
	streams.OutputStream = output
	streams.ErrorStream = output
	streams.AttachOutput = true
	streams.AttachError = true
	streams.AttachInput = true

	logrus.Debugf("executing health check command %s for %s", strings.Join(newCommand, " "), c.ID())
	hcResult := define.HealthCheckSuccess
	config := new(ExecConfig)
	config.Command = newCommand
	timeStart := time.Now()
	exitCode, hcErr := c.healthCheckExec(config, c.HealthCheckConfig().Timeout, streams)
	timeEnd := time.Now()
	if hcErr != nil {
		hcResult = define.HealthCheckFailure
		switch {
		case errors.Is(hcErr, define.ErrOCIRuntimeNotFound) ||
			errors.Is(hcErr, define.ErrOCIRuntimePermissionDenied) ||
			errors.Is(hcErr, define.ErrOCIRuntime):
			returnCode = 1
			hcErr = nil
		case errors.Is(hcErr, define.ErrHealthCheckTimeout):
			returnCode = -1
		default:
			returnCode = 125
		}
	} else if exitCode != 0 {
		hcResult = define.HealthCheckFailure
		returnCode = 1
	}

	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return define.HealthCheckInternalError, "", err
		}
	}

	// Handle startup HC
	if isStartup {
		inStartPeriod = true
		if hcErr != nil || exitCode != 0 {
			hcResult = define.HealthCheckStartup
			if err := c.incrementStartupHCFailureCounter(ctx); err != nil {
				return define.HealthCheckInternalError, "", err
			}
		} else {
			if err := c.incrementStartupHCSuccessCounter(ctx); err != nil {
				return define.HealthCheckInternalError, "", err
			}
		}
	}

	if exitCode != 0 && c.ensureState(define.ContainerStateStopped, define.ContainerStateStopping, define.ContainerStateExited) {
		hcResult = define.HealthCheckContainerStopped
	}

	if c.HealthCheckConfig().StartPeriod > 0 {
		// there is a start-period we need to honor; we add startPeriod to container start time
		startPeriodTime := c.state.StartedTime.Add(c.HealthCheckConfig().StartPeriod)
		if timeStart.Before(startPeriodTime) {
			// we are still in the start period, flip the inStartPeriod bool
			inStartPeriod = true
			logrus.Debugf("healthcheck for %s being run in start-period", c.ID())
		}
	}

	eventLog := output.String()
	if c.HealthCheckMaxLogSize() != 0 && len(eventLog) > int(c.HealthCheckMaxLogSize()) {
		eventLog = eventLog[:c.HealthCheckMaxLogSize()]
	}

	hcl := newHealthCheckLog(timeStart, timeEnd, returnCode, eventLog)

	healthCheckResult, err := c.updateHealthCheckLog(hcl, hcResult, inStartPeriod, isStartup)
	if err != nil {
		return hcResult, "", fmt.Errorf("unable to update health check log %s for %s: %w", c.getHealthCheckLogDestination(), c.ID(), err)
	}

	// Write HC event with appropriate status as the last thing before we
	// return.
	if hcResult == define.HealthCheckNotDefined || hcResult == define.HealthCheckInternalError {
		return hcResult, healthCheckResult.Status, hcErr
	}
	if c.runtime.config.Engine.HealthcheckEvents {
		c.newContainerHealthCheckEvent(healthCheckResult)
	}

	return hcResult, healthCheckResult.Status, hcErr
}

func (c *Container) processHealthCheckStatus(status string) error {
	if status != define.HealthCheckUnhealthy {
		return nil
	}

	switch c.config.HealthCheckOnFailureAction {
	case define.HealthCheckOnFailureActionNone: // Nothing to do

	case define.HealthCheckOnFailureActionKill:
		if err := c.Kill(uint(unix.SIGKILL)); err != nil {
			return fmt.Errorf("killing container health-check turned unhealthy: %w", err)
		}

	case define.HealthCheckOnFailureActionRestart:
		// We let the cleanup process handle the restart.  Otherwise
		// the container would be restarted in the context of a
		// transient systemd unit which may cause undesired side
		// effects.
		if err := c.Stop(); err != nil {
			return fmt.Errorf("restarting/stopping container after health-check turned unhealthy: %w", err)
		}

	case define.HealthCheckOnFailureActionStop:
		if err := c.Stop(); err != nil {
			return fmt.Errorf("stopping container after health-check turned unhealthy: %w", err)
		}

	default: // Should not happen but better be safe than sorry
		return fmt.Errorf("unsupported on-failure action %d", c.config.HealthCheckOnFailureAction)
	}

	return nil
}

func checkHealthCheckCanBeRun(c *Container) (define.HealthCheckStatus, error) {
	cstate, err := c.State()
	if err != nil {
		return define.HealthCheckInternalError, err
	}
	if cstate != define.ContainerStateRunning {
		return define.HealthCheckContainerStopped, fmt.Errorf("container %s is not running", c.ID())
	}
	if !c.HasHealthCheck() {
		return define.HealthCheckNotDefined, fmt.Errorf("container %s has no defined healthcheck", c.ID())
	}
	return define.HealthCheckDefined, nil
}

// Increment the current startup healthcheck success counter.
// Can stop the startup HC and start the regular HC if the startup HC has enough
// consecutive successes.
// NOTE: The caller must lock and sync the container.
func (c *Container) incrementStartupHCSuccessCounter(ctx context.Context) error {
	// We don't have a startup HC, can't do anything
	if c.config.StartupHealthCheckConfig == nil {
		return nil
	}

	// Race: someone else got here first
	if c.state.StartupHCPassed {
		return nil
	}

	// Increment the success counter
	c.state.StartupHCSuccessCount++

	logrus.Debugf("Startup healthcheck for container %s succeeded, success counter now %d", c.ID(), c.state.StartupHCSuccessCount)

	// Did we exceed threshold?
	recreateTimer := false
	if c.config.StartupHealthCheckConfig.Successes == 0 || c.state.StartupHCSuccessCount >= c.config.StartupHealthCheckConfig.Successes {
		c.state.StartupHCPassed = true
		c.state.StartupHCSuccessCount = 0
		c.state.StartupHCFailureCount = 0

		recreateTimer = true
	}

	if err := c.save(); err != nil {
		return err
	}

	if !recreateTimer {
		return nil
	}
	// This kills the process the healthcheck is running.
	// Which happens to be us.
	// So this has to be last - after this, systemd serves us a
	// SIGTERM and we exit.
	// Special case, via SIGTERM we exit(1) which means systemd logs a failure in the unit.
	// We do not want this as the unit will be leaked on failure states unless "reset-failed"
	// is called. Fundamentally this is expected so switch it to exit 0.
	// NOTE: This is only safe while being called from "podman healthcheck run" which we know
	// is the case here as we should not alter the exit code of another process that just
	// happened to call this.
	shutdown.SetExitCode(0)
	return c.recreateHealthCheckTimer(ctx, false, true)
}

func (c *Container) recreateHealthCheckTimer(ctx context.Context, isStartup bool, isStartupRemoved bool) error {
	logrus.Infof("Startup healthcheck for container %s passed, recreating timer", c.ID())

	oldUnit := c.state.HCUnitName
	// Create the new, standard healthcheck timer first.
	interval := c.HealthCheckConfig().Interval.String()
	if isStartup {
		interval = c.config.StartupHealthCheckConfig.StartInterval.String()
	}

	if err := c.createTimer(interval, isStartup); err != nil {
		return fmt.Errorf("recreating container %s (isStartup: %t) healthcheck: %v", c.ID(), isStartup, err)
	}
	if err := c.startTimer(isStartup); err != nil {
		return fmt.Errorf("restarting container %s (isStartup: %t) healthcheck timer: %v", c.ID(), isStartup, err)
	}

	if err := c.removeTransientFiles(ctx, isStartupRemoved, oldUnit); err != nil {
		return fmt.Errorf("removing container %s healthcheck: %v", c.ID(), err)
	}
	return nil
}

// Increment the current startup healthcheck failure counter.
// Can restart the container if the HC fails enough times consecutively.
// NOTE: The caller must lock and sync the container.
func (c *Container) incrementStartupHCFailureCounter(ctx context.Context) error {
	// We don't have a startup HC, can't do anything
	if c.config.StartupHealthCheckConfig == nil {
		return nil
	}

	// Race: someone else got here first
	if c.state.StartupHCPassed {
		return nil
	}

	c.state.StartupHCFailureCount++

	logrus.Debugf("Startup healthcheck for container %s failed, failure counter now %d", c.ID(), c.state.StartupHCFailureCount)

	if c.config.StartupHealthCheckConfig.Retries != 0 && c.state.StartupHCFailureCount >= c.config.StartupHealthCheckConfig.Retries {
		logrus.Infof("Restarting container %s as startup healthcheck failed", c.ID())
		// Restart the container
		if err := c.restartWithTimeout(ctx, c.config.StopTimeout); err != nil {
			return fmt.Errorf("restarting container %s after healthcheck failure: %v", c.ID(), err)
		}
		return nil
	}

	return c.save()
}

func newHealthCheckLog(start, end time.Time, exitCode int, log string) define.HealthCheckLog {
	return define.HealthCheckLog{
		Start:    start.Format(time.RFC3339Nano),
		End:      end.Format(time.RFC3339Nano),
		ExitCode: exitCode,
		Output:   log,
	}
}

// updateHealthStatus updates the health status of the container
// in the healthcheck log
func (c *Container) updateHealthStatus(status string) error {
	healthCheck, err := c.readHealthCheckLog()
	if err != nil {
		return err
	}
	healthCheck.Status = status
	return c.writeHealthCheckLog(healthCheck)
}

// isUnhealthy returns true if the current health check status is unhealthy.
func (c *Container) isUnhealthy() (bool, error) {
	if !c.HasHealthCheck() {
		return false, nil
	}
	healthCheck, err := c.readHealthCheckLog()
	if err != nil {
		return false, err
	}
	return healthCheck.Status == define.HealthCheckUnhealthy, nil
}

// UpdateHealthCheckLog parses the health check results and writes the log
// NOTE: The caller must lock the container.
func (c *Container) updateHealthCheckLog(hcl define.HealthCheckLog, hcResult define.HealthCheckStatus, inStartPeriod, isStartup bool) (define.HealthCheckResults, error) {
	// If we are playing a kube yaml then let's honor the start period time for
	// both failing and succeeding cases to match kube behavior.
	// So don't update the health check log till the start period is over
	if _, ok := c.config.Spec.Annotations[define.KubeHealthCheckAnnotation]; ok && inStartPeriod && !isStartup {
		return define.HealthCheckResults{}, nil
	}

	healthCheck, err := c.readHealthCheckLog()
	if err != nil {
		return define.HealthCheckResults{}, err
	}
	if hcl.ExitCode == 0 {
		//	set status to healthy, reset failing state to 0
		healthCheck.Status = define.HealthCheckHealthy
		healthCheck.FailingStreak = 0
	} else {
		if len(healthCheck.Status) < 1 {
			healthCheck.Status = define.HealthCheckHealthy
		}
		if hcResult == define.HealthCheckContainerStopped {
			healthCheck.Status = define.HealthCheckStopped
		} else if !inStartPeriod {
			// increment failing streak
			healthCheck.FailingStreak++
			// if failing streak > retries, then status to unhealthy
			if healthCheck.FailingStreak >= c.HealthCheckConfig().Retries {
				healthCheck.Status = define.HealthCheckUnhealthy
			}
		}
	}
	healthCheck.Log = append(healthCheck.Log, hcl)
	if c.HealthCheckMaxLogCount() != 0 && len(healthCheck.Log) > int(c.HealthCheckMaxLogCount()) {
		healthCheck.Log = healthCheck.Log[1:]
	}
	return healthCheck, c.writeHealthCheckLog(healthCheck)
}

func (c *Container) witeToFileHealthCheckResults(path string, result define.HealthCheckResults) error {
	newResults, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("unable to marshall healthchecks for writing: %w", err)
	}
	return os.WriteFile(path, newResults, 0o700)
}

func (c *Container) getHealthCheckLogDestination() string {
	var destination string
	switch c.HealthCheckLogDestination() {
	case define.DefaultHealthCheckLocalDestination, define.HealthCheckEventsLoggerDestination, "":
		destination = filepath.Join(filepath.Dir(c.state.RunDir), "healthcheck.log")
	default:
		destination = filepath.Join(c.HealthCheckLogDestination(), c.ID()+"-healthcheck.log")
	}
	return destination
}

func (c *Container) writeHealthCheckLog(result define.HealthCheckResults) error {
	return c.witeToFileHealthCheckResults(c.getHealthCheckLogDestination(), result)
}

// readHealthCheckLog read HealthCheck logs from the path or events_logger
// The caller should lock the container before this function is called.
func (c *Container) readHealthCheckLog() (define.HealthCheckResults, error) {
	return c.readFromFileHealthCheckLog(c.getHealthCheckLogDestination())
}

// readFromFileHealthCheckLog returns HealthCheck results by reading the container's
// health check log file.  If the health check log file does not exist, then
// an empty healthcheck struct is returned
// The caller should lock the container before this function is called.
func (c *Container) readFromFileHealthCheckLog(path string) (define.HealthCheckResults, error) {
	var healthCheck define.HealthCheckResults
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// If the file does not exists just return empty healthcheck and no error.
			return healthCheck, nil
		}
		return healthCheck, fmt.Errorf("failed to read health check log file: %w", err)
	}
	if err := json.Unmarshal(b, &healthCheck); err != nil {
		return healthCheck, fmt.Errorf("failed to unmarshal existing healthcheck results in %s: %w", path, err)
	}
	return healthCheck, nil
}

// HealthCheckStatus returns the current state of a container with a healthcheck.
// Returns an empty string if no health check is defined for the container.
func (c *Container) HealthCheckStatus() (string, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
	}
	return c.healthCheckStatus()
}

// Internal function to return the current state of a container with a healthcheck.
// This function does not lock the container.
func (c *Container) healthCheckStatus() (string, error) {
	if !c.HasHealthCheck() {
		return "", nil
	}

	if err := c.syncContainer(); err != nil {
		return "", err
	}

	results, err := c.readHealthCheckLog()
	if err != nil {
		return "", fmt.Errorf("unable to get healthcheck log for %s: %w", c.ID(), err)
	}

	return results.Status, nil
}
