//go:build linux

package lock

import (
	"fmt"
	"syscall"

	"github.com/containers/podman/v5/libpod/lock/shm"
)

// SHMLockManager manages shared memory locks.
type SHMLockManager struct {
	locks *shm.SHMLocks
}

// NewSHMLockManager makes a new SHMLockManager with the given number of locks.
// Due to the underlying implementation, the exact number of locks created may
// be greater than the number given here.
func NewSHMLockManager(path string, numLocks uint32) (Manager, error) {
	locks, err := shm.CreateSHMLock(path, numLocks)
	if err != nil {
		return nil, err
	}

	manager := new(SHMLockManager)
	manager.locks = locks

	return manager, nil
}

// OpenSHMLockManager opens an existing SHMLockManager with the given number of
// locks.
func OpenSHMLockManager(path string, numLocks uint32) (Manager, error) {
	locks, err := shm.OpenSHMLock(path, numLocks)
	if err != nil {
		return nil, err
	}

	manager := new(SHMLockManager)
	manager.locks = locks

	return manager, nil
}

// AllocateLock allocates a new lock from the manager.
func (m *SHMLockManager) AllocateLock() (Locker, error) {
	semIndex, err := m.locks.AllocateSemaphore()
	if err != nil {
		return nil, err
	}

	lock := new(SHMLock)
	lock.lockID = semIndex
	lock.manager = m

	return lock, nil
}

// AllocateAndRetrieveLock allocates the lock with the given ID and returns it.
// If the lock is already allocated, error.
func (m *SHMLockManager) AllocateAndRetrieveLock(id uint32) (Locker, error) {
	lock := new(SHMLock)
	lock.lockID = id
	lock.manager = m

	if id >= m.locks.GetMaxLocks() {
		return nil, fmt.Errorf("lock ID %d is too large - max lock size is %d: %w",
			id, m.locks.GetMaxLocks()-1, syscall.EINVAL)
	}

	if err := m.locks.AllocateGivenSemaphore(id); err != nil {
		return nil, err
	}

	return lock, nil
}

// RetrieveLock retrieves a lock from the manager given its ID.
func (m *SHMLockManager) RetrieveLock(id uint32) (Locker, error) {
	lock := new(SHMLock)
	lock.lockID = id
	lock.manager = m

	if id >= m.locks.GetMaxLocks() {
		return nil, fmt.Errorf("lock ID %d is too large - max lock size is %d: %w",
			id, m.locks.GetMaxLocks()-1, syscall.EINVAL)
	}

	return lock, nil
}

// FreeAllLocks frees all locks in the manager.
// This function is DANGEROUS. Please read the full comment in locks.go before
// trying to use it.
func (m *SHMLockManager) FreeAllLocks() error {
	return m.locks.DeallocateAllSemaphores()
}

// AvailableLocks returns the number of free locks in the manager.
func (m *SHMLockManager) AvailableLocks() (*uint32, error) {
	avail, err := m.locks.GetFreeLocks()
	if err != nil {
		return nil, err
	}

	return &avail, nil
}

func (m *SHMLockManager) LocksHeld() ([]uint32, error) {
	return m.locks.GetTakenLocks()
}

// SHMLock is an individual shared memory lock.
type SHMLock struct {
	lockID  uint32
	manager *SHMLockManager
}

// ID returns the ID of the lock.
func (l *SHMLock) ID() uint32 {
	return l.lockID
}

// Lock acquires the lock.
func (l *SHMLock) Lock() {
	if err := l.manager.locks.LockSemaphore(l.lockID); err != nil {
		panic(err.Error())
	}
}

// Unlock releases the lock.
func (l *SHMLock) Unlock() {
	if err := l.manager.locks.UnlockSemaphore(l.lockID); err != nil {
		panic(err.Error())
	}
}

// Free releases the lock, allowing it to be reused.
func (l *SHMLock) Free() error {
	return l.manager.locks.DeallocateSemaphore(l.lockID)
}
