package lock

// Manager provides an interface for allocating multiprocess locks.
// Locks returned by Manager MUST be multiprocess - allocating a lock in
// process A and retrieving that lock's ID in process B must return handles for
// the same lock, and locking the lock in A should exclude B from the lock until
// it is unlocked in A.
// All locks must be identified by a UUID (retrieved with Locker's ID() method).
// All locks with a given UUID must refer to the same underlying lock, and it
// must be possible to retrieve the lock given its UUID.
// Each UUID should refer to a unique underlying lock.
// Calls to AllocateLock() must return a unique, unallocated UUID.
// AllocateLock() must fail once all available locks have been allocated.
// Locks are returned to use by calls to Free(), and can subsequently be
// reallocated.
type Manager interface {
	// AllocateLock returns an unallocated lock.
	// It is guaranteed that the same lock will not be returned again by
	// AllocateLock until the returned lock has Free() called on it.
	// If all available locks are allocated, AllocateLock will return an
	// error.
	AllocateLock() (Locker, error)
	// RetrieveLock retrieves a lock given its UUID.
	// The underlying lock MUST be the same as another other lock with the
	// same UUID.
	RetrieveLock(id uint32) (Locker, error)
	// AllocateAndRetrieveLock marks the lock with the given UUID as in use
	// and retrieves it.
	// RetrieveAndAllocateLock will error if the lock in question has
	// already been allocated.
	// This is mostly used after a system restart to repopulate the list of
	// locks in use.
	AllocateAndRetrieveLock(id uint32) (Locker, error)
	// PLEASE READ FULL DESCRIPTION BEFORE USING.
	// FreeAllLocks frees all allocated locks, in preparation for lock
	// reallocation.
	// As this deallocates all presently-held locks, this can be very
	// dangerous - if there are other processes running that might be
	// attempting to allocate new locks and free existing locks, we may
	// encounter races leading to an inconsistent state.
	// (This is in addition to the fact that FreeAllLocks instantly makes
	// the state inconsistent simply by using it, and requires a full
	// lock renumbering to restore consistency!).
	// In short, this should only be used as part of unit tests, or lock
	// renumbering, where reasonable guarantees about other processes can be
	// made.
	FreeAllLocks() error
	// NumAvailableLocks gets the number of remaining locks available to be
	// allocated.
	// Some lock managers do not have a maximum number of locks, and can
	// allocate an unlimited number. These implementations should return
	// a nil uin32.
	AvailableLocks() (*uint32, error)
	// Get a list of locks that are currently locked.
	// This may not be supported by some drivers, depending on the exact
	// backend implementation in use.
	LocksHeld() ([]uint32, error)
}

// Locker is similar to sync.Locker, but provides a method for freeing the lock
// to allow its reuse.
// All Locker implementations must maintain mutex semantics - the lock only
// allows one caller in the critical section at a time.
// All locks with the same ID must refer to the same underlying lock, even
// if they are within multiple processes.
type Locker interface {
	// ID retrieves the lock's ID.
	// ID is guaranteed to uniquely identify the lock within the
	// Manager - that is, calling RetrieveLock with this ID will return
	// another instance of the same lock.
	ID() uint32
	// Lock locks the lock.
	// This call MUST block until it successfully acquires the lock or
	// encounters a fatal error.
	// All errors must be handled internally, as they are not returned. For
	// the most part, panicking should be appropriate.
	// Some lock implementations may require that Lock() and Unlock() occur
	// within the same goroutine (SHM locking, for example). The usual Go
	// Lock()/defer Unlock() pattern will still work fine in these cases.
	Lock()
	// Unlock unlocks the lock.
	// All errors must be handled internally, as they are not returned. For
	// the most part, panicking should be appropriate.
	// This includes unlocking locks which are already unlocked.
	Unlock()
	// Free deallocates the underlying lock, allowing its reuse by other
	// pods and containers.
	// The lock MUST still be usable after a Free() - some libpod instances
	// may still retain Container structs with the old lock. This simply
	// advises the manager that the lock may be reallocated.
	Free() error
}
