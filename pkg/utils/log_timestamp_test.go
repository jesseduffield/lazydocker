package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReformatLogTimestamp(t *testing.T) {
	scenarios := []struct {
		name     string
		line     string
		layout   string
		expected string
	}{
		{
			name:     "two decimal places truncates nanoseconds",
			line:     "2026-11-17T20:30:14.160928256Z [agentcore_app] hello",
			layout:   "15:04:05.00",
			expected: "20:30:14.16 [agentcore_app] hello",
		},
		{
			name:     "milliseconds layout",
			line:     "2026-11-17T20:30:14.160928256Z hello",
			layout:   "2006-01-02T15:04:05.000",
			expected: "2026-11-17T20:30:14.160 hello",
		},
		{
			name:     "no fractional seconds in input",
			line:     "2026-11-17T20:30:14Z hello",
			layout:   "15:04:05.00",
			expected: "20:30:14.00 hello",
		},
		{
			name:     "timezone offset is preserved",
			line:     "2026-11-17T20:30:14.160928256+02:00 hello",
			layout:   "15:04:05.00",
			expected: "20:30:14.16 hello",
		},
		{
			name:     "empty layout leaves the line unchanged",
			line:     "2026-11-17T20:30:14.160928256Z hello",
			layout:   "",
			expected: "2026-11-17T20:30:14.160928256Z hello",
		},
		{
			name:     "line without a timestamp is unchanged",
			line:     "hello world",
			layout:   "15:04:05.00",
			expected: "hello world",
		},
		{
			name:     "malformed timestamp is unchanged",
			line:     "2026-13-99T99:99:99Z hello",
			layout:   "15:04:05.00",
			expected: "2026-13-99T99:99:99Z hello",
		},
		{
			name:     "timestamp without trailing content is unchanged",
			line:     "2026-11-17T20:30:14.160928256Z",
			layout:   "15:04:05.00",
			expected: "2026-11-17T20:30:14.160928256Z",
		},
		{
			name:     "empty line is unchanged",
			line:     "",
			layout:   "15:04:05.00",
			expected: "",
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			assert.Equal(t, s.expected, ReformatLogTimestamp(s.line, s.layout))
		})
	}
}
