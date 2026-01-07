//go:build linux && !cgo

package shm

import (
	"github.com/sirupsen/logrus"
)

// SHMLocks is a struct enabling POSIX semaphore locking in a shared memory
// segment.
type SHMLocks struct {
}

// CreateSHMLock sets up a shared-memory segment holding a given number of POSIX
// semaphores, and returns a struct that can be used to operate on those locks.
// numLocks must not be 0, and may be rounded up to a multiple of the bitmap
// size used by the underlying implementation.
func CreateSHMLock(path string, numLocks uint32) (*SHMLocks, error) {
	logrus.Error("Locks are not supported without cgo")
	return &SHMLocks{}, nil
}

// OpenSHMLock opens an existing shared-memory segment holding a given number of
// POSIX semaphores. numLocks must match the number of locks the shared memory
// segment was created with.
func OpenSHMLock(path string, numLocks uint32) (*SHMLocks, error) {
	logrus.Error("Locks are not supported without cgo")
	return &SHMLocks{}, nil
}

// GetMaxLocks returns the maximum number of locks in the SHM
func (locks *SHMLocks) GetMaxLocks() uint32 {
	logrus.Error("Locks are not supported without cgo")
	return 0
}

// Close closes an existing shared-memory segment.
// The segment will be rendered unusable after closing.
// WARNING: If you Close() while there are still locks locked, these locks may
// fail to release, causing a program freeze.
// Close() is only intended to be used while testing the locks.
func (locks *SHMLocks) Close() error {
	logrus.Error("Locks are not supported without cgo")
	return nil
}

// AllocateSemaphore allocates a semaphore from a shared-memory segment for use
// by a container or pod.
// Returns the index of the semaphore that was allocated.
// Allocations past the maximum number of locks given when the SHM segment was
// created will result in an error, and no semaphore will be allocated.
func (locks *SHMLocks) AllocateSemaphore() (uint32, error) {
	logrus.Error("Locks are not supported without cgo")
	return 0, nil
}

// AllocateGivenSemaphore allocates the given semaphore from the shared-memory
// segment for use by a container or pod.
// If the semaphore is already in use or the index is invalid an error will be
// returned.
func (locks *SHMLocks) AllocateGivenSemaphore(sem uint32) error {
	logrus.Error("Locks are not supported without cgo")
	return nil
}

// DeallocateSemaphore frees a semaphore in a shared-memory segment so it can be
// reallocated to another container or pod.
// The given semaphore must be already allocated, or an error will be returned.
func (locks *SHMLocks) DeallocateSemaphore(sem uint32) error {
	logrus.Error("Locks are not supported without cgo")
	return nil
}

// DeallocateAllSemaphores frees all semaphores so they can be reallocated to
// other containers and pods.
func (locks *SHMLocks) DeallocateAllSemaphores() error {
	logrus.Error("Locks are not supported without cgo")
	return nil
}

// LockSemaphore locks the given semaphore.
// If the semaphore is already locked, LockSemaphore will block until the lock
// can be acquired.
// There is no requirement that the given semaphore be allocated.
// This ensures that attempts to lock a container after it has been deleted,
// but before the caller has queried the database to determine this, will
// succeed.
func (locks *SHMLocks) LockSemaphore(sem uint32) error {
	logrus.Error("Locks are not supported without cgo")
	return nil
}

// UnlockSemaphore unlocks the given semaphore.
// Unlocking a semaphore that is already unlocked with return EBUSY.
// There is no requirement that the given semaphore be allocated.
// This ensures that attempts to lock a container after it has been deleted,
// but before the caller has queried the database to determine this, will
// succeed.
func (locks *SHMLocks) UnlockSemaphore(sem uint32) error {
	logrus.Error("Locks are not supported without cgo")
	return nil
}

// GetFreeLocks gets the number of locks available to be allocated.
func (locks *SHMLocks) GetFreeLocks() (uint32, error) {
	logrus.Error("Locks are not supported without cgo")
	return 0, nil
}

// Get a list of locks that are currently taken.
func (locks *SHMLocks) GetTakenLocks() ([]uint32, error) {
	logrus.Error("Locks are not supported without cgo")
	return nil, nil
}
