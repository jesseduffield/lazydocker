//go:build windows

package lockfile

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const (
	reserved = 0
	allBytes = ^uint32(0)
)

// GetLastWrite returns a LastWrite value corresponding to current state of the lock.
// This is typically called before (_not after_) loading the state when initializing a consumer
// of the data protected by the lock.
// During the lifetime of the consumer, the consumer should usually call ModifiedSince instead.
//
// The caller must hold the lock (for reading or writing) before this function is called.
func (l *LockFile) GetLastWrite() (LastWrite, error) {
	l.AssertLocked()
	contents := make([]byte, lastWriterIDSize)
	ol := new(windows.Overlapped)
	var n uint32
	err := windows.ReadFile(windows.Handle(l.fd), contents, &n, ol)
	if err != nil && err != windows.ERROR_HANDLE_EOF {
		return LastWrite{}, err
	}
	// It is important to handle the partial read case, because
	// the initial size of the lock file is zero, which is a valid
	// state (no writes yet)
	contents = contents[:n]
	return newLastWriteFromData(contents), nil
}

// RecordWrite updates the lock with a new LastWrite value, and returns the new value.
//
// If this function fails, the LastWriter value of the lock is indeterminate;
// the caller should keep using the previously-recorded LastWrite value,
// and possibly detecting its own modification as an external one:
//
//	lw, err := state.lock.RecordWrite()
//	if err != nil { /* fail */ }
//	state.lastWrite = lw
//
// The caller must hold the lock for writing.
func (l *LockFile) RecordWrite() (LastWrite, error) {
	l.AssertLockedForWriting()
	lw := newLastWrite()
	lockContents := lw.serialize()
	ol := new(windows.Overlapped)
	var n uint32
	err := windows.WriteFile(windows.Handle(l.fd), lockContents, &n, ol)
	if err != nil {
		return LastWrite{}, err
	}
	if int(n) != len(lockContents) {
		return LastWrite{}, windows.ERROR_DISK_FULL
	}
	return lw, nil
}

// TouchedSince indicates if the lock file has been touched since the specified time
func (l *LockFile) TouchedSince(when time.Time) bool {
	stat, err := os.Stat(l.file)
	if err != nil {
		return true
	}
	return when.Before(stat.ModTime())
}
