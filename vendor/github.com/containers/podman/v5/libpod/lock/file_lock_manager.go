package lock

import (
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/lock/file"
)

// FileLockManager manages shared memory locks.
type FileLockManager struct {
	locks *file.FileLocks
}

// NewFileLockManager makes a new FileLockManager at the specified directory.
func NewFileLockManager(lockPath string) (Manager, error) {
	locks, err := file.CreateFileLock(lockPath)
	if err != nil {
		return nil, err
	}

	manager := new(FileLockManager)
	manager.locks = locks

	return manager, nil
}

// OpenFileLockManager opens an existing FileLockManager at the specified directory.
func OpenFileLockManager(path string) (Manager, error) {
	locks, err := file.OpenFileLock(path)
	if err != nil {
		return nil, err
	}

	manager := new(FileLockManager)
	manager.locks = locks

	return manager, nil
}

// AllocateLock allocates a new lock from the manager.
func (m *FileLockManager) AllocateLock() (Locker, error) {
	semIndex, err := m.locks.AllocateLock()
	if err != nil {
		return nil, err
	}

	lock := new(FileLock)
	lock.lockID = semIndex
	lock.manager = m

	return lock, nil
}

// AllocateAndRetrieveLock allocates the lock with the given ID and returns it.
// If the lock is already allocated, error.
func (m *FileLockManager) AllocateAndRetrieveLock(id uint32) (Locker, error) {
	lock := new(FileLock)
	lock.lockID = id
	lock.manager = m

	if err := m.locks.AllocateGivenLock(id); err != nil {
		return nil, err
	}

	return lock, nil
}

// RetrieveLock retrieves a lock from the manager given its ID.
func (m *FileLockManager) RetrieveLock(id uint32) (Locker, error) {
	lock := new(FileLock)
	lock.lockID = id
	lock.manager = m

	return lock, nil
}

// FreeAllLocks frees all locks in the manager.
// This function is DANGEROUS. Please read the full comment in locks.go before
// trying to use it.
func (m *FileLockManager) FreeAllLocks() error {
	return m.locks.DeallocateAllLocks()
}

// AvailableLocks returns the number of available locks. Since this is not
// limited in the file lock implementation, nil is returned.
func (m *FileLockManager) AvailableLocks() (*uint32, error) {
	return nil, nil
}

// LocksHeld returns any locks that are presently locked.
// It is not implemented for the file lock backend.
// It ought to be possible, but my motivation to dig into c/storage and add
// trylock semantics to the filelocker implementation for an uncommonly-used
// lock backend is lacking.
func (m *FileLockManager) LocksHeld() ([]uint32, error) {
	return nil, define.ErrNotImplemented
}

// FileLock is an individual shared memory lock.
type FileLock struct {
	lockID  uint32
	manager *FileLockManager
}

// ID returns the ID of the lock.
func (l *FileLock) ID() uint32 {
	return l.lockID
}

// Lock acquires the lock.
func (l *FileLock) Lock() {
	if err := l.manager.locks.LockFileLock(l.lockID); err != nil {
		panic(err.Error())
	}
}

// Unlock releases the lock.
func (l *FileLock) Unlock() {
	if err := l.manager.locks.UnlockFileLock(l.lockID); err != nil {
		panic(err.Error())
	}
}

// Free releases the lock, allowing it to be reused.
func (l *FileLock) Free() error {
	return l.manager.locks.DeallocateLock(l.lockID)
}
