package logs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/containers/podman/v5/libpod/logs/reversereader"
	"github.com/nxadm/tail"
	"github.com/sirupsen/logrus"
)

const (
	// LogTimeFormat is the time format used in the log.
	// It is a modified version of RFC3339Nano that guarantees trailing
	// zeroes are not trimmed, taken from
	// https://github.com/golang/go/issues/19635
	LogTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

	// PartialLogType signifies a log line that exceeded the buffer
	// length and needed to spill into a new line
	PartialLogType = "P"

	// FullLogType signifies a log line is full
	FullLogType = "F"

	// ANSIEscapeResetCode is a code that resets all colors and text effects
	ANSIEscapeResetCode = "\033[0m"
)

// LogOptions is the options you can use for logs
type LogOptions struct {
	Details    bool
	Follow     bool
	Since      time.Time
	Until      time.Time
	Tail       int64
	Timestamps bool
	Colors     bool
	Multi      bool
	WaitGroup  *sync.WaitGroup
	UseName    bool
}

// LogLine describes the information for each line of a log
type LogLine struct {
	Device       string
	ParseLogType string
	Time         time.Time
	Msg          string
	CID          string
	CName        string
	ColorID      int64
}

// GetLogFile returns an hp tail for a container given options
func GetLogFile(path string, options *LogOptions) (*tail.Tail, []*LogLine, error) {
	var (
		whence  int
		err     error
		logTail []*LogLine
	)
	// whence 0=origin, 2=end
	if options.Tail >= 0 {
		whence = 2
	}
	if options.Tail > 0 {
		logTail, err = getTailLog(path, int(options.Tail))
		if err != nil {
			return nil, nil, err
		}
	}
	seek := tail.SeekInfo{
		Offset: 0,
		Whence: whence,
	}

	t, err := tail.TailFile(path, tail.Config{MustExist: true, Poll: true, Follow: options.Follow, Location: &seek, Logger: tail.DiscardingLogger, ReOpen: options.Follow})
	return t, logTail, err
}

func getTailLog(path string, tail int) ([]*LogLine, error) {
	var (
		nllCounter int
		leftover   string
		tailLog    []*LogLine
		eof        bool
	)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rr, err := reversereader.NewReverseReader(f)
	if err != nil {
		return nil, err
	}

	first := true

	for {
		s, err := rr.Read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("reverse log read: %w", err)
			}
			eof = true
		}

		lines := strings.Split(s+leftover, "\n")
		// we read a chunk of data, so make sure to read the line in inverse order
		for i := len(lines) - 1; i > 0; i-- {
			// ignore empty lines
			if lines[i] == "" {
				continue
			}
			nll, err := NewLogLine(lines[i])
			if err != nil {
				return nil, err
			}
			if !nll.Partial() || first {
				nllCounter++
				// Even if the last line is partial we need to count it as it will be printed as line.
				// Because we read backwards the first line we read is the last line in the log.
				first = false
			}
			// We explicitly need to check for more lines than tail because we have
			// to read to next full line and must keep all partial lines
			// https://github.com/containers/podman/issues/19545
			if nllCounter > tail {
				// because we add lines in the inverse order we must invert the slice in the end
				return reverseLog(tailLog), nil
			}
			// only append after the return here because we do not want to include the next full line
			tailLog = append(tailLog, nll)
		}
		leftover = lines[0]

		// eof was reached
		if eof {
			// when we have still a line and do not have enough tail lines already
			if leftover != "" && nllCounter < tail {
				nll, err := NewLogLine(leftover)
				if err != nil {
					return nil, err
				}
				tailLog = append(tailLog, nll)
			}
			// because we add lines in the inverse order we must invert the slice in the end
			return reverseLog(tailLog), nil
		}
	}
}

// reverseLog reverse the log line slice, needed for tail as we read lines backwards but still
// need to print them in the correct order at the end  so use that helper for it.
func reverseLog(s []*LogLine) []*LogLine {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// getColor returns an ANSI escape code for color based on the colorID
func getColor(colorID int64) string {
	colors := map[int64]string{
		0: "\033[37m", // Light Gray
		1: "\033[31m", // Red
		2: "\033[33m", // Yellow
		3: "\033[34m", // Blue
		4: "\033[35m", // Magenta
		5: "\033[36m", // Cyan
		6: "\033[32m", // Green
	}
	return colors[colorID%int64(len(colors))]
}

func (l *LogLine) colorize(prefix string) string {
	return getColor(l.ColorID) + prefix + l.Msg + ANSIEscapeResetCode
}

// String converts a log line to a string for output given whether a detail
// bool is specified.
func (l *LogLine) String(options *LogOptions) string {
	var out string
	if options.Multi {
		if options.UseName {
			out = l.CName + " "
		} else {
			cid := l.CID
			if len(cid) > 12 {
				cid = cid[:12]
			}
			out = fmt.Sprintf("%s ", cid)
		}
	}

	if options.Timestamps {
		out += fmt.Sprintf("%s ", l.Time.Format(LogTimeFormat))
	}

	if options.Colors {
		out = l.colorize(out)
	} else {
		out += l.Msg
	}

	return out
}

// Since returns a bool as to whether a log line occurred after a given time
func (l *LogLine) Since(since time.Time) bool {
	return l.Time.After(since) || since.IsZero()
}

// Until returns a bool as to whether a log line occurred before a given time
func (l *LogLine) Until(until time.Time) bool {
	return l.Time.Before(until) || until.IsZero()
}

// NewLogLine creates a logLine struct from a container log string
func NewLogLine(line string) (*LogLine, error) {
	splitLine := strings.Split(line, " ")
	if len(splitLine) < 4 {
		return nil, fmt.Errorf("'%s' is not a valid container log line", line)
	}
	logTime, err := time.Parse(LogTimeFormat, splitLine[0])
	if err != nil {
		return nil, fmt.Errorf("unable to convert time %s from container log: %w", splitLine[0], err)
	}
	l := LogLine{
		Time:         logTime,
		Device:       splitLine[1],
		ParseLogType: splitLine[2],
		Msg:          strings.Join(splitLine[3:], " "),
	}
	return &l, nil
}

// Partial returns a bool if the log line is a partial log type
func (l *LogLine) Partial() bool {
	return l.ParseLogType == PartialLogType
}

func (l *LogLine) Write(stdout io.Writer, stderr io.Writer, logOpts *LogOptions) {
	switch l.Device {
	case "stdout":
		if stdout != nil {
			if l.Partial() {
				fmt.Fprint(stdout, l.String(logOpts))
			} else {
				fmt.Fprintln(stdout, l.String(logOpts))
			}
		}
	case "stderr":
		if stderr != nil {
			if l.Partial() {
				fmt.Fprint(stderr, l.String(logOpts))
			} else {
				fmt.Fprintln(stderr, l.String(logOpts))
			}
		}
	default:
		// Warn the user if the device type does not match. Most likely the file is corrupted.
		logrus.Warnf("Unknown Device type '%s' in log file from Container %s", l.Device, l.CID)
	}
}
