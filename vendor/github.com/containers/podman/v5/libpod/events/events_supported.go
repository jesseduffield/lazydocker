//go:build linux || freebsd

package events

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

// NewEventer creates an eventer based on the eventer type
func NewEventer(options EventerOptions) (Eventer, error) {
	logrus.Debugf("Initializing event backend %s", options.EventerType)
	switch EventerType(strings.ToLower(options.EventerType)) {
	case Journald:
		return newJournalDEventer(options)
	case LogFile:
		return newLogFileEventer(options)
	case Null:
		return newNullEventer(), nil
	default:
		return nil, fmt.Errorf("unknown event logger type: %s", strings.ToLower(options.EventerType))
	}
}

// newEventFromJSONString takes stringified json and converts
// it to an event
func newEventFromJSONString(event string) (*Event, error) {
	e := new(Event)
	if err := json.Unmarshal([]byte(event), e); err != nil {
		return nil, err
	}
	return e, nil
}

// newNullEventer returns a new null eventer.  You should only do this for
// the purposes of internal libpod testing.
func newNullEventer() Eventer {
	return EventToNull{}
}
