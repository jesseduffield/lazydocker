//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/logs"
	systemdDefine "github.com/containers/podman/v5/pkg/systemd/define"
	"github.com/nxadm/tail"
	"github.com/nxadm/tail/watch"
	"github.com/sirupsen/logrus"
)

// logDrivers stores the currently available log drivers, do not modify
var logDrivers []string

func init() {
	logDrivers = append(logDrivers, define.KubernetesLogging, define.NoLogging, define.PassthroughLogging)
}

// Log is a runtime function that can read one or more container logs.
func (r *Runtime) Log(ctx context.Context, containers []*Container, options *logs.LogOptions, logChannel chan *logs.LogLine) error {
	for c, ctr := range containers {
		if err := ctr.ReadLog(ctx, options, logChannel, int64(c)); err != nil {
			return err
		}
	}
	return nil
}

// ReadLog reads a container's log based on the input options and returns log lines over a channel.
func (c *Container) ReadLog(ctx context.Context, options *logs.LogOptions, logChannel chan *logs.LogLine, colorID int64) error {
	switch c.LogDriver() {
	case define.PassthroughLogging:
		// if running under systemd fallback to a more native journald reading
		if unitName, ok := c.config.Labels[systemdDefine.EnvVariable]; ok {
			return c.readFromJournal(ctx, options, logChannel, colorID, unitName)
		}
		return fmt.Errorf("this container is using the 'passthrough' log driver, cannot read logs: %w", define.ErrNoLogs)
	case define.NoLogging:
		return fmt.Errorf("this container is using the 'none' log driver, cannot read logs: %w", define.ErrNoLogs)
	case define.JournaldLogging:
		return c.readFromJournal(ctx, options, logChannel, colorID, "")
	case define.JSONLogging:
		// TODO provide a separate implementation of this when Conmon
		// has support.
		fallthrough
	case define.KubernetesLogging, "":
		return c.readFromLogFile(ctx, options, logChannel, colorID)
	default:
		return fmt.Errorf("unrecognized log driver %q, cannot read logs: %w", c.LogDriver(), define.ErrInternal)
	}
}

func (c *Container) readFromLogFile(ctx context.Context, options *logs.LogOptions, logChannel chan *logs.LogLine, colorID int64) error {
	t, tailLog, err := logs.GetLogFile(c.LogPath(), options)
	if err != nil {
		// If the log file does not exist, this is not fatal.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("unable to read log file %s for %s : %w", c.ID(), c.LogPath(), err)
	}
	options.WaitGroup.Add(1)
	go func() {
		if options.Until.After(time.Now()) {
			time.Sleep(time.Until(options.Until))
			if err := t.Stop(); err != nil {
				logrus.Errorf("Stopping logger: %v", err)
			}
		}
	}()

	go func() {
		for _, nll := range tailLog {
			nll.CID = c.ID()
			nll.CName = c.Name()
			nll.ColorID = colorID
			if nll.Since(options.Since) && nll.Until(options.Until) {
				logChannel <- nll
			}
		}
		defer options.WaitGroup.Done()
		var line *tail.Line
		var ok bool
		for {
			select {
			case <-ctx.Done():
				// the consumer has cancelled
				t.Kill(errors.New("hangup by client"))
				return
			case line, ok = <-t.Lines:
				if !ok {
					// channel was closed
					return
				}
			}
			nll, err := logs.NewLogLine(line.Text)
			if err != nil {
				logrus.Errorf("Getting new log line: %v", err)
				continue
			}
			nll.CID = c.ID()
			nll.CName = c.Name()
			nll.ColorID = colorID
			if nll.Since(options.Since) && nll.Until(options.Until) {
				logChannel <- nll
			}
		}
	}()
	// Check if container is still running or paused
	if options.Follow {
		// If the container isn't running or if we encountered an error
		// getting its state, instruct the logger to read the file
		// until EOF.
		state, err := c.State()
		if err != nil || state != define.ContainerStateRunning {
			if err != nil && !errors.Is(err, define.ErrNoSuchCtr) {
				logrus.Errorf("Getting container state: %v", err)
			}
			go func() {
				// Make sure to wait at least for the poll duration
				// before stopping the file logger (see #10675).
				time.Sleep(watch.POLL_DURATION)
				tailError := t.StopAtEOF()
				if tailError != nil && tailError.Error() != "tail: stop at eof" {
					logrus.Errorf("Stopping logger: %v", tailError)
				}
			}()
			return nil
		}

		// The container is running, so we need to wait until the container exited
		go func() {
			_, err = c.Wait(ctx)
			if err != nil && !errors.Is(err, define.ErrNoSuchCtr) {
				logrus.Errorf("Waiting for container to exit: %v", err)
			}
			// Make sure to wait at least for the poll duration
			// before stopping the file logger (see #10675).
			time.Sleep(watch.POLL_DURATION)
			tailError := t.StopAtEOF()
			if tailError != nil && tailError.Error() != "tail: stop at eof" {
				logrus.Errorf("Stopping logger: %v", tailError)
			}
		}()
	}
	return nil
}
