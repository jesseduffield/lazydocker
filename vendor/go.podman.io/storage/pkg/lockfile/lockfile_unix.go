//go:build !windows

package lockfile

import (
	"time"

	"go.podman.io/storage/pkg/system"
	"golang.org/x/sys/unix"
)

// GetLastWrite returns a LastWrite value corresponding to current state of the lock.
// This is typically called before (_not after_) loading the state when initializing a consumer
// of the data protected by the lock.
// During the lifetime of the consumer, the consumer should usually call ModifiedSince instead.
//
// The caller must hold the lock (for reading or writing).
func (l *LockFile) GetLastWrite() (LastWrite, error) {
	l.AssertLocked()
	contents := make([]byte, lastWriterIDSize)
	n, err := unix.Pread(int(l.fd), contents, 0)
	if err != nil {
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
	n, err := unix.Pwrite(int(l.fd), lockContents, 0)
	if err != nil {
		return LastWrite{}, err
	}
	if n != len(lockContents) {
		return LastWrite{}, unix.ENOSPC
	}
	return lw, nil
}

// TouchedSince indicates if the lock file has been touched since the specified time
func (l *LockFile) TouchedSince(when time.Time) bool {
	st, err := system.Fstat(int(l.fd))
	if err != nil {
		return true
	}
	mtim := st.Mtim()
	touched := time.Unix(mtim.Unix())
	return when.Before(touched)
}
