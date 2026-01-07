//go:build linux || freebsd

package events

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/containers/podman/v5/pkg/util"
	"github.com/nxadm/tail"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/lockfile"
	"golang.org/x/sys/unix"
)

// EventLogFile is the structure for event writing to a logfile. It contains the eventer
// options and the event itself.  Methods for reading and writing are also defined from it.
type EventLogFile struct {
	options EventerOptions
}

// newLogFileEventer creates a new EventLogFile eventer
func newLogFileEventer(options EventerOptions) (*EventLogFile, error) {
	// Create events log dir
	if err := os.MkdirAll(filepath.Dir(options.LogFilePath), 0o700); err != nil {
		return nil, fmt.Errorf("creating events dirs: %w", err)
	}
	// We have to make sure the file is created otherwise reading events will hang.
	// https://github.com/containers/podman/issues/15688
	fd, err := os.OpenFile(options.LogFilePath, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to create event log file: %w", err)
	}
	return &EventLogFile{options: options}, fd.Close()
}

// Writes to the log file
func (e EventLogFile) Write(ee Event) error {
	// We need to lock events file
	lock, err := lockfile.GetLockFile(e.options.LogFilePath + ".lock")
	if err != nil {
		return err
	}
	lock.Lock()
	defer lock.Unlock()

	eventJSONString, err := ee.ToJSONString()
	if err != nil {
		return err
	}

	if _, err := rotateLog(e.options.LogFilePath, eventJSONString, e.options.LogFileMaxSize); err != nil {
		return err
	}

	return e.writeString(eventJSONString)
}

func (e EventLogFile) writeString(s string) error {
	f, err := os.OpenFile(e.options.LogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeToFile(s, f)
}

func writeToFile(s string, f *os.File) error {
	_, err := f.WriteString(s + "\n")
	return err
}

func (e EventLogFile) getTail(options ReadOptions) (*tail.Tail, error) {
	seek := tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}
	if options.FromStart || !options.Stream {
		seek.Whence = 0
	}
	stream := options.Stream
	return tail.TailFile(e.options.LogFilePath, tail.Config{ReOpen: stream, Follow: stream, Location: &seek, Logger: tail.DiscardingLogger, Poll: true})
}

func (e EventLogFile) readRotateEvent(event *Event) (begin bool, end bool, err error) {
	if event.Status != Rotate {
		return
	}
	if event.Details.Attributes == nil {
		// may be an old event before storing attributes in the rotate event
		return
	}
	switch event.Details.Attributes[rotateEventAttribute] {
	case rotateEventBegin:
		begin = true
		return
	case rotateEventEnd:
		end = true
		return
	default:
		err = fmt.Errorf("unknown rotate-event attribute %q", event.Details.Attributes[rotateEventAttribute])
		return
	}
}

// Reads from the log file
func (e EventLogFile) Read(ctx context.Context, options ReadOptions) error {
	filterMap, err := generateEventFilters(options.Filters, options.Since, options.Until)
	if err != nil {
		return fmt.Errorf("failed to parse event filters: %w", err)
	}
	t, err := e.getTail(options)
	if err != nil {
		return err
	}
	if len(options.Until) > 0 {
		untilTime, err := util.ParseInputTime(options.Until, false)
		if err != nil {
			return err
		}
		go func() {
			time.Sleep(time.Until(untilTime))
			if err := t.Stop(); err != nil {
				logrus.Errorf("Stopping logger: %v", err)
			}
		}()
	}
	logrus.Debugf("Reading events from file %q", e.options.LogFilePath)

	// Get the time *before* starting to read.  Comparing the timestamps
	// with events avoids returning events more than once after a log-file
	// rotation.
	readTime, err := func() (time.Time, error) {
		// We need to lock events file
		lock, err := lockfile.GetLockFile(e.options.LogFilePath + ".lock")
		if err != nil {
			return time.Time{}, err
		}
		lock.Lock()
		defer lock.Unlock()
		return time.Now(), nil
	}()
	if err != nil {
		return err
	}

	go func() {
		defer close(options.EventChannel)
		var line *tail.Line
		var ok bool
		var skipRotate bool
		for {
			select {
			case <-ctx.Done():
				// the consumer has cancelled
				t.Kill(errors.New("hangup by client"))
				return
			case line, ok = <-t.Lines:
				if !ok {
					// channel was closed
					return
				}
				// fallthrough
			}

			event, err := newEventFromJSONString(line.Text)
			if err != nil {
				err := fmt.Errorf("event type is not valid in %s", e.options.LogFilePath)
				options.EventChannel <- ReadResult{Error: err}
				continue
			}
			switch event.Type {
			case Image, Volume, Pod, Container, Network, Secret:
				//	no-op
			case System:
				begin, end, err := e.readRotateEvent(event)
				if err != nil {
					options.EventChannel <- ReadResult{Error: err}
					continue
				}
				if begin && event.Time.After(readTime) {
					// If the rotation event happened _after_ we
					// started reading, we need to ignore/skip
					// subsequent event until the end of the
					// rotation.
					skipRotate = true
					logrus.Debugf("Skipping already read events after log-file rotation: %v", event)
				} else if end {
					// This rotate event
					skipRotate = false
				}
			default:
				err := fmt.Errorf("event type %s is not valid in %s", event.Type.String(), e.options.LogFilePath)
				options.EventChannel <- ReadResult{Error: err}
				continue
			}
			if skipRotate {
				continue
			}
			if applyFilters(event, filterMap) {
				options.EventChannel <- ReadResult{Event: event}
			}
		}
	}()
	return nil
}

// String returns a string representation of the logger
func (e EventLogFile) String() string {
	return LogFile.String()
}

const (
	rotateEventAttribute = "io.podman.event.rotate"
	rotateEventBegin     = "begin"
	rotateEventEnd       = "end"
)

func writeRotateEvent(f *os.File, logFilePath string, begin bool) error {
	rEvent := NewEvent(Rotate)
	rEvent.Type = System
	rEvent.Name = logFilePath
	rEvent.Attributes = make(map[string]string)
	if begin {
		rEvent.Attributes[rotateEventAttribute] = rotateEventBegin
	} else {
		rEvent.Attributes[rotateEventAttribute] = rotateEventEnd
	}
	rotateJSONString, err := rEvent.ToJSONString()
	if err != nil {
		return err
	}
	return writeToFile(rotateJSONString, f)
}

// Rotates the log file if the log file size and content exceeds limit
func rotateLog(logfile string, content string, limit uint64) (bool, error) {
	needsRotation, err := logNeedsRotation(logfile, content, limit)
	if err != nil || !needsRotation {
		return false, err
	}
	if err := truncate(logfile); err != nil {
		return false, err
	}
	return true, nil
}

// logNeedsRotation return true if the log file needs to be rotated.
func logNeedsRotation(logfile string, content string, limit uint64) (bool, error) {
	if limit == 0 {
		return false, nil
	}
	file, err := os.Stat(logfile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The logfile does not exist yet.
			return false, nil
		}
		return false, err
	}
	var filesize = uint64(file.Size())
	var contentsize = uint64(len([]rune(content)))
	if filesize+contentsize < limit {
		return false, nil
	}

	return true, nil
}

// Truncates the log file and saves 50% of content to new log file
func truncate(filePath string) error {
	orig, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer orig.Close()

	origFinfo, err := orig.Stat()
	if err != nil {
		return err
	}

	size := origFinfo.Size()
	threshold := size / 2

	tmp, err := os.CreateTemp(path.Dir(filePath), "")
	if err != nil {
		// Retry in /tmp in case creating a tmp file in the same
		// directory has failed.
		tmp, err = os.CreateTemp("", "")
		if err != nil {
			return err
		}
	}
	defer tmp.Close()

	// Jump directly to the threshold, drop the first line and copy the remainder
	if _, err := orig.Seek(threshold, 0); err != nil {
		return err
	}
	reader := bufio.NewReader(orig)
	if _, err := reader.ReadString('\n'); err != nil {
		if !errors.Is(err, io.EOF) {
			return err
		}
	}

	if err := writeRotateEvent(tmp, filePath, true); err != nil {
		return fmt.Errorf("writing rotation event begin marker: %w", err)
	}
	if _, err := reader.WriteTo(tmp); err != nil {
		return fmt.Errorf("writing truncated contents: %w", err)
	}
	if err := writeRotateEvent(tmp, filePath, false); err != nil {
		return fmt.Errorf("writing rotation event end marker: %w", err)
	}

	if err := renameLog(tmp.Name(), filePath); err != nil {
		return fmt.Errorf("writing back %s to %s: %w", tmp.Name(), filePath, err)
	}

	return nil
}

// Renames from, to
func renameLog(from, to string) error {
	err := os.Rename(from, to)
	if err == nil {
		return nil
	}

	if !errors.Is(err, unix.EXDEV) {
		return err
	}

	// Files are not on the same partition, so we need to copy them the
	// hard way.
	fFrom, err := os.Open(from)
	if err != nil {
		return err
	}
	defer fFrom.Close()

	// Remove the old file to make sure we're not truncating current
	// readers.
	if err := os.Remove(to); err != nil {
		return fmt.Errorf("recreating file %s: %w", to, err)
	}

	fTo, err := os.Create(to)
	if err != nil {
		return err
	}
	defer fTo.Close()

	if _, err := io.Copy(fTo, fFrom); err != nil {
		return fmt.Errorf("writing back from temporary file: %w", err)
	}

	if err := os.Remove(from); err != nil {
		return fmt.Errorf("removing temporary file: %w", err)
	}

	return nil
}
