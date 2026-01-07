package define

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.podman.io/image/v5/manifest"
)

const (
	// HealthCheckHealthy describes a healthy container
	HealthCheckHealthy string = "healthy"
	// HealthCheckUnhealthy describes an unhealthy container
	HealthCheckUnhealthy string = "unhealthy"
	// HealthCheckStarting describes the time between when the container starts
	// and the start-period (time allowed for the container to start and application
	// to be running) expires.
	HealthCheckStarting string = "starting"
	// HealthCheckReset describes reset of HealthCheck logs
	HealthCheckReset string = "reset"
	// HealthCheckStopped describes the time when container was stopped during HealthCheck
	// and HealthCheck was terminated
	HealthCheckStopped string = "stopped"
)

// HealthCheckStatus represents the current state of a container
type HealthCheckStatus int

const (
	// HealthCheckSuccess means the health worked
	HealthCheckSuccess HealthCheckStatus = iota
	// HealthCheckFailure means the health ran and failed
	HealthCheckFailure HealthCheckStatus = iota
	// HealthCheckContainerStopped means the health check cannot
	// be run because the container is stopped
	HealthCheckContainerStopped HealthCheckStatus = iota
	// HealthCheckContainerNotFound means the container could
	// not be found in local store
	HealthCheckContainerNotFound HealthCheckStatus = iota
	// HealthCheckNotDefined means the container has no health
	// check defined in it
	HealthCheckNotDefined HealthCheckStatus = iota
	// HealthCheckInternalError means some something failed obtaining or running
	// a given health check
	HealthCheckInternalError HealthCheckStatus = iota
	// HealthCheckDefined means the healthcheck was found on the container
	HealthCheckDefined HealthCheckStatus = iota
	// HealthCheckStartup means the healthcheck was unhealthy, but is still
	// either within the startup HC or the startup period of the healthcheck
	HealthCheckStartup HealthCheckStatus = iota
)

func (s HealthCheckStatus) String() string {
	switch s {
	case HealthCheckSuccess:
		return HealthCheckHealthy
	case HealthCheckStartup:
		return HealthCheckStarting
	case HealthCheckContainerStopped:
		return HealthCheckStopped
	default:
		return HealthCheckUnhealthy
	}
}

// Healthcheck defaults.  These are used both in the cli as well in
// libpod and were moved from cmd/podman/common
const (
	// DefaultHealthCheckInterval default value
	DefaultHealthCheckInterval = "30s"
	// DefaultHealthCheckRetries default value
	DefaultHealthCheckRetries uint = 3
	// DefaultHealthCheckStartPeriod default value
	DefaultHealthCheckStartPeriod = "0s"
	// DefaultHealthCheckTimeout default value
	DefaultHealthCheckTimeout = "30s"
	// DefaultHealthMaxLogCount default value
	DefaultHealthMaxLogCount uint = 5
	// DefaultHealthMaxLogSize default value
	DefaultHealthMaxLogSize uint = 500
	// DefaultHealthCheckLocalDestination default value
	DefaultHealthCheckLocalDestination string = "local"
)

const HealthCheckEventsLoggerDestination string = "events_logger"

// HealthConfig.Test options
const (
	// HealthConfigTestNone disables healthcheck
	HealthConfigTestNone = "NONE"
	// HealthConfigTestCmd execs arguments directly
	HealthConfigTestCmd = "CMD"
	// HealthConfigTestCmdShell runs commands with the system's default shell
	HealthConfigTestCmdShell = "CMD-SHELL"
)

// HealthCheckOnFailureAction defines how Podman reacts when a container's health
// status turns unhealthy.
type HealthCheckOnFailureAction int

// Healthcheck on-failure actions.
const (
	// HealthCheckOnFailureActionNonce instructs Podman to not react on an unhealthy status.
	HealthCheckOnFailureActionNone = iota // Must be first iota for backwards compatibility
	// HealthCheckOnFailureActionInvalid denotes an invalid on-failure policy.
	HealthCheckOnFailureActionInvalid = iota
	// HealthCheckOnFailureActionNonce instructs Podman to kill the container on an unhealthy status.
	HealthCheckOnFailureActionKill = iota
	// HealthCheckOnFailureActionNonce instructs Podman to restart the container on an unhealthy status.
	HealthCheckOnFailureActionRestart = iota
	// HealthCheckOnFailureActionNonce instructs Podman to stop the container on an unhealthy status.
	HealthCheckOnFailureActionStop = iota
)

// String representations for on-failure actions.
const (
	strHealthCheckOnFailureActionNone    = "none"
	strHealthCheckOnFailureActionInvalid = "invalid"
	strHealthCheckOnFailureActionKill    = "kill"
	strHealthCheckOnFailureActionRestart = "restart"
	strHealthCheckOnFailureActionStop    = "stop"
)

// SupportedHealthCheckOnFailureActions lists all supported healthcheck restart policies.
var SupportedHealthCheckOnFailureActions = []string{
	strHealthCheckOnFailureActionNone,
	strHealthCheckOnFailureActionKill,
	strHealthCheckOnFailureActionRestart,
	strHealthCheckOnFailureActionStop,
}

// String returns the string representation of the HealthCheckOnFailureAction.
func (h HealthCheckOnFailureAction) String() string {
	switch h {
	case HealthCheckOnFailureActionNone:
		return strHealthCheckOnFailureActionNone
	case HealthCheckOnFailureActionKill:
		return strHealthCheckOnFailureActionKill
	case HealthCheckOnFailureActionRestart:
		return strHealthCheckOnFailureActionRestart
	case HealthCheckOnFailureActionStop:
		return strHealthCheckOnFailureActionStop
	default:
		return strHealthCheckOnFailureActionInvalid
	}
}

// ParseHealthCheckOnFailureAction parses the specified string into a HealthCheckOnFailureAction.
// An error is returned for an invalid input.
func ParseHealthCheckOnFailureAction(s string) (HealthCheckOnFailureAction, error) {
	switch s {
	case "", strHealthCheckOnFailureActionNone:
		return HealthCheckOnFailureActionNone, nil
	case strHealthCheckOnFailureActionKill:
		return HealthCheckOnFailureActionKill, nil
	case strHealthCheckOnFailureActionRestart:
		return HealthCheckOnFailureActionRestart, nil
	case strHealthCheckOnFailureActionStop:
		return HealthCheckOnFailureActionStop, nil
	default:
		err := fmt.Errorf("invalid on-failure action %q for health check: supported actions are %s", s, strings.Join(SupportedHealthCheckOnFailureActions, ","))
		return HealthCheckOnFailureActionInvalid, err
	}
}

// StartupHealthCheck is the configuration of a startup healthcheck.
type StartupHealthCheck struct {
	manifest.Schema2HealthConfig
	// Successes are the number of successes required to mark the startup HC
	// as passed.
	// If set to 0, a single success will mark the HC as passed.
	Successes int `json:",omitempty"`
}

type UpdateHealthCheckConfig struct {
	// HealthLogDestination set the destination of the HealthCheck log.
	// Directory path, local or events_logger (local use container state file)
	// Warning: Changing this setting may cause the loss of previous logs!
	HealthLogDestination *string `json:"health_log_destination,omitempty"`
	// HealthMaxLogSize set maximum length in characters of stored HealthCheck log.
	// ('0' value means an infinite log length)
	HealthMaxLogSize *uint `json:"health_max_log_size,omitempty"`
	// HealthMaxLogCount set maximum number of attempts in the HealthCheck log file.
	// ('0' value means an infinite number of attempts in the log file)
	HealthMaxLogCount *uint `json:"health_max_log_count,omitempty"`
	// HealthOnFailure set the action to take once the container turns unhealthy.
	HealthOnFailure *string `json:"health_on_failure,omitempty"`
	// Disable healthchecks on container.
	NoHealthCheck *bool `json:"no_healthcheck,omitempty"`
	// HealthCmd set a healthcheck command for the container. ('none' disables the existing healthcheck)
	HealthCmd *string `json:"health_cmd,omitempty"`
	// HealthInterval set an interval for the healthcheck.
	// (a value of disable results in no automatic timer setup) Changing this setting resets timer.
	HealthInterval *string `json:"health_interval,omitempty"`
	// HealthRetries set the number of retries allowed before a healthcheck is considered to be unhealthy.
	HealthRetries *uint `json:"health_retries,omitempty"`
	// HealthTimeout set the maximum time allowed to complete the healthcheck before an interval is considered failed.
	HealthTimeout *string `json:"health_timeout,omitempty"`
	// HealthStartPeriod set the initialization time needed for a container to bootstrap.
	HealthStartPeriod *string `json:"health_start_period,omitempty"`
	// HealthStartupCmd set a startup healthcheck command for the container.
	HealthStartupCmd *string `json:"health_startup_cmd,omitempty"`
	// HealthStartupInterval set an interval for the startup healthcheck.
	// Changing this setting resets the timer, depending on the state of the container.
	HealthStartupInterval *string `json:"health_startup_interval,omitempty"`
	// HealthStartupRetries set the maximum number of retries before the startup healthcheck will restart the container.
	HealthStartupRetries *uint `json:"health_startup_retries,omitempty"`
	// HealthStartupTimeout set the maximum amount of time that the startup healthcheck may take before it is considered failed.
	HealthStartupTimeout *string `json:"health_startup_timeout,omitempty"`
	// HealthStartupSuccess set the number of consecutive successes before the startup healthcheck is marked as successful
	// and the normal healthcheck begins (0 indicates any success will start the regular healthcheck)
	HealthStartupSuccess *uint `json:"health_startup_success,omitempty"`
}

func (u *UpdateHealthCheckConfig) IsStartupHealthCheckCommandSet(startupHealthCheck *StartupHealthCheck) bool {
	containsStartupHealthCheckCmd := u.HealthStartupCmd != nil
	containsFlags := (u.HealthStartupInterval != nil || u.HealthStartupRetries != nil ||
		u.HealthStartupTimeout != nil || u.HealthStartupSuccess != nil)
	return startupHealthCheck == nil && !containsStartupHealthCheckCmd && containsFlags
}

func (u *UpdateHealthCheckConfig) IsHealthCheckCommandSet(healthCheck *manifest.Schema2HealthConfig) bool {
	containsStartupHealthCheckCmd := u.HealthCmd != nil
	containsFlags := (u.HealthInterval != nil || u.HealthRetries != nil ||
		u.HealthTimeout != nil || u.HealthStartPeriod != nil)
	return healthCheck == nil && !containsStartupHealthCheckCmd && containsFlags
}

func (u *UpdateHealthCheckConfig) SetNewStartupHealthCheckConfigTo(healthCheckOptions *HealthCheckOptions) bool {
	changed := false

	if u.HealthStartupCmd != nil {
		healthCheckOptions.Cmd = *u.HealthStartupCmd
		changed = true
	}

	if u.HealthStartupInterval != nil {
		healthCheckOptions.Interval = *u.HealthStartupInterval
		changed = true
	}

	if u.HealthStartupRetries != nil {
		healthCheckOptions.Retries = int(*u.HealthStartupRetries)
		changed = true
	}

	if u.HealthStartupTimeout != nil {
		healthCheckOptions.Timeout = *u.HealthStartupTimeout
		changed = true
	}

	if u.HealthStartupSuccess != nil {
		healthCheckOptions.Successes = int(*u.HealthStartupSuccess)
		changed = true
	}

	healthCheckOptions.StartPeriod = "1s"

	return changed
}

func (u *UpdateHealthCheckConfig) SetNewHealthCheckConfigTo(healthCheckOptions *HealthCheckOptions) bool {
	changed := false

	if u.HealthCmd != nil {
		healthCheckOptions.Cmd = *u.HealthCmd
		changed = true
	}

	if u.HealthInterval != nil {
		healthCheckOptions.Interval = *u.HealthInterval
		changed = true
	}

	if u.HealthRetries != nil {
		healthCheckOptions.Retries = int(*u.HealthRetries)
		changed = true
	}

	if u.HealthTimeout != nil {
		healthCheckOptions.Timeout = *u.HealthTimeout
		changed = true
	}

	if u.HealthStartPeriod != nil {
		healthCheckOptions.StartPeriod = *u.HealthStartPeriod
		changed = true
	}

	return changed
}

func GetValidHealthCheckDestination(destination string) (string, error) {
	if destination == HealthCheckEventsLoggerDestination || destination == DefaultHealthCheckLocalDestination {
		return destination, nil
	}

	fileInfo, err := os.Stat(destination)
	if err != nil {
		return "", fmt.Errorf("HealthCheck Log '%s' destination error: %w", destination, err)
	}
	mode := fileInfo.Mode()
	if !mode.IsDir() {
		return "", fmt.Errorf("HealthCheck Log '%s' destination must be directory", destination)
	}

	absPath, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

func (u *UpdateHealthCheckConfig) GetNewGlobalHealthCheck() (GlobalHealthCheckOptions, error) {
	globalOptions := GlobalHealthCheckOptions{}

	healthLogDestination := u.HealthLogDestination
	if u.HealthLogDestination != nil {
		dest, err := GetValidHealthCheckDestination(*u.HealthLogDestination)
		if err != nil {
			return GlobalHealthCheckOptions{}, err
		}
		healthLogDestination = &dest
	}
	globalOptions.HealthLogDestination = healthLogDestination

	globalOptions.HealthMaxLogSize = u.HealthMaxLogSize

	globalOptions.HealthMaxLogCount = u.HealthMaxLogCount

	if u.HealthOnFailure != nil {
		val, err := ParseHealthCheckOnFailureAction(*u.HealthOnFailure)
		if err != nil {
			return globalOptions, err
		}
		globalOptions.HealthCheckOnFailureAction = &val
	}

	return globalOptions, nil
}

type HealthCheckOptions struct {
	Cmd         string
	Interval    string
	Retries     int
	Timeout     string
	StartPeriod string
	Successes   int
}

type GlobalHealthCheckOptions struct {
	HealthLogDestination       *string
	HealthMaxLogCount          *uint
	HealthMaxLogSize           *uint
	HealthCheckOnFailureAction *HealthCheckOnFailureAction
}
