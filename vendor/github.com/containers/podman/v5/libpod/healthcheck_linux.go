//go:build !remote && systemd

package libpod

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"

	"github.com/containers/podman/v5/pkg/errorhandling"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/systemd"
	"github.com/sirupsen/logrus"
	systemdCommon "go.podman.io/common/pkg/systemd"
)

// createTimer systemd timers for healthchecks of a container
func (c *Container) createTimer(interval string, isStartup bool) error {
	if c.disableHealthCheckSystemd(isStartup) {
		return nil
	}

	hcUnitName := c.hcUnitName(isStartup, false)

	podman, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get path for podman for a health check timer: %w", err)
	}

	var cmd = []string{"--property", "LogLevelMax=notice"}
	if rootless.IsRootless() {
		cmd = append(cmd, "--user")
	}
	path := os.Getenv("PATH")
	if path != "" {
		cmd = append(cmd, "--setenv=PATH="+path)
	}

	cmd = append(cmd, "--unit", hcUnitName, fmt.Sprintf("--on-unit-inactive=%s", interval), "--timer-property=AccuracySec=1s", podman)

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		cmd = append(cmd, "--log-level=debug", "--syslog")
	}

	cmd = append(cmd, "healthcheck", "run", c.ID())

	conn, err := systemd.ConnectToDBUS()
	if err != nil {
		return fmt.Errorf("unable to get systemd connection to add healthchecks: %w", err)
	}
	conn.Close()
	logrus.Debugf("creating systemd-transient files: %s %s", "systemd-run", cmd)
	systemdRun := exec.Command("systemd-run", cmd...)
	if output, err := systemdRun.CombinedOutput(); err != nil {
		exitError := &exec.ExitError{}
		if errors.As(err, &exitError) {
			return fmt.Errorf("systemd-run failed: %w: output: %s", err, strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("failed to execute systemd-run: %w", err)
	}

	c.state.HCUnitName = hcUnitName
	if err := c.save(); err != nil {
		return fmt.Errorf("saving container %s healthcheck unit name: %w", c.ID(), err)
	}

	return nil
}

// Wait for a message on the channel.  Throw an error if the message is not "done".
func systemdOpSuccessful(c chan string) error {
	msg := <-c
	switch msg {
	case "done":
		return nil
	default:
		return fmt.Errorf("expected %q but received %q", "done", msg)
	}
}

// startTimer starts a systemd timer for the healthchecks
func (c *Container) startTimer(isStartup bool) error {
	if c.disableHealthCheckSystemd(isStartup) {
		return nil
	}

	hcUnitName := c.state.HCUnitName
	if hcUnitName == "" {
		hcUnitName = c.hcUnitName(isStartup, true)
	}

	conn, err := systemd.ConnectToDBUS()
	if err != nil {
		return fmt.Errorf("unable to get systemd connection to start healthchecks: %w", err)
	}
	defer conn.Close()

	startFile := fmt.Sprintf("%s.service", hcUnitName)
	startChan := make(chan string)
	if _, err := conn.RestartUnitContext(context.Background(), startFile, "fail", startChan); err != nil {
		return err
	}
	if err := systemdOpSuccessful(startChan); err != nil {
		return fmt.Errorf("starting systemd health-check timer %q: %w", startFile, err)
	}

	return nil
}

// removeTransientFiles removes the systemd timer and unit files
// for the container
func (c *Container) removeTransientFiles(ctx context.Context, isStartup bool, unitName string) error {
	if c.disableHealthCheckSystemd(isStartup) {
		return nil
	}
	conn, err := systemd.ConnectToDBUS()
	if err != nil {
		return fmt.Errorf("unable to get systemd connection to remove healthchecks: %w", err)
	}
	defer conn.Close()

	// Errors are returned at the very end. Let's make sure to stop and
	// clean up as much as possible.
	stopErrors := []error{}

	if unitName == "" {
		unitName = c.hcUnitName(isStartup, true)
	}
	// Stop the timer before the service to make sure the timer does not
	// fire after the service is stopped.
	timerChan := make(chan string)
	timerFile := fmt.Sprintf("%s.timer", unitName)
	if _, err := conn.StopUnitContext(ctx, timerFile, "ignore-dependencies", timerChan); err != nil {
		if !strings.HasSuffix(err.Error(), ".timer not loaded.") {
			stopErrors = append(stopErrors, fmt.Errorf("removing health-check timer %q: %w", timerFile, err))
		}
	} else if err := systemdOpSuccessful(timerChan); err != nil {
		stopErrors = append(stopErrors, fmt.Errorf("stopping systemd health-check timer %q: %w", timerFile, err))
	}

	serviceChan := make(chan string)
	serviceFile := fmt.Sprintf("%s.service", unitName)
	if _, err := conn.StopUnitContext(ctx, serviceFile, "ignore-dependencies", serviceChan); err != nil {
		if !strings.HasSuffix(err.Error(), ".service not loaded.") {
			stopErrors = append(stopErrors, fmt.Errorf("removing health-check service %q: %w", serviceFile, err))
		}
	} else if err := systemdOpSuccessful(serviceChan); err != nil {
		stopErrors = append(stopErrors, fmt.Errorf("stopping systemd health-check service %q: %w", serviceFile, err))
	}
	// Reset the service after stopping it to make sure it's being removed, systemd keep failed transient services
	// around in its state. We do not care about the error and we need to ensure to reset the state so we do not
	// leak resources forever.
	if err := conn.ResetFailedUnitContext(ctx, serviceFile); err != nil {
		logrus.Debugf("Failed to reset unit file: %q", err)
	}

	return errorhandling.JoinErrors(stopErrors)
}

func (c *Container) disableHealthCheckSystemd(isStartup bool) bool {
	if !systemdCommon.RunsOnSystemd() || os.Getenv("DISABLE_HC_SYSTEMD") == "true" {
		return true
	}
	if isStartup {
		if c.config.StartupHealthCheckConfig.Interval == 0 {
			return true
		}
	}
	if c.config.HealthCheckConfig.Interval == 0 {
		return true
	}
	return false
}

// Systemd unit name for the healthcheck systemd unit.
// Bare indicates that a random suffix should not be applied to the name. This
// was default behavior previously, and is used for backwards compatibility.
func (c *Container) hcUnitName(isStartup, bare bool) string {
	unitName := c.ID()
	if isStartup {
		unitName += "-startup"
	}
	if !bare {
		// Ensure that unit names are unique from run to run by appending
		// a random suffix.
		// Ref: RH Jira RHEL-26105
		unitName += fmt.Sprintf("-%x", rand.Int())
	}
	return unitName
}
