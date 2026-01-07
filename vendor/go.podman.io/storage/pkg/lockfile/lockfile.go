package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.podman.io/storage/internal/rawfilelock"
)

// A Locker represents a file lock where the file is used to cache an
// identifier of the last party that made changes to whatever's being protected
// by the lock.
//
// Deprecated: Refer directly to *LockFile, the provided implementation, instead.
type Locker interface {
	// Acquire a writer lock.
	// The default unix implementation panics if:
	// - opening the lockfile failed
	// - tried to lock a read-only lock-file
	Lock()

	// Unlock the lock.
	// The default unix implementation panics if:
	// - unlocking an unlocked lock
	// - if the lock counter is corrupted
	Unlock()

	// Acquire a reader lock.
	RLock()

	// Touch records, for others sharing the lock, that the caller was the
	// last writer.  It should only be called with the lock held.
	//
	// Deprecated: Use *LockFile.RecordWrite.
	Touch() error

	// Modified() checks if the most recent writer was a party other than the
	// last recorded writer.  It should only be called with the lock held.
	// Deprecated: Use *LockFile.ModifiedSince.
	Modified() (bool, error)

	// TouchedSince() checks if the most recent writer modified the file (likely using Touch()) after the specified time.
	TouchedSince(when time.Time) bool

	// IsReadWrite() checks if the lock file is read-write
	IsReadWrite() bool

	// AssertLocked() can be used by callers that _know_ that they hold the lock (for reading or writing), for sanity checking.
	// It might do nothing at all, or it may panic if the caller is not the owner of this lock.
	AssertLocked()

	// AssertLockedForWriting() can be used by callers that _know_ that they hold the lock locked for writing, for sanity checking.
	// It might do nothing at all, or it may panic if the caller is not the owner of this lock for writing.
	AssertLockedForWriting()
}

// LockFile represents a file lock where the file is used to cache an
// identifier of the last party that made changes to whatever's being protected
// by the lock.
//
// It MUST NOT be created manually. Use GetLockFile or GetROLockFile instead.
type LockFile struct {
	// The following fields are only set when constructing *LockFile, and must never be modified afterwards.
	// They are safe to access without any other locking.
	file string
	ro   bool

	// rwMutex serializes concurrent reader-writer acquisitions in the same process space
	rwMutex *sync.RWMutex
	// stateMutex is used to synchronize concurrent accesses to the state below
	stateMutex *sync.Mutex
	counter    int64
	lw         LastWrite // A global value valid as of the last .Touch() or .Modified()
	lockType   rawfilelock.LockType
	locked     bool
	// The following fields are only modified on transitions between counter == 0 / counter != 0.
	// Thus, they can be safely accessed by users _that currently hold the LockFile_ without locking.
	// In other cases, they need to be protected using stateMutex.
	fd rawfilelock.FileHandle
}

var (
	lockFiles     map[string]*LockFile
	lockFilesLock sync.Mutex
)

// GetLockFile opens a read-write lock file, creating it if necessary.  The
// *LockFile object may already be locked if the path has already been requested
// by the current process.
func GetLockFile(path string) (*LockFile, error) {
	return getLockfile(path, false)
}

// GetLockfile opens a read-write lock file, creating it if necessary.  The
// Locker object may already be locked if the path has already been requested
// by the current process.
//
// Deprecated: Use GetLockFile
func GetLockfile(path string) (Locker, error) {
	return GetLockFile(path)
}

// GetROLockFile opens a read-only lock file, creating it if necessary.  The
// *LockFile object may already be locked if the path has already been requested
// by the current process.
func GetROLockFile(path string) (*LockFile, error) {
	return getLockfile(path, true)
}

// GetROLockfile opens a read-only lock file, creating it if necessary.  The
// Locker object may already be locked if the path has already been requested
// by the current process.
//
// Deprecated: Use GetROLockFile
func GetROLockfile(path string) (Locker, error) {
	return GetROLockFile(path)
}

// Lock locks the lockfile as a writer.  Panic if the lock is a read-only one.
func (l *LockFile) Lock() {
	if l.ro {
		panic("can't take write lock on read-only lock file")
	}
	l.lock(rawfilelock.WriteLock)
}

// RLock locks the lockfile as a reader.
func (l *LockFile) RLock() {
	l.lock(rawfilelock.ReadLock)
}

// TryLock attempts to lock the lockfile as a writer.  Panic if the lock is a read-only one.
func (l *LockFile) TryLock() error {
	if l.ro {
		panic("can't take write lock on read-only lock file")
	}
	return l.tryLock(rawfilelock.WriteLock)
}

// TryRLock attempts to lock the lockfile as a reader.
func (l *LockFile) TryRLock() error {
	return l.tryLock(rawfilelock.ReadLock)
}

// Unlock unlocks the lockfile.
func (l *LockFile) Unlock() {
	l.stateMutex.Lock()
	if !l.locked {
		// Panic when unlocking an unlocked lock.  That's a violation
		// of the lock semantics and will reveal such.
		panic("calling Unlock on unlocked lock")
	}
	l.counter--
	if l.counter < 0 {
		// Panic when the counter is negative.  There is no way we can
		// recover from a corrupted lock and we need to protect the
		// storage from corruption.
		panic(fmt.Sprintf("lock %q has been unlocked too often", l.file))
	}
	if l.counter == 0 {
		// We should only release the lock when the counter is 0 to
		// avoid releasing read-locks too early; a given process may
		// acquire a read lock multiple times.
		l.locked = false
		// Close the file descriptor on the last unlock, releasing the
		// file lock.
		rawfilelock.UnlockAndCloseHandle(l.fd)
	}
	if l.lockType == rawfilelock.ReadLock {
		l.rwMutex.RUnlock()
	} else {
		l.rwMutex.Unlock()
	}
	l.stateMutex.Unlock()
}

func (l *LockFile) AssertLocked() {
	// DO NOT provide a variant that returns the value of l.locked.
	//
	// If the caller does not hold the lock, l.locked might nevertheless be true because another goroutine does hold it, and
	// we can’t tell the difference.
	//
	// Hence, this “AssertLocked” method, which exists only for sanity checks.

	// Don’t even bother with l.stateMutex: The caller is expected to hold the lock, and in that case l.locked is constant true
	// with no possible writers.
	// If the caller does not hold the lock, we are violating the locking/memory model anyway, and accessing the data
	// without the lock is more efficient for callers, and potentially more visible to lock analysers for incorrect callers.
	if !l.locked {
		panic("internal error: lock is not held by the expected owner")
	}
}

func (l *LockFile) AssertLockedForWriting() {
	// DO NOT provide a variant that returns the current lock state.
	//
	// The same caveats as for AssertLocked apply equally.

	l.AssertLocked()
	// Like AssertLocked, don’t even bother with l.stateMutex.
	if l.lockType == rawfilelock.ReadLock {
		panic("internal error: lock is not held for writing")
	}
}

// ModifiedSince checks if the lock has been changed since a provided LastWrite value,
// and returns the one to record instead.
//
// If ModifiedSince reports no modification, the previous LastWrite value
// is still valid and can continue to be used.
//
// If this function fails, the LastWriter value of the lock is indeterminate;
// the caller should fail and keep using the previously-recorded LastWrite value,
// so that it continues failing until the situation is resolved. Similarly,
// it should only update the recorded LastWrite value after processing the update:
//
//	lw2, modified, err := state.lock.ModifiedSince(state.lastWrite)
//	if err != nil { /* fail */ }
//	state.lastWrite = lw2
//	if modified {
//		if err := reload(); err != nil { /* fail */ }
//		state.lastWrite = lw2
//	}
//
// The caller must hold the lock (for reading or writing).
func (l *LockFile) ModifiedSince(previous LastWrite) (LastWrite, bool, error) {
	l.AssertLocked()
	currentLW, err := l.GetLastWrite()
	if err != nil {
		return LastWrite{}, false, err
	}
	modified := !previous.equals(currentLW)
	return currentLW, modified, nil
}

// Modified indicates if the lockfile has been updated since the last time it
// was loaded.
// NOTE: Unlike ModifiedSince, this returns true the first time it is called on a *LockFile.
// Callers cannot, in general, rely on this, because that might have happened for some other
// owner of the same *LockFile who created it previously.
//
// Deprecated: Use *LockFile.ModifiedSince.
func (l *LockFile) Modified() (bool, error) {
	l.stateMutex.Lock()
	if !l.locked {
		panic("attempted to check last-writer in lockfile without locking it first")
	}
	defer l.stateMutex.Unlock()
	oldLW := l.lw
	// Note that this is called with stateMutex held; that’s fine because ModifiedSince doesn’t need to lock it.
	currentLW, modified, err := l.ModifiedSince(oldLW)
	if err != nil {
		return true, err
	}
	l.lw = currentLW
	return modified, nil
}

// Touch updates the lock file with to record that the current lock holder has modified the lock-protected data.
//
// Deprecated: Use *LockFile.RecordWrite.
func (l *LockFile) Touch() error {
	lw, err := l.RecordWrite()
	if err != nil {
		return err
	}
	l.stateMutex.Lock()
	if !l.locked || (l.lockType == rawfilelock.ReadLock) {
		panic("attempted to update last-writer in lockfile without the write lock")
	}
	defer l.stateMutex.Unlock()
	l.lw = lw
	return nil
}

// IsReadWrite indicates if the lock file is a read-write lock.
func (l *LockFile) IsReadWrite() bool {
	return !l.ro
}

// getLockFile returns a *LockFile object, possibly (depending on the platform)
// working inter-process, and associated with the specified path.
//
// If ro, the lock is a read-write lock and the returned *LockFile should correspond to the
// “lock for reading” (shared) operation; otherwise, the lock is either an exclusive lock,
// or a read-write lock and *LockFile should correspond to the “lock for writing” (exclusive) operation.
//
// WARNING:
// - The lock may or MAY NOT be inter-process.
// - There may or MAY NOT be an actual object on the filesystem created for the specified path.
// - Even if ro, the lock MAY be exclusive.
func getLockfile(path string, ro bool) (*LockFile, error) {
	lockFilesLock.Lock()
	defer lockFilesLock.Unlock()
	if lockFiles == nil {
		lockFiles = make(map[string]*LockFile)
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("ensuring that path %q is an absolute path: %w", path, err)
	}
	if lockFile, ok := lockFiles[cleanPath]; ok {
		if ro && lockFile.IsReadWrite() {
			return nil, fmt.Errorf("lock %q is not a read-only lock", cleanPath)
		}
		if !ro && !lockFile.IsReadWrite() {
			return nil, fmt.Errorf("lock %q is not a read-write lock", cleanPath)
		}
		return lockFile, nil
	}
	lockFile, err := createLockFileForPath(cleanPath, ro) // platform-dependent LockFile
	if err != nil {
		return nil, err
	}
	lockFiles[cleanPath] = lockFile
	return lockFile, nil
}

// openLock opens a lock file at the specified path, creating the parent directory if it does not exist.
func openLock(path string, readOnly bool) (rawfilelock.FileHandle, error) {
	fd, err := rawfilelock.OpenLock(path, readOnly)
	if err == nil {
		return fd, nil
	}

	// the directory of the lockfile seems to be removed, try to create it
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fd, fmt.Errorf("creating lock file directory: %w", err)
		}

		return openLock(path, readOnly)
	}
	return fd, &os.PathError{Op: "open", Path: path, Err: err}
}

// createLockFileForPath returns new *LockFile object, possibly (depending on the platform)
// working inter-process and associated with the specified path.
//
// This function will be called at most once for each path value within a single process.
//
// If ro, the lock is a read-write lock and the returned *LockFile should correspond to the
// “lock for reading” (shared) operation; otherwise, the lock is either an exclusive lock,
// or a read-write lock and *LockFile should correspond to the “lock for writing” (exclusive) operation.
//
// WARNING:
// - The lock may or MAY NOT be inter-process.
// - There may or MAY NOT be an actual object on the filesystem created for the specified path.
// - Even if ro, the lock MAY be exclusive.
func createLockFileForPath(path string, ro bool) (*LockFile, error) {
	// Check if we can open the lock.
	fd, err := openLock(path, ro)
	if err != nil {
		return nil, err
	}
	rawfilelock.UnlockAndCloseHandle(fd)

	lType := rawfilelock.WriteLock
	if ro {
		lType = rawfilelock.ReadLock
	}

	return &LockFile{
		file: path,
		ro:   ro,

		rwMutex:    &sync.RWMutex{},
		stateMutex: &sync.Mutex{},
		lw:         newLastWrite(), // For compatibility, the first call of .Modified() will always report a change.
		lockType:   lType,
		locked:     false,
	}, nil
}

// lock locks the lockfile via syscall based on the specified type and
// command.
func (l *LockFile) lock(lType rawfilelock.LockType) {
	if lType == rawfilelock.ReadLock {
		l.rwMutex.RLock()
	} else {
		l.rwMutex.Lock()
	}
	l.stateMutex.Lock()
	defer l.stateMutex.Unlock()
	if l.counter == 0 {
		// If we're the first reference on the lock, we need to open the file again.
		fd, err := openLock(l.file, l.ro)
		if err != nil {
			panic(err)
		}
		l.fd = fd

		// Optimization: only use the (expensive) syscall when
		// the counter is 0.  In this case, we're either the first
		// reader lock or a writer lock.
		if err := rawfilelock.LockFile(l.fd, lType); err != nil {
			panic(err)
		}
	}
	l.lockType = lType
	l.locked = true
	l.counter++
}

// lock locks the lockfile via syscall based on the specified type and
// command.
func (l *LockFile) tryLock(lType rawfilelock.LockType) error {
	var success bool
	var rwMutexUnlocker func()
	if lType == rawfilelock.ReadLock {
		success = l.rwMutex.TryRLock()
		rwMutexUnlocker = l.rwMutex.RUnlock
	} else {
		success = l.rwMutex.TryLock()
		rwMutexUnlocker = l.rwMutex.Unlock
	}
	if !success {
		return fmt.Errorf("resource temporarily unavailable")
	}
	l.stateMutex.Lock()
	defer l.stateMutex.Unlock()
	if l.counter == 0 {
		// If we're the first reference on the lock, we need to open the file again.
		fd, err := openLock(l.file, l.ro)
		if err != nil {
			rwMutexUnlocker()
			return err
		}
		l.fd = fd

		// Optimization: only use the (expensive) syscall when
		// the counter is 0.  In this case, we're either the first
		// reader lock or a writer lock.
		if err = rawfilelock.TryLockFile(l.fd, lType); err != nil {
			rawfilelock.CloseHandle(fd)
			rwMutexUnlocker()
			return err
		}
	}
	l.lockType = lType
	l.locked = true
	l.counter++
	return nil
}
