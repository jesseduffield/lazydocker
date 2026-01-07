//go:build (linux || freebsd) && cgo

package shm

// #cgo LDFLAGS: -lrt -lpthread
// #cgo CFLAGS: -Wall -Werror
// #include <stdlib.h>
// #include <sys/types.h>
// #include <sys/mman.h>
// #include <fcntl.h>
// #include "shm_lock.h"
// const uint32_t bitmap_size_c = BITMAP_SIZE;
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/sirupsen/logrus"
)

var (
	// BitmapSize is the size of the bitmap used when managing SHM locks.
	// an SHM lock manager's max locks will be rounded up to a multiple of
	// this number.
	BitmapSize = uint32(C.bitmap_size_c)
)

// SHMLocks is a struct enabling POSIX semaphore locking in a shared memory
// segment.
type SHMLocks struct {
	lockStruct *C.shm_struct_t
	maxLocks   uint32
	valid      bool
}

// CreateSHMLock sets up a shared-memory segment holding a given number of POSIX
// semaphores, and returns a struct that can be used to operate on those locks.
// numLocks must not be 0, and may be rounded up to a multiple of the bitmap
// size used by the underlying implementation.
func CreateSHMLock(path string, numLocks uint32) (*SHMLocks, error) {
	if numLocks == 0 {
		return nil, fmt.Errorf("number of locks must be greater than 0: %w", syscall.EINVAL)
	}

	locks := new(SHMLocks)

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var errCode C.int
	lockStruct := C.setup_lock_shm(cPath, C.uint32_t(numLocks), &errCode)
	if lockStruct == nil {
		// We got a null pointer, so something errored
		return nil, fmt.Errorf("failed to create %d locks in %s: %w", numLocks, path, syscall.Errno(-1*errCode))
	}

	locks.lockStruct = lockStruct
	locks.maxLocks = uint32(lockStruct.num_locks)
	locks.valid = true

	logrus.Debugf("Initialized SHM lock manager at path %s", path)

	return locks, nil
}

// OpenSHMLock opens an existing shared-memory segment holding a given number of
// POSIX semaphores. numLocks must match the number of locks the shared memory
// segment was created with.
func OpenSHMLock(path string, numLocks uint32) (*SHMLocks, error) {
	if numLocks == 0 {
		return nil, fmt.Errorf("number of locks must be greater than 0: %w", syscall.EINVAL)
	}

	locks := new(SHMLocks)

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var errCode C.int
	lockStruct := C.open_lock_shm(cPath, C.uint32_t(numLocks), &errCode)
	if lockStruct == nil {
		// We got a null pointer, so something errored
		return nil, fmt.Errorf("failed to open %d locks in %s: %w", numLocks, path, syscall.Errno(-1*errCode))
	}

	locks.lockStruct = lockStruct
	locks.maxLocks = numLocks
	locks.valid = true

	return locks, nil
}

// GetMaxLocks returns the maximum number of locks in the SHM
func (locks *SHMLocks) GetMaxLocks() uint32 {
	return locks.maxLocks
}

// Close closes an existing shared-memory segment.
// The segment will be rendered unusable after closing.
// WARNING: If you Close() while there are still locks locked, these locks may
// fail to release, causing a program freeze.
// Close() is only intended to be used while testing the locks.
func (locks *SHMLocks) Close() error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	locks.valid = false

	retCode := C.close_lock_shm(locks.lockStruct)
	if retCode < 0 {
		// Negative errno returned
		return syscall.Errno(-1 * retCode)
	}

	return nil
}

// AllocateSemaphore allocates a semaphore from a shared-memory segment for use
// by a container or pod.
// Returns the index of the semaphore that was allocated.
// Allocations past the maximum number of locks given when the SHM segment was
// created will result in an error, and no semaphore will be allocated.
func (locks *SHMLocks) AllocateSemaphore() (uint32, error) {
	if !locks.valid {
		return 0, fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	// This returns a U64, so we have the full u32 range available for
	// semaphore indexes, and can still return error codes.
	retCode := C.allocate_semaphore(locks.lockStruct)
	if retCode < 0 {
		var err = syscall.Errno(-1 * retCode)
		// Negative errno returned
		if errors.Is(err, syscall.ENOSPC) {
			// ENOSPC expands to "no space left on device".  While it is technically true
			// that there's no room in the SHM inn for this lock, this tends to send normal people
			// down the path of checking disk-space which is not actually their problem.
			// Give a clue that it's actually due to num_locks filling up.
			var errFull = fmt.Errorf("allocation failed; exceeded num_locks (%d)", locks.maxLocks)
			return uint32(retCode), errFull
		}
		return uint32(retCode), syscall.Errno(-1 * retCode)
	}

	return uint32(retCode), nil
}

// AllocateGivenSemaphore allocates the given semaphore from the shared-memory
// segment for use by a container or pod.
// If the semaphore is already in use or the index is invalid an error will be
// returned.
func (locks *SHMLocks) AllocateGivenSemaphore(sem uint32) error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	retCode := C.allocate_given_semaphore(locks.lockStruct, C.uint32_t(sem))
	if retCode < 0 {
		return syscall.Errno(-1 * retCode)
	}

	return nil
}

// DeallocateSemaphore frees a semaphore in a shared-memory segment so it can be
// reallocated to another container or pod.
// The given semaphore must be already allocated, or an error will be returned.
func (locks *SHMLocks) DeallocateSemaphore(sem uint32) error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	if sem > locks.maxLocks {
		return fmt.Errorf("given semaphore %d is higher than maximum locks count %d: %w", sem, locks.maxLocks, syscall.EINVAL)
	}

	retCode := C.deallocate_semaphore(locks.lockStruct, C.uint32_t(sem))
	if retCode < 0 {
		// Negative errno returned
		return syscall.Errno(-1 * retCode)
	}

	return nil
}

// DeallocateAllSemaphores frees all semaphores so they can be reallocated to
// other containers and pods.
func (locks *SHMLocks) DeallocateAllSemaphores() error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	retCode := C.deallocate_all_semaphores(locks.lockStruct)
	if retCode < 0 {
		// Negative errno return from C
		return syscall.Errno(-1 * retCode)
	}

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
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	if sem > locks.maxLocks {
		return fmt.Errorf("given semaphore %d is higher than maximum locks count %d: %w", sem, locks.maxLocks, syscall.EINVAL)
	}

	// For pthread mutexes, we have to guarantee lock and unlock happen in
	// the same thread.
	runtime.LockOSThread()

	retCode := C.lock_semaphore(locks.lockStruct, C.uint32_t(sem))
	if retCode < 0 {
		// Negative errno returned
		return syscall.Errno(-1 * retCode)
	}

	return nil
}

// UnlockSemaphore unlocks the given semaphore.
// Unlocking a semaphore that is already unlocked with return EBUSY.
// There is no requirement that the given semaphore be allocated.
// This ensures that attempts to lock a container after it has been deleted,
// but before the caller has queried the database to determine this, will
// succeed.
func (locks *SHMLocks) UnlockSemaphore(sem uint32) error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	if sem > locks.maxLocks {
		return fmt.Errorf("given semaphore %d is higher than maximum locks count %d: %w", sem, locks.maxLocks, syscall.EINVAL)
	}

	retCode := C.unlock_semaphore(locks.lockStruct, C.uint32_t(sem))
	if retCode < 0 {
		// Negative errno returned
		return syscall.Errno(-1 * retCode)
	}

	// For pthread mutexes, we have to guarantee lock and unlock happen in
	// the same thread.
	// OK if we take multiple locks - UnlockOSThread() won't actually unlock
	// until the number of calls equals the number of calls to
	// LockOSThread()
	runtime.UnlockOSThread()

	return nil
}

// GetFreeLocks gets the number of locks available to be allocated.
func (locks *SHMLocks) GetFreeLocks() (uint32, error) {
	if !locks.valid {
		return 0, fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	retCode := C.available_locks(locks.lockStruct)
	if retCode < 0 {
		// Negative errno returned
		return 0, syscall.Errno(-1 * retCode)
	}

	return uint32(retCode), nil
}

// Get a list of locks that are currently taken.
func (locks *SHMLocks) GetTakenLocks() ([]uint32, error) {
	if !locks.valid {
		return nil, fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	var usedLocks []uint32

	// I don't think we need to lock the OS thread here, since the lock (if
	// taken) is immediately released, and Go shouldn't reschedule the CGo
	// to another thread before the function finished executing.
	var i uint32
	for i = 0; i < locks.maxLocks; i++ {
		retCode := C.try_lock(locks.lockStruct, C.uint32_t(i))
		if retCode < 0 {
			return nil, syscall.Errno(-1 * retCode)
		}
		if retCode == 0 {
			usedLocks = append(usedLocks, i)
		}
	}

	return usedLocks, nil
}

func unlinkSHMLock(path string) error {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	if _, err := C.shm_unlink(cPath); err != nil {
		return fmt.Errorf("failed to unlink SHM locks: %w", err)
	}
	return nil
}
