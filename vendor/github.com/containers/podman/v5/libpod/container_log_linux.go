//go:build !remote && linux && systemd

package libpod

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/libpod/logs"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/sirupsen/logrus"
)

const (
	// journaldLogOut is the journald priority signifying stdout
	journaldLogOut = "6"

	// journaldLogErr is the journald priority signifying stderr
	journaldLogErr = "3"
)

func init() {
	logDrivers = append(logDrivers, define.JournaldLogging)
}

func (c *Container) readFromJournal(ctx context.Context, options *logs.LogOptions,
	logChannel chan *logs.LogLine, colorID int64, passthroughUnit string) error {
	// We need the container's events in the same journal to guarantee
	// consistency, see #10323.
	if options.Follow && c.runtime.config.Engine.EventsLogger != "journald" {
		return fmt.Errorf("using --follow with the journald --log-driver but without the journald --events-backend (%s) is not supported", c.runtime.config.Engine.EventsLogger)
	}

	journal, err := sdjournal.NewJournal()
	if err != nil {
		return err
	}
	// While logs are written to the `logChannel`, we inspect each event
	// and stop once the container has died.  Having logs and events in one
	// stream prevents a race condition that we faced in #10323.

	// Add the filters for events.
	match := sdjournal.Match{Field: "SYSLOG_IDENTIFIER", Value: "podman"}
	if err := journal.AddMatch(match.String()); err != nil {
		return fmt.Errorf("adding filter to journald logger: %v: %w", match, err)
	}
	match = sdjournal.Match{Field: "PODMAN_ID", Value: c.ID()}
	if err := journal.AddMatch(match.String()); err != nil {
		return fmt.Errorf("adding filter to journald logger: %v: %w", match, err)
	}
	// Make sure we only read events for the current user, while it is unlikely that there
	// is a container ID duplication for two users, it is better to have it just in case.
	uidMatch := sdjournal.Match{Field: "_UID", Value: strconv.Itoa(rootless.GetRootlessUID())}
	if err := journal.AddMatch(uidMatch.String()); err != nil {
		return fmt.Errorf("adding filter to journald logger: %v: %w", uidMatch, err)
	}

	// Add the filter for logs.  Note the disjunction so that we match
	// either the events or the logs.
	if err := journal.AddDisjunction(); err != nil {
		return fmt.Errorf("adding filter disjunction to journald logger: %w", err)
	}

	if passthroughUnit != "" {
		// Match based on systemd unit which is the container is cgroup
		// so we get the exact logs for a single container even in the
		// play kube case where a single unit starts more than one container.
		unitTypeName := "_SYSTEMD_UNIT"
		if rootless.IsRootless() {
			unitTypeName = "_SYSTEMD_USER_UNIT"
		}
		// By default we will have our own systemd cgroup with the name libpod-<ID>.scope.
		value := "libpod-" + c.ID() + ".scope"
		if c.config.CgroupsMode == cgroupSplit {
			// If cgroup split the container runs in the unit cgroup so we use this for logs,
			// the good thing is we filter the podman events already out below.
			// Thus we are left with the real container log and possibly podman output (e.g. logrus).
			value = passthroughUnit
		}

		match = sdjournal.Match{Field: unitTypeName, Value: value}
		if err := journal.AddMatch(match.String()); err != nil {
			return fmt.Errorf("adding filter to journald logger: %v: %w", match, err)
		}
	} else {
		match = sdjournal.Match{Field: "CONTAINER_ID_FULL", Value: c.ID()}
		if err := journal.AddMatch(match.String()); err != nil {
			return fmt.Errorf("adding filter to journald logger: %v: %w", match, err)
		}
	}

	if err := journal.AddMatch(uidMatch.String()); err != nil {
		return fmt.Errorf("adding filter to journald logger: %v: %w", uidMatch, err)
	}

	if options.Since.IsZero() {
		if err := journal.SeekHead(); err != nil {
			return err
		}
	} else {
		// seek based on time which helps to reduce unnecessary event reads
		if err := journal.SeekRealtimeUsec(uint64(options.Since.UnixMicro())); err != nil {
			return err
		}
	}

	c.lock.Lock()
	if err := c.syncContainer(); err != nil {
		c.lock.Unlock()
		return err
	}
	// The initial "containerCouldBeLogging" state must be correct, we cannot rely on the start event being still in the journal.
	// This can happen if the journal was rotated after the container was started or when --since is used.
	// https://github.com/containers/podman/issues/16950
	containerCouldBeLogging := c.ensureState(define.ContainerStateRunning, define.ContainerStateStopping)
	c.lock.Unlock()

	options.WaitGroup.Add(1)
	go func() {
		defer func() {
			options.WaitGroup.Done()
			if err := journal.Close(); err != nil {
				logrus.Errorf("Unable to close journal: %v", err)
			}
		}()

		tailQueue := []*logs.LogLine{} // needed for options.Tail
		doTail := options.Tail >= 0
		doTailFunc := func() {
			// Flush *once* we hit the end of the journal.
			startIndex := int64(len(tailQueue))
			outputLines := int64(0)
			for startIndex > 0 && outputLines < options.Tail {
				startIndex--
				for startIndex > 0 && tailQueue[startIndex].Partial() {
					startIndex--
				}
				outputLines++
			}
			for i := startIndex; i < int64(len(tailQueue)); i++ {
				logChannel <- tailQueue[i]
			}
			tailQueue = nil
			doTail = false
		}
		for {
			entry, err := events.GetNextEntry(ctx, journal, !doTail && options.Follow && containerCouldBeLogging, options.Until)
			if err != nil {
				logrus.Errorf("Failed to get journal entry: %v", err)
				return
			}
			// entry nil == EOF in journal
			if entry == nil {
				if doTail {
					doTailFunc()
					continue
				}
				return
			}

			entryTime := time.Unix(0, int64(entry.RealtimeTimestamp)*int64(time.Microsecond))
			if (entryTime.Before(options.Since) && !options.Since.IsZero()) || (entryTime.After(options.Until) && !options.Until.IsZero()) {
				continue
			}
			// If we're reading an event and the container exited/died,
			// then we're done and can return.
			event, ok := entry.Fields["PODMAN_EVENT"]
			if ok {
				status, err := events.StringToStatus(event)
				if err != nil {
					logrus.Errorf("Failed to translate event: %v", err)
					return
				}
				switch status {
				case events.History, events.Init, events.Start, events.Restart:
					containerCouldBeLogging = true
				case events.Exited:
					containerCouldBeLogging = false
				}
				continue
			}

			logLine, err := journalToLogLine(entry)
			if err != nil {
				logrus.Errorf("Failed parse journal entry: %v", err)
				return
			}
			id := c.ID()
			if len(id) > 12 {
				id = id[:12]
			}
			logLine.CID = id
			logLine.ColorID = colorID
			if options.UseName {
				logLine.CName = c.Name()
			}
			if doTail {
				tailQueue = append(tailQueue, logLine)
				continue
			}
			logChannel <- logLine
		}
	}()

	return nil
}

func journalToLogLine(entry *sdjournal.JournalEntry) (*logs.LogLine, error) {
	line := &logs.LogLine{}

	usec := entry.RealtimeTimestamp
	line.Time = time.Unix(0, int64(usec)*int64(time.Microsecond))

	priority, ok := entry.Fields["PRIORITY"]
	if !ok {
		return nil, errors.New("no PRIORITY field present in journal entry")
	}
	switch priority {
	case journaldLogOut:
		line.Device = "stdout"
	case journaldLogErr:
		line.Device = "stderr"
	default:
		return nil, errors.New("unexpected PRIORITY field in journal entry")
	}

	// if CONTAINER_PARTIAL_MESSAGE is defined, the log type is "P"
	if _, ok := entry.Fields["CONTAINER_PARTIAL_MESSAGE"]; ok {
		line.ParseLogType = logs.PartialLogType
	} else {
		line.ParseLogType = logs.FullLogType
	}

	line.Msg, ok = entry.Fields["MESSAGE"]
	if !ok {
		return nil, errors.New("no MESSAGE field present in journal entry")
	}
	line.Msg = strings.TrimSuffix(line.Msg, "\n")

	return line, nil
}
