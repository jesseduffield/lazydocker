package rawfilelock

import (
	"os"
)

type LockType byte

const (
	ReadLock LockType = iota
	WriteLock
)

type FileHandle = fileHandle

// OpenLock opens a file for locking
// WARNING: This is the underlying file locking primitive of the OS;
// because closing FileHandle releases the lock, it is not suitable for use
// if there is any chance of two concurrent goroutines attempting to use the same lock.
// Most users should use the higher-level operations from internal/staging_lockfile or pkg/lockfile.
func OpenLock(path string, readOnly bool) (FileHandle, error) {
	flags := os.O_CREATE
	if readOnly {
		flags |= os.O_RDONLY
	} else {
		flags |= os.O_RDWR
	}

	fd, err := openHandle(path, flags)
	if err == nil {
		return fd, nil
	}

	return fd, &os.PathError{Op: "open", Path: path, Err: err}
}

// TryLockFile attempts to lock a file handle
func TryLockFile(fd FileHandle, lockType LockType) error {
	return lockHandle(fd, lockType, true)
}

// LockFile locks a file handle
func LockFile(fd FileHandle, lockType LockType) error {
	return lockHandle(fd, lockType, false)
}

// UnlockAndClose unlocks and closes a file handle
func UnlockAndCloseHandle(fd FileHandle) {
	unlockAndCloseHandle(fd)
}

// CloseHandle closes a file handle without unlocking
//
// WARNING: This is a last-resort function for error handling only!
// On Unix systems, closing a file descriptor automatically releases any locks,
// so "closing without unlocking" is impossible. This function will release
// the lock as a side effect of closing the file.
//
// This function should only be used in error paths where the lock state
// is already corrupted or when giving up on lock management entirely.
// Normal code should use UnlockAndCloseHandle instead.
func CloseHandle(fd FileHandle) {
	closeHandle(fd)
}
