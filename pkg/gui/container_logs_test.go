package gui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamFilterLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		filter   string
		expected string
	}{
		{
			name:     "simple match - single line",
			input:    "hello world\n",
			filter:   "hello",
			expected: "hello world\n",
		},
		{
			name:     "simple match - multiple lines",
			input:    "hello world\nfoo bar\nbaz qux\n",
			filter:   "foo",
			expected: "foo bar\n",
		},
		{
			name:     "multiple matches",
			input:    "hello world\nhello again\nfoo bar\n",
			filter:   "hello",
			expected: "hello world\nhello again\n",
		},
		{
			name:     "no matches",
			input:    "hello world\nfoo bar\nbaz qux\n",
			filter:   "xyz",
			expected: "",
		},
		{
			name:     "empty filter - matches all",
			input:    "hello world\nfoo bar\n",
			filter:   "",
			expected: "hello world\nfoo bar\n",
		},
		{
			name:     "case sensitive match",
			input:    "Hello World\nhello world\nHELLO WORLD\n",
			filter:   "hello",
			expected: "hello world\n",
		},
		{
			name:     "partial word match",
			input:    "hello world\nhelloworld\n",
			filter:   "hello",
			expected: "hello world\nhelloworld\n",
		},
		{
			name:     "filter in middle of line",
			input:    "prefix hello suffix\n",
			filter:   "hello",
			expected: "prefix hello suffix\n",
		},
		{
			name:     "empty input",
			input:    "",
			filter:   "hello",
			expected: "",
		},
		{
			name:     "newline only lines",
			input:    "\n\n\n",
			filter:   "",
			expected: "\n\n\n",
		},
		{
			name:     "filter with special characters",
			input:    "error: something went wrong\ninfo: all good\n",
			filter:   "error:",
			expected: "error: something went wrong\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			writer := &bytes.Buffer{}
			ctx := context.Background()

			err := streamFilterLines(reader, writer, tt.filter, ctx)

			assert.NoError(t, err)
			assert.Equal(t, tt.expected, writer.String())
		})
	}
}

func TestStreamFilterLines_ContextCancellation(t *testing.T) {
	input := strings.Repeat("hello world\n", 1000)
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}
	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	err := streamFilterLines(reader, writer, "hello", ctx)

	assert.NoError(t, err)
}

func TestStreamFilterLines_NonASCIIFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		filter   string
		expected string
	}{
		{
			name:     "UTF-8 characters",
			input:    "hello 世界\nfoo bar\n",
			filter:   "世界",
			expected: "hello 世界\n",
		},
		{
			name:     "emoji filter",
			input:    "error 🚨 occurred\ninfo: all good\n",
			filter:   "🚨",
			expected: "error 🚨 occurred\n",
		},
		{
			name:     "mixed ASCII and UTF-8",
			input:    "hello 世界 world\nfoo bar\n",
			filter:   "世界",
			expected: "hello 世界 world\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			writer := &bytes.Buffer{}
			ctx := context.Background()

			err := streamFilterLines(reader, writer, tt.filter, ctx)

			assert.NoError(t, err)
			assert.Equal(t, tt.expected, writer.String())
		})
	}
}

func TestStreamFilterLines_LongLines(t *testing.T) {
	longLine := strings.Repeat("a", 100000) + " target " + strings.Repeat("b", 100000) + "\n"
	shortLine := "target\n"

	input := longLine + shortLine
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}
	ctx := context.Background()

	err := streamFilterLines(reader, writer, "target", ctx)

	assert.NoError(t, err)
	assert.Contains(t, writer.String(), "target")
	assert.Equal(t, 2, strings.Count(writer.String(), "target"))
}

func TestStreamFilterLines_EmptyLines(t *testing.T) {
	input := "line with content\n\nanother line\n\n\n"
	reader := strings.NewReader(input)
	writer := &bytes.Buffer{}
	ctx := context.Background()

	err := streamFilterLines(reader, writer, "", ctx)

	assert.NoError(t, err)
	lines := strings.Split(writer.String(), "\n")
	assert.GreaterOrEqual(t, len(lines), 5)
}
