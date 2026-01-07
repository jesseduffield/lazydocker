//go:build !remote

package libpod

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/sirupsen/logrus"
)

// newEventer returns an eventer that can be used to read/write events
func (r *Runtime) newEventer() (events.Eventer, error) {
	if r.config.Engine.EventsLogFilePath == "" {
		// default, use path under tmpdir when none was explicitly set by the user
		r.config.Engine.EventsLogFilePath = filepath.Join(r.config.Engine.TmpDir, "events", "events.log")
	}
	options := events.EventerOptions{
		EventerType:    r.config.Engine.EventsLogger,
		LogFilePath:    r.config.Engine.EventsLogFilePath,
		LogFileMaxSize: r.config.Engine.EventsLogMaxSize(),
	}
	return events.NewEventer(options)
}

// newContainerEvent creates a new event based on a container
func (c *Container) newContainerEvent(status events.Status) {
	if err := c.newContainerEventWithInspectData(status, define.HealthCheckResults{}, false); err != nil {
		logrus.Errorf("Unable to write container event: %v", err)
	}
}

// newContainerHealthCheckEvent creates a new healthcheck event with the given status
func (c *Container) newContainerHealthCheckEvent(healthCheckResult define.HealthCheckResults) {
	if err := c.newContainerEventWithInspectData(events.HealthStatus, healthCheckResult, false); err != nil {
		logrus.Errorf("Unable to write container event: %v", err)
	}
}

// newContainerEventWithInspectData creates a new event and sets the
// ContainerInspectData field if inspectData is set.
func (c *Container) newContainerEventWithInspectData(status events.Status, healthCheckResult define.HealthCheckResults, inspectData bool) error {
	e := events.NewEvent(status)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Image = c.config.RootfsImageName
	e.Type = events.Container
	e.HealthStatus = healthCheckResult.Status
	if c.HealthCheckLogDestination() == define.HealthCheckEventsLoggerDestination {
		if len(healthCheckResult.Log) > 0 {
			logData, err := json.Marshal(healthCheckResult.Log[len(healthCheckResult.Log)-1])
			if err != nil {
				return fmt.Errorf("unable to marshall healthcheck log for writing: %w", err)
			}
			e.HealthLog = string(logData)
		}
	}
	e.HealthFailingStreak = healthCheckResult.FailingStreak

	e.Details = events.Details{
		PodID:      c.PodID(),
		Attributes: c.Labels(),
	}

	if inspectData {
		err := func() error {
			data, err := c.inspectLocked(true)
			if err != nil {
				return err
			}
			rawData, err := json.Marshal(data)
			if err != nil {
				return err
			}
			e.Details.ContainerInspectData = string(rawData)
			return nil
		}()
		if err != nil {
			return fmt.Errorf("adding inspect data to container-create event: %v", err)
		}
	}

	if status == events.Remove {
		exitCode, err := c.runtime.state.GetContainerExitCode(c.ID())
		if err == nil {
			intExitCode := int(exitCode)
			e.ContainerExitCode = &intExitCode
		}
	}

	return c.runtime.eventer.Write(e)
}

// newContainerExitedEvent creates a new event for a container's death
func (c *Container) newContainerExitedEvent(exitCode int32) {
	e := events.NewEvent(events.Exited)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Image = c.config.RootfsImageName
	e.Type = events.Container
	e.PodID = c.PodID()
	intExitCode := int(exitCode)
	e.ContainerExitCode = &intExitCode

	e.Details = events.Details{
		Attributes: c.Labels(),
	}

	if err := c.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write container exited event: %q", err)
	}
}

// newExecDiedEvent creates a new event for an exec session's death
func (c *Container) newExecDiedEvent(sessionID string, exitCode int) {
	e := events.NewEvent(events.ExecDied)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Image = c.config.RootfsImageName
	e.Type = events.Container
	intExitCode := exitCode
	e.ContainerExitCode = &intExitCode
	e.Attributes = make(map[string]string)
	e.Attributes["execID"] = sessionID

	e.Details = events.Details{
		Attributes: c.Labels(),
	}

	if err := c.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write exec died event: %q", err)
	}
}

// newNetworkEvent creates a new event based on a network create/remove
func (r *Runtime) NewNetworkEvent(status events.Status, netName, netID, netDriver string) {
	e := events.NewEvent(status)
	e.Network = netName
	e.ID = netID
	e.Attributes = make(map[string]string)
	e.Attributes["driver"] = netDriver
	e.Type = events.Network
	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write network event: %q", err)
	}
}

// newNetworkEvent creates a new event based on a network connect/disconnect
func (c *Container) newNetworkEvent(status events.Status, netName string) {
	e := events.NewEvent(status)
	e.ID = c.ID()
	e.Name = c.Name()
	e.Type = events.Network
	e.Network = netName
	if err := c.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write pod event: %q", err)
	}
}

// newPodEvent creates a new event for a libpod pod
func (p *Pod) newPodEvent(status events.Status) {
	e := events.NewEvent(status)
	e.ID = p.ID()
	e.Name = p.Name()
	e.Type = events.Pod
	if err := p.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write pod event: %q", err)
	}
}

// NewSystemEvent creates a new event for libpod as a whole.
func (r *Runtime) NewSystemEvent(status events.Status) {
	e := events.NewEvent(status)
	e.Type = events.System

	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write system event: %q", err)
	}
}

// newVolumeEvent creates a new event for a libpod volume
func (v *Volume) newVolumeEvent(status events.Status) {
	e := events.NewEvent(status)
	e.Name = v.Name()
	e.Type = events.Volume
	if err := v.runtime.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write volume event: %q", err)
	}
}

// NewSecretEvent creates a new event for a libpod secret
func (r *Runtime) NewSecretEvent(status events.Status, secretID string) {
	e := events.NewEvent(status)
	e.ID = secretID
	e.Type = events.Secret
	if err := r.eventer.Write(e); err != nil {
		logrus.Errorf("Unable to write secret event: %q", err)
	}
}

// Events is a wrapper function for everyone to begin tailing the events log
// with options
func (r *Runtime) Events(ctx context.Context, options events.ReadOptions) error {
	return r.eventer.Read(ctx, options)
}

// GetEvents reads the event log and returns events based on input filters
func (r *Runtime) GetEvents(ctx context.Context, filters []string) ([]*events.Event, error) {
	eventChannel := make(chan events.ReadResult)
	options := events.ReadOptions{
		EventChannel: eventChannel,
		Filters:      filters,
		FromStart:    true,
		Stream:       false,
	}

	err := r.eventer.Read(ctx, options)
	if err != nil {
		return nil, err
	}

	logEvents := make([]*events.Event, 0, len(eventChannel))
	for evt := range eventChannel {
		// we ignore any error here, this is only used on the backup
		// GetExecDiedEvent() died path as best effort anyway
		if evt.Error == nil {
			logEvents = append(logEvents, evt.Event)
		}
	}

	return logEvents, nil
}

// GetExecDiedEvent takes a container name or ID, exec session ID, and returns
// that exec session's Died event (if it has already occurred).
func (r *Runtime) GetExecDiedEvent(ctx context.Context, nameOrID, execSessionID string) (*events.Event, error) {
	filters := []string{
		fmt.Sprintf("container=%s", nameOrID),
		"event=exec_died",
		"type=container",
		fmt.Sprintf("label=execID=%s", execSessionID),
	}

	containerEvents, err := r.GetEvents(ctx, filters)
	if err != nil {
		return nil, err
	}
	// There *should* only be one event maximum.
	// But... just in case... let's not blow up if there's more than one.
	if len(containerEvents) < 1 {
		return nil, fmt.Errorf("exec died event for session %s (container %s) not found: %w", execSessionID, nameOrID, events.ErrEventNotFound)
	}
	return containerEvents[len(containerEvents)-1], nil
}
