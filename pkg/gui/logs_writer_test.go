package gui

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimestampReformattingWriterReformatsCompleteLines(t *testing.T) {
	var dest bytes.Buffer
	writer := newTimestampReformattingWriter(&dest, "15:04:05.00")

	_, err := writer.Write([]byte("2026-11-17T20:30:14.160928256Z hello\n2026-11-17T20:30:15.987654321Z world\n"))
	assert.NoError(t, err)

	assert.Equal(t, "20:30:14.16 hello\n20:30:15.98 world\n", dest.String())
}

func TestTimestampReformattingWriterBuffersPartialLines(t *testing.T) {
	var dest bytes.Buffer
	writer := newTimestampReformattingWriter(&dest, "15:04:05.00")

	_, err := writer.Write([]byte("2026-11-17T20:30:14.160928256Z hel"))
	assert.NoError(t, err)
	assert.Empty(t, dest.String())

	_, err = writer.Write([]byte("lo\n2026-11-17T20:30:15.987654321Z wor"))
	assert.NoError(t, err)
	assert.Equal(t, "20:30:14.16 hello\n", dest.String())

	_, err = writer.Write([]byte("ld"))
	assert.NoError(t, err)
	assert.Equal(t, "20:30:14.16 hello\n", dest.String())

	assert.NoError(t, writer.Flush())
	assert.Equal(t, "20:30:14.16 hello\n20:30:15.98 world", dest.String())
}

func TestTimestampReformattingWriterPassesThroughUntimestampedLines(t *testing.T) {
	var dest bytes.Buffer
	writer := newTimestampReformattingWriter(&dest, "15:04:05.00")

	_, err := writer.Write([]byte("no timestamp here\n"))
	assert.NoError(t, err)

	assert.Equal(t, "no timestamp here\n", dest.String())
}

func TestTimestampReformattingWriterFlushWithoutBufferedDataIsNoop(t *testing.T) {
	var dest bytes.Buffer
	writer := newTimestampReformattingWriter(&dest, "15:04:05.00")

	assert.NoError(t, writer.Flush())
	assert.Empty(t, dest.String())
}
