package utils

import (
	"strings"
	"time"
)

// ReformatLogTimestamp rewrites the RFC3339 timestamp that Docker prepends to a
// log line when timestamps are enabled, using the given Go time layout. Lines
// that do not start with a parseable timestamp (or an empty layout) are
// returned unchanged.
func ReformatLogTimestamp(line string, layout string) string {
	if layout == "" {
		return line
	}

	spaceIndex := strings.IndexByte(line, ' ')
	if spaceIndex <= 0 {
		return line
	}

	timestamp, err := time.Parse(time.RFC3339Nano, line[:spaceIndex])
	if err != nil {
		return line
	}

	return timestamp.Format(layout) + line[spaceIndex:]
}
