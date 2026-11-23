package utils

import (
	"strings"
	"testing"
)

func TestColoriseLog(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "Highlight error log level",
			input:    "ERROR: database failed to connect",
			contains: []string{"\x1b[31;1mERROR\x1b[0m"},
		},
		{
			name:     "Highlight warning log level",
			input:    "[WARN] deprecation warning",
			contains: []string{"\x1b[33;1mWARN\x1b[0m"},
		},
		{
			name:     "Highlight timestamp",
			input:    "2026-06-04T12:00:00Z something happened",
			contains: []string{"\x1b[90m2026-06-04T12:00:00Z\x1b[0m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ColoriseLog(tt.input)
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("expected %q to contain %q, but it was %q", got, c, got)
				}
			}
		})
	}
}
