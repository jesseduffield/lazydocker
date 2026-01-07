package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.podman.io/storage/pkg/stringid"
)

// ErrNoJournaldLogging indicates that there is no journald logging
// supported (requires libsystemd)
var ErrNoJournaldLogging = errors.New("no support for journald logging")

// String returns a string representation of EventerType
func (et EventerType) String() string {
	return string(et)
}

// IsValidEventer checks if the given string is a valid eventer type.
func IsValidEventer(eventer string) bool {
	switch EventerType(eventer) {
	case LogFile, Journald, Null:
		return true
	default:
		return false
	}
}

// NewEvent creates an event struct and populates with
// the given status and time.
func NewEvent(status Status) Event {
	return Event{
		Status: status,
		Time:   time.Now(),
	}
}

// ToJSONString returns the event as a json'ified string
func (e *Event) ToJSONString() (string, error) {
	b, err := json.Marshal(e)
	return string(b), err
}

// ToHumanReadable returns human-readable event as a formatted string
func (e *Event) ToHumanReadable(truncate bool) string {
	if e == nil {
		return ""
	}
	var humanFormat string
	id := e.ID
	if truncate {
		id = stringid.TruncateID(id)
	}
	switch e.Type {
	case Container, Pod:
		humanFormat = fmt.Sprintf("%s %s %s %s (image=%s, name=%s", e.Time, e.Type, e.Status, id, e.Image, e.Name)
		if e.PodID != "" {
			humanFormat += fmt.Sprintf(", pod_id=%s", e.PodID)
		}
		if e.Status == HealthStatus {
			humanFormat += fmt.Sprintf(", health_status=%s", e.HealthStatus)
			humanFormat += fmt.Sprintf(", health_failing_streak=%d", e.HealthFailingStreak)
			humanFormat += fmt.Sprintf(", health_log=%s", e.HealthLog)
		}
		// check if the container has labels and add it to the output
		if len(e.Attributes) > 0 {
			for k, v := range e.Attributes {
				humanFormat += fmt.Sprintf(", %s=%s", k, v)
			}
		}
		humanFormat += ")"
	case Network:
		if e.Status == Create || e.Status == Remove {
			if netdriver, exists := e.Attributes["driver"]; exists {
				humanFormat = fmt.Sprintf("%s %s %s %s (name=%s, type=%s)", e.Time, e.Type, e.Status, e.ID, e.Network, netdriver)
			}
		} else {
			humanFormat = fmt.Sprintf("%s %s %s %s (container=%s, name=%s)", e.Time, e.Type, e.Status, id, id, e.Network)
		}
	case Image:
		humanFormat = fmt.Sprintf("%s %s %s %s %s", e.Time, e.Type, e.Status, id, e.Name)
		if e.Error != "" {
			humanFormat += " " + e.Error
		}
	case System:
		if e.Name != "" {
			humanFormat = fmt.Sprintf("%s %s %s %s", e.Time, e.Type, e.Status, e.Name)
		} else {
			humanFormat = fmt.Sprintf("%s %s %s", e.Time, e.Type, e.Status)
		}
	case Volume, Machine:
		humanFormat = fmt.Sprintf("%s %s %s %s", e.Time, e.Type, e.Status, e.Name)
	case Secret:
		humanFormat = fmt.Sprintf("%s %s %s %s", e.Time, e.Type, e.Status, id)
	}
	return humanFormat
}

// String converts a Type to a string
func (t Type) String() string {
	return string(t)
}

// String converts a status to a string
func (s Status) String() string {
	return string(s)
}

// StringToType converts string to an EventType
func StringToType(name string) (Type, error) {
	switch name {
	case Container.String():
		return Container, nil
	case Image.String():
		return Image, nil
	case Machine.String():
		return Machine, nil
	case Network.String():
		return Network, nil
	case Pod.String():
		return Pod, nil
	case System.String():
		return System, nil
	case Volume.String():
		return Volume, nil
	case Secret.String():
		return Secret, nil
	case "":
		return "", ErrEventTypeBlank
	}
	return "", fmt.Errorf("unknown event type %q", name)
}

// StringToStatus converts a string to an Event Status
func StringToStatus(name string) (Status, error) {
	switch name {
	case Attach.String():
		return Attach, nil
	case AutoUpdate.String():
		return AutoUpdate, nil
	case Build.String():
		return Build, nil
	case Checkpoint.String():
		return Checkpoint, nil
	case Cleanup.String():
		return Cleanup, nil
	case Commit.String():
		return Commit, nil
	case Create.String():
		return Create, nil
	case Exec.String():
		return Exec, nil
	case ExecDied.String():
		return ExecDied, nil
	case Exited.String():
		return Exited, nil
	case Export.String():
		return Export, nil
	case HealthStatus.String():
		return HealthStatus, nil
	case History.String():
		return History, nil
	case Import.String():
		return Import, nil
	case Init.String():
		return Init, nil
	case Kill.String():
		return Kill, nil
	case LoadFromArchive.String():
		return LoadFromArchive, nil
	case Mount.String():
		return Mount, nil
	case NetworkConnect.String():
		return NetworkConnect, nil
	case NetworkDisconnect.String():
		return NetworkDisconnect, nil
	case Pause.String():
		return Pause, nil
	case Prune.String():
		return Prune, nil
	case Pull.String():
		return Pull, nil
	case PullError.String():
		return PullError, nil
	case Push.String():
		return Push, nil
	case Refresh.String():
		return Refresh, nil
	case Remove.String():
		return Remove, nil
	case Rename.String():
		return Rename, nil
	case Renumber.String():
		return Renumber, nil
	case Restart.String():
		return Restart, nil
	case Restore.String():
		return Restore, nil
	case Rotate.String():
		return Rotate, nil
	case Save.String():
		return Save, nil
	case Start.String():
		return Start, nil
	case Stop.String():
		return Stop, nil
	case Sync.String():
		return Sync, nil
	case Tag.String():
		return Tag, nil
	case Unmount.String():
		return Unmount, nil
	case Unpause.String():
		return Unpause, nil
	case Untag.String():
		return Untag, nil
	case Update.String():
		return Update, nil
	}
	return "", fmt.Errorf("unknown event status %q", name)
}
