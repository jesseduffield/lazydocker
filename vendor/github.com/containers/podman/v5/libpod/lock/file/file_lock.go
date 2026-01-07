package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/lockfile"
)

// FileLocks is a struct enabling POSIX lock locking in a shared memory
// segment.
type FileLocks struct {
	lockPath string
	valid    bool
}

// CreateFileLock sets up a directory containing the various lock files.
func CreateFileLock(path string) (*FileLocks, error) {
	err := fileutils.Exists(path)
	if err == nil {
		return nil, fmt.Errorf("directory %s exists: %w", path, syscall.EEXIST)
	}
	if err := os.MkdirAll(path, 0o711); err != nil {
		return nil, err
	}

	locks := new(FileLocks)
	locks.lockPath = path
	locks.valid = true

	return locks, nil
}

// OpenFileLock opens an existing directory with the lock files.
func OpenFileLock(path string) (*FileLocks, error) {
	err := fileutils.Exists(path)
	if err != nil {
		return nil, err
	}

	locks := new(FileLocks)
	locks.lockPath = path
	locks.valid = true

	return locks, nil
}

// Close closes an existing shared-memory segment.
// The segment will be rendered unusable after closing.
// WARNING: If you Close() while there are still locks locked, these locks may
// fail to release, causing a program freeze.
// Close() is only intended to be used while testing the locks.
func (locks *FileLocks) Close() error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}
	err := os.RemoveAll(locks.lockPath)
	if err != nil {
		return fmt.Errorf("deleting directory %s: %w", locks.lockPath, err)
	}
	return nil
}

func (locks *FileLocks) getLockPath(lck uint32) string {
	return filepath.Join(locks.lockPath, strconv.FormatInt(int64(lck), 10))
}

// AllocateLock allocates a lock and returns the index of the lock that was allocated.
func (locks *FileLocks) AllocateLock() (uint32, error) {
	if !locks.valid {
		return 0, fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	id := uint32(0)
	for ; ; id++ {
		path := locks.getLockPath(id)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return 0, fmt.Errorf("creating lock file: %w", err)
		}
		f.Close()
		break
	}
	return id, nil
}

// AllocateGivenLock allocates the given lock from the shared-memory
// segment for use by a container or pod.
// If the lock is already in use or the index is invalid an error will be
// returned.
func (locks *FileLocks) AllocateGivenLock(lck uint32) error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	f, err := os.OpenFile(locks.getLockPath(lck), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return fmt.Errorf("creating lock %d: %w", lck, err)
	}
	f.Close()

	return nil
}

// DeallocateLock frees a lock in a shared-memory segment so it can be
// reallocated to another container or pod.
// The given lock must be already allocated, or an error will be returned.
func (locks *FileLocks) DeallocateLock(lck uint32) error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}
	if err := os.Remove(locks.getLockPath(lck)); err != nil {
		return fmt.Errorf("deallocating lock %d: %w", lck, err)
	}
	return nil
}

// DeallocateAllLocks frees all locks so they can be reallocated to
// other containers and pods.
func (locks *FileLocks) DeallocateAllLocks() error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}
	files, err := os.ReadDir(locks.lockPath)
	if err != nil {
		return fmt.Errorf("reading directory %s: %w", locks.lockPath, err)
	}
	var lastErr error
	for _, f := range files {
		p := filepath.Join(locks.lockPath, f.Name())
		err := os.Remove(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			logrus.Errorf("Deallocating lock %s", p)
		}
	}
	return lastErr
}

// LockFileLock locks the given lock.
func (locks *FileLocks) LockFileLock(lck uint32) error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}

	l, err := lockfile.GetLockFile(locks.getLockPath(lck))
	if err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}

	l.Lock()
	return nil
}

// UnlockFileLock unlocks the given lock.
func (locks *FileLocks) UnlockFileLock(lck uint32) error {
	if !locks.valid {
		return fmt.Errorf("locks have already been closed: %w", syscall.EINVAL)
	}
	l, err := lockfile.GetLockFile(locks.getLockPath(lck))
	if err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}

	l.Unlock()
	return nil
}
