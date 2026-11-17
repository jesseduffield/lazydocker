package gui

import (
	"bytes"
	"io"

	"github.com/jesseduffield/lazydocker/pkg/utils"
)

// timestampReformattingWriter buffers writes until a full line is available and
// then reformats the timestamp Docker prepended to that line. Log streams are
// delivered in arbitrary chunks, so lines can be split across writes.
type timestampReformattingWriter struct {
	dest   io.Writer
	layout string
	buffer []byte
}

func newTimestampReformattingWriter(dest io.Writer, layout string) *timestampReformattingWriter {
	return &timestampReformattingWriter{
		dest:   dest,
		layout: layout,
	}
}

func (w *timestampReformattingWriter) Write(p []byte) (int, error) {
	w.buffer = append(w.buffer, p...)

	for {
		newlineIndex := bytes.IndexByte(w.buffer, '\n')
		if newlineIndex < 0 {
			break
		}

		line := string(w.buffer[:newlineIndex+1])
		w.buffer = w.buffer[newlineIndex+1:]
		if err := w.writeLine(line); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

// Flush writes any buffered partial line that was never terminated by a newline.
func (w *timestampReformattingWriter) Flush() error {
	if len(w.buffer) == 0 {
		return nil
	}

	line := string(w.buffer)
	w.buffer = nil

	return w.writeLine(line)
}

func (w *timestampReformattingWriter) writeLine(line string) error {
	_, err := io.WriteString(w.dest, utils.ReformatLogTimestamp(line, w.layout))
	return err
}
