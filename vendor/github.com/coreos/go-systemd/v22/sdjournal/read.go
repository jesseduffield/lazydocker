// Copyright 2015 RedHat, Inc.
// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sdjournal

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// ErrExpired gets returned when the Follow function runs into the
// specified timeout.
var ErrExpired = errors.New("Timeout expired")

// JournalReaderConfig represents options to drive the behavior of a JournalReader.
type JournalReaderConfig struct {
	// The Since, NumFromTail and Cursor options are mutually exclusive and
	// determine where the reading begins within the journal. The order in which
	// options are written is exactly the order of precedence.
	Since       time.Duration // start relative to a Duration from now
	NumFromTail uint64        // start relative to the tail
	Cursor      string        // start relative to the cursor

	// Show only journal entries whose fields match the supplied values. If
	// the array is empty, entries will not be filtered.
	Matches []Match

	// If not empty, the journal instance will point to a journal residing
	// in this directory. The supplied path may be relative or absolute.
	Path string

	// If not nil, Formatter will be used to translate the resulting entries
	// into strings. If not set, the default format (timestamp and message field)
	// will be used. If Formatter returns an error, Read will stop and return the error.
	Formatter func(entry *JournalEntry) (string, error)
}

// JournalReader is an io.ReadCloser which provides a simple interface for iterating through the
// systemd journal. A JournalReader is not safe for concurrent use by multiple goroutines.
type JournalReader struct {
	journal   *Journal
	msgReader *strings.Reader
	formatter func(entry *JournalEntry) (string, error)
}

// NewJournalReader creates a new JournalReader with configuration options that are similar to the
// systemd journalctl tool's iteration and filtering features.
func NewJournalReader(config JournalReaderConfig) (*JournalReader, error) {
	// use simpleMessageFormatter as default formatter.
	if config.Formatter == nil {
		config.Formatter = simpleMessageFormatter
	}

	r := &JournalReader{
		formatter: config.Formatter,
	}

	// Open the journal
	var err error
	if config.Path != "" {
		r.journal, err = NewJournalFromDir(config.Path)
	} else {
		r.journal, err = NewJournal()
	}
	if err != nil {
		return nil, err
	}

	// Add any supplied matches
	for _, m := range config.Matches {
		if err = r.journal.AddMatch(m.String()); err != nil {
			return nil, err
		}
	}

	// Set the start position based on options
	if config.Since != 0 {
		// Start based on a relative time
		start := time.Now().Add(config.Since)
		if err := r.journal.SeekRealtimeUsec(uint64(start.UnixNano() / 1000)); err != nil {
			return nil, err
		}
	} else if config.NumFromTail != 0 {
		// Start based on a number of lines before the tail
		if err := r.journal.SeekTail(); err != nil {
			return nil, err
		}

		// Move the read pointer into position near the tail. Go one further than
		// the option so that the initial cursor advancement positions us at the
		// correct starting point.
		skip, err := r.journal.PreviousSkip(config.NumFromTail + 1)
		if err != nil {
			return nil, err
		}
		// If we skipped fewer lines than expected, we have reached journal start.
		// Thus, we seek to head so that next invocation can read the first line.
		if skip != config.NumFromTail+1 {
			if err := r.journal.SeekHead(); err != nil {
				return nil, err
			}
		}
	} else if config.Cursor != "" {
		// Start based on a custom cursor
		if err := r.journal.SeekCursor(config.Cursor); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// Read reads entries from the journal. Read follows the Reader interface so
// it must be able to read a specific amount of bytes. Journald on the other
// hand only allows us to read full entries of arbitrary size (without byte
// granularity). JournalReader is therefore internally buffering entries that
// don't fit in the read buffer. Callers should keep calling until 0 and/or an
// error is returned.
func (r *JournalReader) Read(b []byte) (int, error) {
	if r.msgReader == nil {
		// Advance the journal cursor. It has to be called at least one time
		// before reading
		c, err := r.journal.Next()
		// An unexpected error
		if err != nil {
			return 0, err
		}

		// EOF detection
		if c == 0 {
			return 0, io.EOF
		}

		entry, err := r.journal.GetEntry()
		if err != nil {
			return 0, err
		}

		// Build a message
		msg, err := r.formatter(entry)
		if err != nil {
			return 0, err
		}
		r.msgReader = strings.NewReader(msg)
	}

	// Copy and return the message
	sz, err := r.msgReader.Read(b)
	if err == io.EOF {
		// The current entry has been fully read. Don't propagate this
		// EOF, so the next entry can be read at the next Read()
		// iteration.
		r.msgReader = nil
		return sz, nil
	}
	if err != nil {
		return sz, err
	}
	if r.msgReader.Len() == 0 {
		r.msgReader = nil
	}

	return sz, nil
}

// Close closes the JournalReader's handle to the journal.
func (r *JournalReader) Close() error {
	return r.journal.Close()
}

// Rewind attempts to rewind the JournalReader to the first entry.
func (r *JournalReader) Rewind() error {
	r.msgReader = nil
	return r.journal.SeekHead()
}

// Follow synchronously follows the JournalReader, writing each new journal entry to writer. The
// follow will continue until a single time.Time is received on the until channel.
func (r *JournalReader) Follow(until <-chan time.Time, writer io.Writer) error {
	// Process journal entries and events. Entries are flushed until the tail or
	// timeout is reached, and then we wait for new events or the timeout.
	msg := make([]byte, 64*1<<(10))
	waitCh := make(chan int, 1)
	var waitGroup sync.WaitGroup
	defer waitGroup.Wait()

process:
	for {
		c, err := r.Read(msg)
		if err != nil && err != io.EOF {
			return err
		}

		select {
		case <-until:
			return ErrExpired
		default:
		}
		if c > 0 {
			if _, err = writer.Write(msg[:c]); err != nil {
				return err
			}
			continue process
		}

		// We're at the tail, so wait for new events or time out.
		// Holds journal events to process. Tightly bounded for now unless there's a
		// reason to unblock the journal watch routine more quickly.
		for {
			waitGroup.Add(1)
			go func() {
				status := r.journal.Wait(100 * time.Millisecond)
				waitCh <- status
				waitGroup.Done()
			}()

			select {
			case <-until:
				return ErrExpired
			case e := <-waitCh:
				switch e {
				case SD_JOURNAL_NOP:
					// the journal did not change since the last invocation
				case SD_JOURNAL_APPEND, SD_JOURNAL_INVALIDATE:
					continue process
				default:
					if e < 0 {
						return fmt.Errorf("received error event: %d", e)
					}

					log.Printf("received unknown event: %d\n", e)
				}
			}
		}
	}
}

// simpleMessageFormatter is the default formatter.
// It returns a string representing the current journal entry in a simple format which
// includes the entry timestamp and MESSAGE field.
func simpleMessageFormatter(entry *JournalEntry) (string, error) {
	msg, ok := entry.Fields["MESSAGE"]
	if !ok {
		return "", fmt.Errorf("no MESSAGE field present in journal entry")
	}

	usec := entry.RealtimeTimestamp
	timestamp := time.Unix(0, int64(usec)*int64(time.Microsecond))

	return fmt.Sprintf("%s %s\n", timestamp, msg), nil
}
