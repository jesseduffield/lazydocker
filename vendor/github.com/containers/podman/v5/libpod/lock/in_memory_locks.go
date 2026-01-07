package lock

import (
	"errors"
	"fmt"
	"sync"
)

// Mutex holds a single mutex and whether it has been allocated.
type Mutex struct {
	id        uint32
	lock      sync.Mutex
	allocated bool
}

// ID retrieves the ID of the mutex
func (m *Mutex) ID() uint32 {
	return m.id
}

// Lock locks the mutex
func (m *Mutex) Lock() {
	m.lock.Lock()
}

// Unlock unlocks the mutex
func (m *Mutex) Unlock() {
	m.lock.Unlock()
}

// Free deallocates the mutex to allow its reuse
func (m *Mutex) Free() error {
	m.allocated = false

	return nil
}

// InMemoryManager is a lock manager that allocates and retrieves local-only
// locks - that is, they are not multiprocess. This lock manager is intended
// purely for unit and integration testing and should not be used in production
// deployments.
type InMemoryManager struct {
	locks     []*Mutex
	numLocks  uint32
	localLock sync.Mutex
}

// NewInMemoryManager creates a new in-memory lock manager with the given number
// of locks.
func NewInMemoryManager(numLocks uint32) (Manager, error) {
	if numLocks == 0 {
		return nil, errors.New("must provide a non-zero number of locks")
	}

	manager := new(InMemoryManager)
	manager.numLocks = numLocks
	manager.locks = make([]*Mutex, numLocks)

	var i uint32
	for i = range numLocks {
		lock := new(Mutex)
		lock.id = i
		manager.locks[i] = lock
	}

	return manager, nil
}

// AllocateLock allocates a lock from the manager.
func (m *InMemoryManager) AllocateLock() (Locker, error) {
	m.localLock.Lock()
	defer m.localLock.Unlock()

	for _, lock := range m.locks {
		if !lock.allocated {
			lock.allocated = true
			return lock, nil
		}
	}

	return nil, errors.New("all locks have been allocated")
}

// RetrieveLock retrieves a lock from the manager.
func (m *InMemoryManager) RetrieveLock(id uint32) (Locker, error) {
	if id >= m.numLocks {
		return nil, fmt.Errorf("given lock ID %d is too large - this manager only supports lock indexes up to %d", id, m.numLocks-1)
	}

	return m.locks[id], nil
}

// AllocateAndRetrieveLock allocates a lock with the given ID (if not already in
// use) and returns it.
func (m *InMemoryManager) AllocateAndRetrieveLock(id uint32) (Locker, error) {
	if id >= m.numLocks {
		return nil, fmt.Errorf("given lock ID %d is too large - this manager only supports lock indexes up to %d", id, m.numLocks)
	}

	if m.locks[id].allocated {
		return nil, fmt.Errorf("given lock ID %d is already in use, cannot reallocate", id)
	}

	m.locks[id].allocated = true

	return m.locks[id], nil
}

// FreeAllLocks frees all locks.
// This function is DANGEROUS. Please read the full comment in locks.go before
// trying to use it.
func (m *InMemoryManager) FreeAllLocks() error {
	for _, lock := range m.locks {
		lock.allocated = false
	}

	return nil
}

// Get number of available locks
func (m *InMemoryManager) AvailableLocks() (*uint32, error) {
	var count uint32

	for _, lock := range m.locks {
		if !lock.allocated {
			count++
		}
	}

	return &count, nil
}

// Get any locks that are presently being held.
// Useful for debugging deadlocks.
func (m *InMemoryManager) LocksHeld() ([]uint32, error) {
	//nolint:prealloc
	var locks []uint32

	for _, lock := range m.locks {
		if lock.lock.TryLock() {
			lock.lock.Unlock()
			continue
		}
		locks = append(locks, lock.ID())
	}

	return locks, nil
}
