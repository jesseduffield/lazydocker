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

// Package sdjournal provides a low-level Go interface to the
// systemd journal wrapped around the sd-journal C API.
//
// All public read methods map closely to the sd-journal API functions. See the
// sd-journal.h documentation[1] for information about each function.
//
// To write to the journal, see the pure-Go "journal" package
//
// [1] http://www.freedesktop.org/software/systemd/man/sd-journal.html
package sdjournal

// #include <systemd/sd-journal.h>
// #include <systemd/sd-id128.h>
// #include <stdlib.h>
// #include <syslog.h>
//
// int
// my_sd_journal_open(void *f, sd_journal **ret, int flags)
// {
//   int (*sd_journal_open)(sd_journal **, int);
//
//   sd_journal_open = f;
//   return sd_journal_open(ret, flags);
// }
//
// int
// my_sd_journal_open_directory(void *f, sd_journal **ret, const char *path, int flags)
// {
//   int (*sd_journal_open_directory)(sd_journal **, const char *, int);
//
//   sd_journal_open_directory = f;
//   return sd_journal_open_directory(ret, path, flags);
// }
//
// int
// my_sd_journal_open_files(void *f, sd_journal **ret, const char **paths, int flags)
// {
//   int (*sd_journal_open_files)(sd_journal **, const char **, int);
//
//   sd_journal_open_files = f;
//   return sd_journal_open_files(ret, paths, flags);
// }
//
// void
// my_sd_journal_close(void *f, sd_journal *j)
// {
//   void (*sd_journal_close)(sd_journal *);
//
//   sd_journal_close = f;
//   sd_journal_close(j);
// }
//
// int
// my_sd_journal_get_usage(void *f, sd_journal *j, uint64_t *bytes)
// {
//   int (*sd_journal_get_usage)(sd_journal *, uint64_t *);
//
//   sd_journal_get_usage = f;
//   return sd_journal_get_usage(j, bytes);
// }
//
// int
// my_sd_journal_add_match(void *f, sd_journal *j, const void *data, size_t size)
// {
//   int (*sd_journal_add_match)(sd_journal *, const void *, size_t);
//
//   sd_journal_add_match = f;
//   return sd_journal_add_match(j, data, size);
// }
//
// int
// my_sd_journal_add_disjunction(void *f, sd_journal *j)
// {
//   int (*sd_journal_add_disjunction)(sd_journal *);
//
//   sd_journal_add_disjunction = f;
//   return sd_journal_add_disjunction(j);
// }
//
// int
// my_sd_journal_add_conjunction(void *f, sd_journal *j)
// {
//   int (*sd_journal_add_conjunction)(sd_journal *);
//
//   sd_journal_add_conjunction = f;
//   return sd_journal_add_conjunction(j);
// }
//
// void
// my_sd_journal_flush_matches(void *f, sd_journal *j)
// {
//   void (*sd_journal_flush_matches)(sd_journal *);
//
//   sd_journal_flush_matches = f;
//   sd_journal_flush_matches(j);
// }
//
// int
// my_sd_journal_next(void *f, sd_journal *j)
// {
//   int (*sd_journal_next)(sd_journal *);
//
//   sd_journal_next = f;
//   return sd_journal_next(j);
// }
//
// int
// my_sd_journal_next_skip(void *f, sd_journal *j, uint64_t skip)
// {
//   int (*sd_journal_next_skip)(sd_journal *, uint64_t);
//
//   sd_journal_next_skip = f;
//   return sd_journal_next_skip(j, skip);
// }
//
// int
// my_sd_journal_previous(void *f, sd_journal *j)
// {
//   int (*sd_journal_previous)(sd_journal *);
//
//   sd_journal_previous = f;
//   return sd_journal_previous(j);
// }
//
// int
// my_sd_journal_previous_skip(void *f, sd_journal *j, uint64_t skip)
// {
//   int (*sd_journal_previous_skip)(sd_journal *, uint64_t);
//
//   sd_journal_previous_skip = f;
//   return sd_journal_previous_skip(j, skip);
// }
//
// int
// my_sd_journal_get_data(void *f, sd_journal *j, const char *field, const void **data, size_t *length)
// {
//   int (*sd_journal_get_data)(sd_journal *, const char *, const void **, size_t *);
//
//   sd_journal_get_data = f;
//   return sd_journal_get_data(j, field, data, length);
// }
//
// int
// my_sd_journal_set_data_threshold(void *f, sd_journal *j, size_t sz)
// {
//   int (*sd_journal_set_data_threshold)(sd_journal *, size_t);
//
//   sd_journal_set_data_threshold = f;
//   return sd_journal_set_data_threshold(j, sz);
// }
//
// int
// my_sd_journal_get_cursor(void *f, sd_journal *j, char **cursor)
// {
//   int (*sd_journal_get_cursor)(sd_journal *, char **);
//
//   sd_journal_get_cursor = f;
//   return sd_journal_get_cursor(j, cursor);
// }
//
// int
// my_sd_journal_test_cursor(void *f, sd_journal *j, const char *cursor)
// {
//   int (*sd_journal_test_cursor)(sd_journal *, const char *);
//
//   sd_journal_test_cursor = f;
//   return sd_journal_test_cursor(j, cursor);
// }
//
// int
// my_sd_journal_get_realtime_usec(void *f, sd_journal *j, uint64_t *usec)
// {
//   int (*sd_journal_get_realtime_usec)(sd_journal *, uint64_t *);
//
//   sd_journal_get_realtime_usec = f;
//   return sd_journal_get_realtime_usec(j, usec);
// }
//
// int
// my_sd_journal_get_monotonic_usec(void *f, sd_journal *j, uint64_t *usec, sd_id128_t *boot_id)
// {
//   int (*sd_journal_get_monotonic_usec)(sd_journal *, uint64_t *, sd_id128_t *);
//
//   sd_journal_get_monotonic_usec = f;
//   return sd_journal_get_monotonic_usec(j, usec, boot_id);
// }
//
// int
// my_sd_journal_seek_head(void *f, sd_journal *j)
// {
//   int (*sd_journal_seek_head)(sd_journal *);
//
//   sd_journal_seek_head = f;
//   return sd_journal_seek_head(j);
// }
//
// int
// my_sd_journal_seek_tail(void *f, sd_journal *j)
// {
//   int (*sd_journal_seek_tail)(sd_journal *);
//
//   sd_journal_seek_tail = f;
//   return sd_journal_seek_tail(j);
// }
//
//
// int
// my_sd_journal_seek_cursor(void *f, sd_journal *j, const char *cursor)
// {
//   int (*sd_journal_seek_cursor)(sd_journal *, const char *);
//
//   sd_journal_seek_cursor = f;
//   return sd_journal_seek_cursor(j, cursor);
// }
//
// int
// my_sd_journal_seek_realtime_usec(void *f, sd_journal *j, uint64_t usec)
// {
//   int (*sd_journal_seek_realtime_usec)(sd_journal *, uint64_t);
//
//   sd_journal_seek_realtime_usec = f;
//   return sd_journal_seek_realtime_usec(j, usec);
// }
//
// int
// my_sd_journal_wait(void *f, sd_journal *j, uint64_t timeout_usec)
// {
//   int (*sd_journal_wait)(sd_journal *, uint64_t);
//
//   sd_journal_wait = f;
//   return sd_journal_wait(j, timeout_usec);
// }
//
// void
// my_sd_journal_restart_data(void *f, sd_journal *j)
// {
//   void (*sd_journal_restart_data)(sd_journal *);
//
//   sd_journal_restart_data = f;
//   sd_journal_restart_data(j);
// }
//
// int
// my_sd_journal_enumerate_data(void *f, sd_journal *j, const void **data, size_t *length)
// {
//   int (*sd_journal_enumerate_data)(sd_journal *, const void **, size_t *);
//
//   sd_journal_enumerate_data = f;
//   return sd_journal_enumerate_data(j, data, length);
// }
//
// int
// my_sd_journal_query_unique(void *f, sd_journal *j, const char *field)
// {
//   int(*sd_journal_query_unique)(sd_journal *, const char *);
//
//   sd_journal_query_unique = f;
//   return sd_journal_query_unique(j, field);
// }
//
// int
// my_sd_journal_enumerate_unique(void *f, sd_journal *j, const void **data, size_t *length)
// {
//   int(*sd_journal_enumerate_unique)(sd_journal *, const void **, size_t *);
//
//   sd_journal_enumerate_unique = f;
//   return sd_journal_enumerate_unique(j, data, length);
// }
//
// void
// my_sd_journal_restart_unique(void *f, sd_journal *j)
// {
//   void(*sd_journal_restart_unique)(sd_journal *);
//
//   sd_journal_restart_unique = f;
//   sd_journal_restart_unique(j);
// }
//
// int
// my_sd_journal_get_catalog(void *f, sd_journal *j, char **ret)
// {
//   int(*sd_journal_get_catalog)(sd_journal *, char **);
//
//   sd_journal_get_catalog = f;
//   return sd_journal_get_catalog(j, ret);
// }
//
// int
// my_sd_id128_get_boot(void *f, sd_id128_t *boot_id)
// {
//   int(*sd_id128_get_boot)(sd_id128_t *);
//
//   sd_id128_get_boot = f;
//   return sd_id128_get_boot(boot_id);
// }
//
// char *
// my_sd_id128_to_string(void *f, sd_id128_t boot_id, char s[SD_ID128_STRING_MAX])
// {
//   char *(*sd_id128_to_string)(sd_id128_t, char *);
//
//   sd_id128_to_string = f;
//   return sd_id128_to_string(boot_id, s);
// }
//
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Journal entry field strings which correspond to:
// http://www.freedesktop.org/software/systemd/man/systemd.journal-fields.html
const (
	// User Journal Fields
	SD_JOURNAL_FIELD_MESSAGE           = "MESSAGE"
	SD_JOURNAL_FIELD_MESSAGE_ID        = "MESSAGE_ID"
	SD_JOURNAL_FIELD_PRIORITY          = "PRIORITY"
	SD_JOURNAL_FIELD_CODE_FILE         = "CODE_FILE"
	SD_JOURNAL_FIELD_CODE_LINE         = "CODE_LINE"
	SD_JOURNAL_FIELD_CODE_FUNC         = "CODE_FUNC"
	SD_JOURNAL_FIELD_ERRNO             = "ERRNO"
	SD_JOURNAL_FIELD_SYSLOG_FACILITY   = "SYSLOG_FACILITY"
	SD_JOURNAL_FIELD_SYSLOG_IDENTIFIER = "SYSLOG_IDENTIFIER"
	SD_JOURNAL_FIELD_SYSLOG_PID        = "SYSLOG_PID"

	// Trusted Journal Fields
	SD_JOURNAL_FIELD_PID                       = "_PID"
	SD_JOURNAL_FIELD_UID                       = "_UID"
	SD_JOURNAL_FIELD_GID                       = "_GID"
	SD_JOURNAL_FIELD_COMM                      = "_COMM"
	SD_JOURNAL_FIELD_EXE                       = "_EXE"
	SD_JOURNAL_FIELD_CMDLINE                   = "_CMDLINE"
	SD_JOURNAL_FIELD_CAP_EFFECTIVE             = "_CAP_EFFECTIVE"
	SD_JOURNAL_FIELD_AUDIT_SESSION             = "_AUDIT_SESSION"
	SD_JOURNAL_FIELD_AUDIT_LOGINUID            = "_AUDIT_LOGINUID"
	SD_JOURNAL_FIELD_SYSTEMD_CGROUP            = "_SYSTEMD_CGROUP"
	SD_JOURNAL_FIELD_SYSTEMD_SESSION           = "_SYSTEMD_SESSION"
	SD_JOURNAL_FIELD_SYSTEMD_UNIT              = "_SYSTEMD_UNIT"
	SD_JOURNAL_FIELD_SYSTEMD_USER_UNIT         = "_SYSTEMD_USER_UNIT"
	SD_JOURNAL_FIELD_SYSTEMD_OWNER_UID         = "_SYSTEMD_OWNER_UID"
	SD_JOURNAL_FIELD_SYSTEMD_SLICE             = "_SYSTEMD_SLICE"
	SD_JOURNAL_FIELD_SELINUX_CONTEXT           = "_SELINUX_CONTEXT"
	SD_JOURNAL_FIELD_SOURCE_REALTIME_TIMESTAMP = "_SOURCE_REALTIME_TIMESTAMP"
	SD_JOURNAL_FIELD_BOOT_ID                   = "_BOOT_ID"
	SD_JOURNAL_FIELD_MACHINE_ID                = "_MACHINE_ID"
	SD_JOURNAL_FIELD_HOSTNAME                  = "_HOSTNAME"
	SD_JOURNAL_FIELD_TRANSPORT                 = "_TRANSPORT"

	// Address Fields
	SD_JOURNAL_FIELD_CURSOR              = "__CURSOR"
	SD_JOURNAL_FIELD_REALTIME_TIMESTAMP  = "__REALTIME_TIMESTAMP"
	SD_JOURNAL_FIELD_MONOTONIC_TIMESTAMP = "__MONOTONIC_TIMESTAMP"
)

// Journal event constants
const (
	SD_JOURNAL_NOP        = int(C.SD_JOURNAL_NOP)
	SD_JOURNAL_APPEND     = int(C.SD_JOURNAL_APPEND)
	SD_JOURNAL_INVALIDATE = int(C.SD_JOURNAL_INVALIDATE)
)

const (
	// IndefiniteWait is a sentinel value that can be passed to
	// sdjournal.Wait() to signal an indefinite wait for new journal
	// events. It is implemented as the maximum value for a time.Duration:
	// https://github.com/golang/go/blob/e4dcf5c8c22d98ac9eac7b9b226596229624cb1d/src/time/time.go#L434
	IndefiniteWait time.Duration = 1<<63 - 1
)

// ErrNoTestCursor gets returned when using TestCursor function and cursor
// parameter is not the same as the current cursor position.
var ErrNoTestCursor = errors.New("Cursor parameter is not the same as current position")

// Journal is a Go wrapper of an sd_journal structure.
type Journal struct {
	cjournal *C.sd_journal
	mu       sync.Mutex
}

// JournalEntry represents all fields of a journal entry plus address fields.
type JournalEntry struct {
	Fields             map[string]string
	Cursor             string
	RealtimeTimestamp  uint64
	MonotonicTimestamp uint64
}

// Match is a convenience wrapper to describe filters supplied to AddMatch.
type Match struct {
	Field string
	Value string
}

// String returns a string representation of a Match suitable for use with AddMatch.
func (m *Match) String() string {
	return m.Field + "=" + m.Value
}

// NewJournal returns a new Journal instance pointing to the local journal
func NewJournal() (j *Journal, err error) {
	j = &Journal{}

	sd_journal_open, err := getFunction("sd_journal_open")
	if err != nil {
		return nil, err
	}

	r := C.my_sd_journal_open(sd_journal_open, &j.cjournal, C.SD_JOURNAL_LOCAL_ONLY)

	if r < 0 {
		return nil, fmt.Errorf("failed to open journal: %s", syscall.Errno(-r).Error())
	}

	return j, nil
}

// NewJournalFromDir returns a new Journal instance pointing to a journal residing
// in a given directory.
func NewJournalFromDir(path string) (j *Journal, err error) {
	j = &Journal{}

	sd_journal_open_directory, err := getFunction("sd_journal_open_directory")
	if err != nil {
		return nil, err
	}

	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))

	r := C.my_sd_journal_open_directory(sd_journal_open_directory, &j.cjournal, p, 0)
	if r < 0 {
		return nil, fmt.Errorf("failed to open journal in directory %q: %s", path, syscall.Errno(-r).Error())
	}

	return j, nil
}

// NewJournalFromFiles returns a new Journal instance pointing to a journals residing
// in a given files.
func NewJournalFromFiles(paths ...string) (j *Journal, err error) {
	j = &Journal{}

	sd_journal_open_files, err := getFunction("sd_journal_open_files")
	if err != nil {
		return nil, err
	}

	// by making the slice 1 elem too long, we guarantee it'll be null-terminated
	cPaths := make([]*C.char, len(paths)+1)
	for idx, path := range paths {
		p := C.CString(path)
		cPaths[idx] = p
		defer C.free(unsafe.Pointer(p))
	}

	r := C.my_sd_journal_open_files(sd_journal_open_files, &j.cjournal, &cPaths[0], 0)
	if r < 0 {
		return nil, fmt.Errorf("failed to open journals in paths %q: %s", paths, syscall.Errno(-r).Error())
	}

	return j, nil
}

// Close closes a journal opened with NewJournal.
func (j *Journal) Close() error {
	sd_journal_close, err := getFunction("sd_journal_close")
	if err != nil {
		return err
	}

	j.mu.Lock()
	C.my_sd_journal_close(sd_journal_close, j.cjournal)
	j.mu.Unlock()

	return nil
}

// AddMatch adds a match by which to filter the entries of the journal.
func (j *Journal) AddMatch(match string) error {
	sd_journal_add_match, err := getFunction("sd_journal_add_match")
	if err != nil {
		return err
	}

	m := C.CString(match)
	defer C.free(unsafe.Pointer(m))

	j.mu.Lock()
	r := C.my_sd_journal_add_match(sd_journal_add_match, j.cjournal, unsafe.Pointer(m), C.size_t(len(match)))
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to add match: %s", syscall.Errno(-r).Error())
	}

	return nil
}

// AddDisjunction inserts a logical OR in the match list.
func (j *Journal) AddDisjunction() error {
	sd_journal_add_disjunction, err := getFunction("sd_journal_add_disjunction")
	if err != nil {
		return err
	}

	j.mu.Lock()
	r := C.my_sd_journal_add_disjunction(sd_journal_add_disjunction, j.cjournal)
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to add a disjunction in the match list: %s", syscall.Errno(-r).Error())
	}

	return nil
}

// AddConjunction inserts a logical AND in the match list.
func (j *Journal) AddConjunction() error {
	sd_journal_add_conjunction, err := getFunction("sd_journal_add_conjunction")
	if err != nil {
		return err
	}

	j.mu.Lock()
	r := C.my_sd_journal_add_conjunction(sd_journal_add_conjunction, j.cjournal)
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to add a conjunction in the match list: %s", syscall.Errno(-r).Error())
	}

	return nil
}

// FlushMatches flushes all matches, disjunctions and conjunctions.
func (j *Journal) FlushMatches() {
	sd_journal_flush_matches, err := getFunction("sd_journal_flush_matches")
	if err != nil {
		return
	}

	j.mu.Lock()
	C.my_sd_journal_flush_matches(sd_journal_flush_matches, j.cjournal)
	j.mu.Unlock()
}

// Next advances the read pointer into the journal by one entry.
func (j *Journal) Next() (uint64, error) {
	sd_journal_next, err := getFunction("sd_journal_next")
	if err != nil {
		return 0, err
	}

	j.mu.Lock()
	r := C.my_sd_journal_next(sd_journal_next, j.cjournal)
	j.mu.Unlock()

	if r < 0 {
		return 0, fmt.Errorf("failed to iterate journal: %s", syscall.Errno(-r).Error())
	}

	return uint64(r), nil
}

// NextSkip advances the read pointer by multiple entries at once,
// as specified by the skip parameter.
func (j *Journal) NextSkip(skip uint64) (uint64, error) {
	sd_journal_next_skip, err := getFunction("sd_journal_next_skip")
	if err != nil {
		return 0, err
	}

	j.mu.Lock()
	r := C.my_sd_journal_next_skip(sd_journal_next_skip, j.cjournal, C.uint64_t(skip))
	j.mu.Unlock()

	if r < 0 {
		return 0, fmt.Errorf("failed to iterate journal: %s", syscall.Errno(-r).Error())
	}

	return uint64(r), nil
}

// Previous sets the read pointer into the journal back by one entry.
func (j *Journal) Previous() (uint64, error) {
	sd_journal_previous, err := getFunction("sd_journal_previous")
	if err != nil {
		return 0, err
	}

	j.mu.Lock()
	r := C.my_sd_journal_previous(sd_journal_previous, j.cjournal)
	j.mu.Unlock()

	if r < 0 {
		return 0, fmt.Errorf("failed to iterate journal: %s", syscall.Errno(-r).Error())
	}

	return uint64(r), nil
}

// PreviousSkip sets back the read pointer by multiple entries at once,
// as specified by the skip parameter.
func (j *Journal) PreviousSkip(skip uint64) (uint64, error) {
	sd_journal_previous_skip, err := getFunction("sd_journal_previous_skip")
	if err != nil {
		return 0, err
	}

	j.mu.Lock()
	r := C.my_sd_journal_previous_skip(sd_journal_previous_skip, j.cjournal, C.uint64_t(skip))
	j.mu.Unlock()

	if r < 0 {
		return 0, fmt.Errorf("failed to iterate journal: %s", syscall.Errno(-r).Error())
	}

	return uint64(r), nil
}

func (j *Journal) getData(field string) (unsafe.Pointer, C.int, error) {
	sd_journal_get_data, err := getFunction("sd_journal_get_data")
	if err != nil {
		return nil, 0, err
	}

	f := C.CString(field)
	defer C.free(unsafe.Pointer(f))

	var d unsafe.Pointer
	var l C.size_t

	j.mu.Lock()
	r := C.my_sd_journal_get_data(sd_journal_get_data, j.cjournal, f, &d, &l)
	j.mu.Unlock()

	if r < 0 {
		return nil, 0, fmt.Errorf("failed to read message: %s", syscall.Errno(-r).Error())
	}

	return d, C.int(l), nil
}

// GetData gets the data object associated with a specific field from the
// the journal entry referenced by the last completed Next/Previous function
// call. To call GetData, you must have first called one of these functions.
func (j *Journal) GetData(field string) (string, error) {
	d, l, err := j.getData(field)
	if err != nil {
		return "", err
	}

	return C.GoStringN((*C.char)(d), l), nil
}

// GetDataValue gets the data object associated with a specific field from the
// journal entry referenced by the last completed Next/Previous function call,
// returning only the value of the object. To call GetDataValue, you must first
// have called one of the Next/Previous functions.
func (j *Journal) GetDataValue(field string) (string, error) {
	val, err := j.GetData(field)
	if err != nil {
		return "", err
	}

	return strings.SplitN(val, "=", 2)[1], nil
}

// GetDataBytes gets the data object associated with a specific field from the
// journal entry referenced by the last completed Next/Previous function call.
// To call GetDataBytes, you must first have called one of these functions.
func (j *Journal) GetDataBytes(field string) ([]byte, error) {
	d, l, err := j.getData(field)
	if err != nil {
		return nil, err
	}

	return C.GoBytes(d, l), nil
}

// GetDataValueBytes gets the data object associated with a specific field from the
// journal entry referenced by the last completed Next/Previous function call,
// returning only the value of the object. To call GetDataValueBytes, you must first
// have called one of the Next/Previous functions.
func (j *Journal) GetDataValueBytes(field string) ([]byte, error) {
	val, err := j.GetDataBytes(field)
	if err != nil {
		return nil, err
	}

	return bytes.SplitN(val, []byte("="), 2)[1], nil
}

// GetEntry returns a full representation of the journal entry referenced by the
// last completed Next/Previous function call, with all key-value pairs of data
// as well as address fields (cursor, realtime timestamp and monotonic timestamp).
// To call GetEntry, you must first have called one of the Next/Previous functions.
func (j *Journal) GetEntry() (*JournalEntry, error) {
	sd_journal_get_realtime_usec, err := getFunction("sd_journal_get_realtime_usec")
	if err != nil {
		return nil, err
	}

	sd_journal_get_monotonic_usec, err := getFunction("sd_journal_get_monotonic_usec")
	if err != nil {
		return nil, err
	}

	sd_journal_get_cursor, err := getFunction("sd_journal_get_cursor")
	if err != nil {
		return nil, err
	}

	sd_journal_restart_data, err := getFunction("sd_journal_restart_data")
	if err != nil {
		return nil, err
	}

	sd_journal_enumerate_data, err := getFunction("sd_journal_enumerate_data")
	if err != nil {
		return nil, err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	var r C.int
	entry := &JournalEntry{Fields: make(map[string]string)}

	var realtimeUsec C.uint64_t
	r = C.my_sd_journal_get_realtime_usec(sd_journal_get_realtime_usec, j.cjournal, &realtimeUsec)
	if r < 0 {
		return nil, fmt.Errorf("failed to get realtime timestamp: %s", syscall.Errno(-r).Error())
	}

	entry.RealtimeTimestamp = uint64(realtimeUsec)

	var monotonicUsec C.uint64_t
	var boot_id C.sd_id128_t

	r = C.my_sd_journal_get_monotonic_usec(sd_journal_get_monotonic_usec, j.cjournal, &monotonicUsec, &boot_id)
	if r < 0 {
		return nil, fmt.Errorf("failed to get monotonic timestamp: %s", syscall.Errno(-r).Error())
	}

	entry.MonotonicTimestamp = uint64(monotonicUsec)

	var c *C.char
	// since the pointer is mutated by sd_journal_get_cursor, need to wait
	// until after the call to free the memory
	r = C.my_sd_journal_get_cursor(sd_journal_get_cursor, j.cjournal, &c)
	defer C.free(unsafe.Pointer(c))
	if r < 0 {
		return nil, fmt.Errorf("failed to get cursor: %s", syscall.Errno(-r).Error())
	}

	entry.Cursor = C.GoString(c)

	// Implements the JOURNAL_FOREACH_DATA_RETVAL macro from journal-internal.h
	var d unsafe.Pointer
	var l C.size_t
	C.my_sd_journal_restart_data(sd_journal_restart_data, j.cjournal)
	for {
		r = C.my_sd_journal_enumerate_data(sd_journal_enumerate_data, j.cjournal, &d, &l)
		if r == 0 {
			break
		}

		if r < 0 {
			return nil, fmt.Errorf("failed to read message field: %s", syscall.Errno(-r).Error())
		}

		msg := C.GoStringN((*C.char)(d), C.int(l))
		kv := strings.SplitN(msg, "=", 2)
		if len(kv) < 2 {
			return nil, fmt.Errorf("failed to parse field")
		}

		entry.Fields[kv[0]] = kv[1]
	}

	return entry, nil
}

// SetDataThreshold sets the data field size threshold for data returned by
// GetData. To retrieve the complete data fields this threshold should be
// turned off by setting it to 0, so that the library always returns the
// complete data objects.
func (j *Journal) SetDataThreshold(threshold uint64) error {
	sd_journal_set_data_threshold, err := getFunction("sd_journal_set_data_threshold")
	if err != nil {
		return err
	}

	j.mu.Lock()
	r := C.my_sd_journal_set_data_threshold(sd_journal_set_data_threshold, j.cjournal, C.size_t(threshold))
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to set data threshold: %s", syscall.Errno(-r).Error())
	}

	return nil
}

// GetRealtimeUsec gets the realtime (wallclock) timestamp of the journal
// entry referenced by the last completed Next/Previous function call. To
// call GetRealtimeUsec, you must first have called one of the Next/Previous
// functions.
func (j *Journal) GetRealtimeUsec() (uint64, error) {
	var usec C.uint64_t

	sd_journal_get_realtime_usec, err := getFunction("sd_journal_get_realtime_usec")
	if err != nil {
		return 0, err
	}

	j.mu.Lock()
	r := C.my_sd_journal_get_realtime_usec(sd_journal_get_realtime_usec, j.cjournal, &usec)
	j.mu.Unlock()

	if r < 0 {
		return 0, fmt.Errorf("failed to get realtime timestamp: %s", syscall.Errno(-r).Error())
	}

	return uint64(usec), nil
}

// GetMonotonicUsec gets the monotonic timestamp of the journal entry
// referenced by the last completed Next/Previous function call. To call
// GetMonotonicUsec, you must first have called one of the Next/Previous
// functions.
func (j *Journal) GetMonotonicUsec() (uint64, error) {
	var usec C.uint64_t
	var boot_id C.sd_id128_t

	sd_journal_get_monotonic_usec, err := getFunction("sd_journal_get_monotonic_usec")
	if err != nil {
		return 0, err
	}

	j.mu.Lock()
	r := C.my_sd_journal_get_monotonic_usec(sd_journal_get_monotonic_usec, j.cjournal, &usec, &boot_id)
	j.mu.Unlock()

	if r < 0 {
		return 0, fmt.Errorf("failed to get monotonic timestamp: %s", syscall.Errno(-r).Error())
	}

	return uint64(usec), nil
}

// GetCursor gets the cursor of the last journal entry reeferenced by the
// last completed Next/Previous function call. To call GetCursor, you must
// first have called one of the Next/Previous functions.
func (j *Journal) GetCursor() (string, error) {
	sd_journal_get_cursor, err := getFunction("sd_journal_get_cursor")
	if err != nil {
		return "", err
	}

	var d *C.char
	// since the pointer is mutated by sd_journal_get_cursor, need to wait
	// until after the call to free the memory

	j.mu.Lock()
	r := C.my_sd_journal_get_cursor(sd_journal_get_cursor, j.cjournal, &d)
	j.mu.Unlock()
	defer C.free(unsafe.Pointer(d))

	if r < 0 {
		return "", fmt.Errorf("failed to get cursor: %s", syscall.Errno(-r).Error())
	}

	cursor := C.GoString(d)

	return cursor, nil
}

// TestCursor checks whether the current position in the journal matches the
// specified cursor
func (j *Journal) TestCursor(cursor string) error {
	sd_journal_test_cursor, err := getFunction("sd_journal_test_cursor")
	if err != nil {
		return err
	}

	c := C.CString(cursor)
	defer C.free(unsafe.Pointer(c))

	j.mu.Lock()
	r := C.my_sd_journal_test_cursor(sd_journal_test_cursor, j.cjournal, c)
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to test to cursor %q: %s", cursor, syscall.Errno(-r).Error())
	} else if r == 0 {
		return ErrNoTestCursor
	}

	return nil
}

// SeekHead seeks to the beginning of the journal, i.e. the oldest available
// entry. This call must be followed by a call to Next before any call to
// Get* will return data about the first element.
func (j *Journal) SeekHead() error {
	sd_journal_seek_head, err := getFunction("sd_journal_seek_head")
	if err != nil {
		return err
	}

	j.mu.Lock()
	r := C.my_sd_journal_seek_head(sd_journal_seek_head, j.cjournal)
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to seek to head of journal: %s", syscall.Errno(-r).Error())
	}

	return nil
}

// SeekTail may be used to seek to the end of the journal, i.e. the most recent
// available entry. This call must be followed by a call to Previous before any
// call to Get* will return data about the last element.
func (j *Journal) SeekTail() error {
	sd_journal_seek_tail, err := getFunction("sd_journal_seek_tail")
	if err != nil {
		return err
	}

	j.mu.Lock()
	r := C.my_sd_journal_seek_tail(sd_journal_seek_tail, j.cjournal)
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to seek to tail of journal: %s", syscall.Errno(-r).Error())
	}

	return nil
}

// SeekRealtimeUsec seeks to the entry with the specified realtime (wallclock)
// timestamp, i.e. CLOCK_REALTIME. This call must be followed by a call to
// Next/Previous before any call to Get* will return data about the sought entry.
func (j *Journal) SeekRealtimeUsec(usec uint64) error {
	sd_journal_seek_realtime_usec, err := getFunction("sd_journal_seek_realtime_usec")
	if err != nil {
		return err
	}

	j.mu.Lock()
	r := C.my_sd_journal_seek_realtime_usec(sd_journal_seek_realtime_usec, j.cjournal, C.uint64_t(usec))
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to seek to %d: %s", usec, syscall.Errno(-r).Error())
	}

	return nil
}

// SeekCursor seeks to a concrete journal cursor. This call must be
// followed by a call to Next/Previous before any call to Get* will return
// data about the sought entry.
func (j *Journal) SeekCursor(cursor string) error {
	sd_journal_seek_cursor, err := getFunction("sd_journal_seek_cursor")
	if err != nil {
		return err
	}

	c := C.CString(cursor)
	defer C.free(unsafe.Pointer(c))

	j.mu.Lock()
	r := C.my_sd_journal_seek_cursor(sd_journal_seek_cursor, j.cjournal, c)
	j.mu.Unlock()

	if r < 0 {
		return fmt.Errorf("failed to seek to cursor %q: %s", cursor, syscall.Errno(-r).Error())
	}

	return nil
}

// Wait will synchronously wait until the journal gets changed. The maximum time
// this call sleeps may be controlled with the timeout parameter.  If
// sdjournal.IndefiniteWait is passed as the timeout parameter, Wait will
// wait indefinitely for a journal change.
func (j *Journal) Wait(timeout time.Duration) int {
	var to uint64

	sd_journal_wait, err := getFunction("sd_journal_wait")
	if err != nil {
		return -1
	}

	if timeout == IndefiniteWait {
		// sd_journal_wait(3) calls for a (uint64_t) -1 to be passed to signify
		// indefinite wait, but using a -1 overflows our C.uint64_t, so we use an
		// equivalent hex value.
		to = 0xffffffffffffffff
	} else {
		to = uint64(timeout / time.Microsecond)
	}
	j.mu.Lock()
	r := C.my_sd_journal_wait(sd_journal_wait, j.cjournal, C.uint64_t(to))
	j.mu.Unlock()

	return int(r)
}

// GetUsage returns the journal disk space usage, in bytes.
func (j *Journal) GetUsage() (uint64, error) {
	var out C.uint64_t

	sd_journal_get_usage, err := getFunction("sd_journal_get_usage")
	if err != nil {
		return 0, err
	}

	j.mu.Lock()
	r := C.my_sd_journal_get_usage(sd_journal_get_usage, j.cjournal, &out)
	j.mu.Unlock()

	if r < 0 {
		return 0, fmt.Errorf("failed to get journal disk space usage: %s", syscall.Errno(-r).Error())
	}

	return uint64(out), nil
}

// GetUniqueValues returns all unique values for a given field.
func (j *Journal) GetUniqueValues(field string) ([]string, error) {
	var result []string

	sd_journal_query_unique, err := getFunction("sd_journal_query_unique")
	if err != nil {
		return nil, err
	}

	sd_journal_enumerate_unique, err := getFunction("sd_journal_enumerate_unique")
	if err != nil {
		return nil, err
	}

	sd_journal_restart_unique, err := getFunction("sd_journal_restart_unique")
	if err != nil {
		return nil, err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	f := C.CString(field)
	defer C.free(unsafe.Pointer(f))

	r := C.my_sd_journal_query_unique(sd_journal_query_unique, j.cjournal, f)

	if r < 0 {
		return nil, fmt.Errorf("failed to query journal: %s", syscall.Errno(-r).Error())
	}

	// Implements the SD_JOURNAL_FOREACH_UNIQUE macro from sd-journal.h
	var d unsafe.Pointer
	var l C.size_t
	C.my_sd_journal_restart_unique(sd_journal_restart_unique, j.cjournal)
	for {
		r = C.my_sd_journal_enumerate_unique(sd_journal_enumerate_unique, j.cjournal, &d, &l)
		if r == 0 {
			break
		}

		if r < 0 {
			return nil, fmt.Errorf("failed to read message field: %s", syscall.Errno(-r).Error())
		}

		msg := C.GoStringN((*C.char)(d), C.int(l))
		kv := strings.SplitN(msg, "=", 2)
		if len(kv) < 2 {
			return nil, fmt.Errorf("failed to parse field")
		}

		result = append(result, kv[1])
	}

	return result, nil
}

// GetCatalog retrieves a message catalog entry for the journal entry referenced
// by the last completed Next/Previous function call. To call GetCatalog, you
// must first have called one of these functions.
func (j *Journal) GetCatalog() (string, error) {
	sd_journal_get_catalog, err := getFunction("sd_journal_get_catalog")
	if err != nil {
		return "", err
	}

	var c *C.char

	j.mu.Lock()
	r := C.my_sd_journal_get_catalog(sd_journal_get_catalog, j.cjournal, &c)
	j.mu.Unlock()
	defer C.free(unsafe.Pointer(c))

	if r < 0 {
		return "", fmt.Errorf("failed to retrieve catalog entry for current journal entry: %s", syscall.Errno(-r).Error())
	}

	catalog := C.GoString(c)

	return catalog, nil
}

// GetBootID get systemd boot id
func (j *Journal) GetBootID() (string, error) {
	sd_id128_get_boot, err := getFunction("sd_id128_get_boot")
	if err != nil {
		return "", err
	}

	var boot_id C.sd_id128_t
	r := C.my_sd_id128_get_boot(sd_id128_get_boot, &boot_id)
	if r < 0 {
		return "", fmt.Errorf("failed to get boot id: %s", syscall.Errno(-r).Error())
	}

	sd_id128_to_string, err := getFunction("sd_id128_to_string")
	if err != nil {
		return "", err
	}

	id128StringMax := C.size_t(C.SD_ID128_STRING_MAX)
	c := (*C.char)(C.malloc(id128StringMax))
	defer C.free(unsafe.Pointer(c))
	C.my_sd_id128_to_string(sd_id128_to_string, boot_id, c)

	bootID := C.GoString(c)
	if len(bootID) <= 0 {
		return "", fmt.Errorf("get boot id %s is not valid", bootID)
	}

	return bootID, nil
}
