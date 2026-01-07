//go:build systemd

package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/coreos/go-systemd/v22/journal"
	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/sirupsen/logrus"
)

// EventJournalD is the journald implementation of an eventer
type EventJournalD struct {
	options EventerOptions
}

// newJournalDEventer creates a new EventJournalD Eventer
func newJournalDEventer(options EventerOptions) (Eventer, error) {
	return EventJournalD{options}, nil
}

// Write to journald
func (e EventJournalD) Write(ee Event) error {
	m := make(map[string]string)
	m["SYSLOG_IDENTIFIER"] = "podman"
	m["PODMAN_EVENT"] = ee.Status.String()
	m["PODMAN_TYPE"] = ee.Type.String()
	m["PODMAN_TIME"] = ee.Time.Format(time.RFC3339Nano)

	// Add specialized information based on the podman type
	switch ee.Type {
	case Image:
		m["PODMAN_NAME"] = ee.Name
		m["PODMAN_ID"] = ee.ID
		if ee.Error != "" {
			m["ERROR"] = ee.Error
		}
	case Container, Pod:
		m["PODMAN_IMAGE"] = ee.Image
		m["PODMAN_NAME"] = ee.Name
		m["PODMAN_ID"] = ee.ID
		if ee.ContainerExitCode != nil {
			m["PODMAN_EXIT_CODE"] = strconv.Itoa(*ee.ContainerExitCode)
		}
		if ee.PodID != "" {
			m["PODMAN_POD_ID"] = ee.PodID
		}
		if err := addLabelsToJournal(m, ee.Details.Attributes); err != nil {
			return err
		}
		if ee.Status == HealthStatus {
			m["PODMAN_HEALTH_STATUS"] = ee.HealthStatus
			if ee.HealthLog != "" {
				m["PODMAN_HEALTH_LOG"] = ee.HealthLog
			}
			m["PODMAN_HEALTH_FAILING_STREAK"] = strconv.Itoa(ee.HealthFailingStreak)
		}
		if len(ee.Details.ContainerInspectData) > 0 {
			m["PODMAN_CONTAINER_INSPECT_DATA"] = ee.Details.ContainerInspectData
		}
	case Network:
		m["PODMAN_ID"] = ee.ID
		m["PODMAN_NETWORK_NAME"] = ee.Network
		if err := addLabelsToJournal(m, ee.Details.Attributes); err != nil {
			return err
		}
	case Volume:
		m["PODMAN_NAME"] = ee.Name
	}

	// starting with commit 7e6e267329 we set LogLevel=notice for the systemd healthcheck unit
	// This so it doesn't log the started/stopped unit messages al the time which spam the
	// journal if a small interval is used. That however broke the healthcheck event as it no
	// longer showed up in podman events when running as root as we only send the event on info
	// level. To fix this we have to send the event on notice level.
	// https://github.com/containers/podman/issues/20342
	prio := journal.PriInfo
	if len(ee.HealthStatus) > 0 {
		prio = journal.PriNotice
	}

	return journal.Send(ee.ToHumanReadable(false), prio, m)
}

func addLabelsToJournal(journalEntry, eventAttributes map[string]string) error {
	// If we have container labels, we need to convert them to a string so they
	// can be recorded with the event
	if len(eventAttributes) > 0 {
		b, err := json.Marshal(eventAttributes)
		if err != nil {
			return err
		}
		journalEntry["PODMAN_LABELS"] = string(b)
	}
	return nil
}

func getLabelsFromJournal(entry *sdjournal.JournalEntry, event *Event) error {
	// we need to check for the presence of labels recorded to a container event
	if stringLabels, ok := entry.Fields["PODMAN_LABELS"]; ok && len(stringLabels) > 0 {
		labels := make(map[string]string, 0)
		if err := json.Unmarshal([]byte(stringLabels), &labels); err != nil {
			return err
		}

		// if we have labels, add them to the event
		if len(labels) > 0 {
			event.Attributes = labels
		}
	}
	return nil
}

// Read reads events from the journal and sends qualified events to the event channel
func (e EventJournalD) Read(ctx context.Context, options ReadOptions) (retErr error) {
	filterMap, err := generateEventFilters(options.Filters, options.Since, options.Until)
	if err != nil {
		return fmt.Errorf("failed to parse event filters: %w", err)
	}

	var untilTime time.Time
	if len(options.Until) > 0 {
		untilTime, err = util.ParseInputTime(options.Until, false)
		if err != nil {
			return err
		}
	}

	j, err := sdjournal.NewJournal()
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			if err := j.Close(); err != nil {
				logrus.Errorf("Unable to close journal :%v", err)
			}
		}
	}()
	err = j.SetDataThreshold(0)
	if err != nil {
		return fmt.Errorf("cannot set data threshold for journal: %v", err)
	}
	// match only podman journal entries
	podmanJournal := sdjournal.Match{Field: "SYSLOG_IDENTIFIER", Value: "podman"}
	if err := j.AddMatch(podmanJournal.String()); err != nil {
		return fmt.Errorf("failed to add SYSLOG_IDENTIFIER journal filter for event log: %w", err)
	}

	// make sure we only read events for the current user
	uidMatch := sdjournal.Match{Field: "_UID", Value: strconv.Itoa(rootless.GetRootlessUID())}
	if err := j.AddMatch(uidMatch.String()); err != nil {
		return fmt.Errorf("failed to add _UID journal filter for event log: %w", err)
	}

	if len(options.Since) == 0 && len(options.Until) == 0 && options.Stream {
		if err := j.SeekTail(); err != nil {
			return fmt.Errorf("failed to seek end of journal: %w", err)
		}
		// After SeekTail calling Next moves to a random entry.
		// To prevent this we have to call Previous first.
		// see: https://bugs.freedesktop.org/show_bug.cgi?id=64614
		if _, err := j.Previous(); err != nil {
			return fmt.Errorf("failed to move journal cursor to previous entry: %w", err)
		}
	} else if len(options.Since) > 0 {
		since, err := util.ParseInputTime(options.Since, true)
		if err != nil {
			return err
		}
		// seek based on time which helps to reduce unnecessary event reads
		if err := j.SeekRealtimeUsec(uint64(since.UnixMicro())); err != nil {
			return err
		}
	}

	go func() {
		defer close(options.EventChannel)
		defer func() {
			if err := j.Close(); err != nil {
				logrus.Errorf("Unable to close journal :%v", err)
			}
		}()
		for {
			entry, err := GetNextEntry(ctx, j, options.Stream, untilTime)
			if err != nil {
				options.EventChannel <- ReadResult{Error: err}
				break
			}
			// no entry == we hit the end
			if entry == nil {
				break
			}

			newEvent, err := newEventFromJournalEntry(entry)
			if err != nil {
				// We can't decode this event.
				// Don't fail hard - that would make events unusable.
				// Instead, log and continue.
				if !errors.Is(err, ErrEventTypeBlank) {
					options.EventChannel <- ReadResult{Error: fmt.Errorf("unable to decode event: %v", err)}
				}
				continue
			}
			if applyFilters(newEvent, filterMap) {
				options.EventChannel <- ReadResult{Event: newEvent}
			}
		}
	}()
	return nil
}

func newEventFromJournalEntry(entry *sdjournal.JournalEntry) (*Event, error) {
	newEvent := Event{}
	eventType, err := StringToType(entry.Fields["PODMAN_TYPE"])
	if err != nil {
		return nil, err
	}
	eventTime, err := time.Parse(time.RFC3339Nano, entry.Fields["PODMAN_TIME"])
	if err != nil {
		return nil, err
	}
	eventStatus, err := StringToStatus(entry.Fields["PODMAN_EVENT"])
	if err != nil {
		return nil, err
	}
	newEvent.Type = eventType
	newEvent.Time = eventTime
	newEvent.Status = eventStatus
	newEvent.Name = entry.Fields["PODMAN_NAME"]

	switch eventType {
	case Container, Pod:
		newEvent.ID = entry.Fields["PODMAN_ID"]
		newEvent.Image = entry.Fields["PODMAN_IMAGE"]
		newEvent.PodID = entry.Fields["PODMAN_POD_ID"]
		if code, ok := entry.Fields["PODMAN_EXIT_CODE"]; ok {
			intCode, err := strconv.Atoi(code)
			if err != nil {
				logrus.Errorf("Parsing event exit code %s", code)
			} else {
				newEvent.ContainerExitCode = &intCode
			}
		}
		if err := getLabelsFromJournal(entry, &newEvent); err != nil {
			return nil, err
		}
		newEvent.HealthStatus = entry.Fields["PODMAN_HEALTH_STATUS"]
		if log, ok := entry.Fields["PODMAN_HEALTH_LOG"]; ok {
			newEvent.HealthLog = log
		}
		if FailingStreak, ok := entry.Fields["PODMAN_HEALTH_FAILING_STREAK"]; ok {
			FailingStreakInt, err := strconv.Atoi(FailingStreak)
			if err == nil {
				newEvent.HealthFailingStreak = FailingStreakInt
			}
		}
		newEvent.Details.ContainerInspectData = entry.Fields["PODMAN_CONTAINER_INSPECT_DATA"]
	case Network:
		newEvent.ID = entry.Fields["PODMAN_ID"]
		newEvent.Network = entry.Fields["PODMAN_NETWORK_NAME"]
		if err := getLabelsFromJournal(entry, &newEvent); err != nil {
			return nil, err
		}
	case Image:
		newEvent.ID = entry.Fields["PODMAN_ID"]
		if val, ok := entry.Fields["ERROR"]; ok {
			newEvent.Error = val
		}
	}
	return &newEvent, nil
}

// String returns a string representation of the logger
func (e EventJournalD) String() string {
	return Journald.String()
}

// GetNextEntry returns the next entry in the journal. If the end of the
// journal is reached and stream is not set or the current time is after
// the until time this function returns nil,nil.
func GetNextEntry(ctx context.Context, j *sdjournal.Journal, stream bool, untilTime time.Time) (*sdjournal.JournalEntry, error) {
	for {
		select {
		case <-ctx.Done():
			// the consumer has cancelled
			return nil, nil
		default:
			// fallthrough
		}
		// the api requires a next|prev before reading the event
		ret, err := j.Next()
		if err != nil {
			return nil, fmt.Errorf("failed to move journal cursor to next entry: %w", err)
		}
		// ret == 0 equals EOF, see sd_journal_next(3)
		if ret == 0 {
			if !stream || (!untilTime.IsZero() && time.Now().After(untilTime)) {
				// we hit the end and should not keep streaming
				return nil, nil
			}
			// keep waiting for the next entry
			// j.Wait() is blocking, this would cause the goroutine to hang forever
			// if no more journal entries are generated and thus if the client
			// has closed the connection in the meantime to leak memory.
			// Waiting only 5 seconds makes sure we can check if the client closed in the
			// meantime at least every 5 seconds.
			t := 5 * time.Second
			if !untilTime.IsZero() {
				until := time.Until(untilTime)
				if until < t {
					t = until
				}
			}
			_ = j.Wait(t)
			continue
		}

		entry, err := j.GetEntry()
		if err != nil {
			return nil, fmt.Errorf("failed to read journal entry: %w", err)
		}
		return entry, nil
	}
}
