package storage

import (
	"go.podman.io/storage/pkg/lockfile"
)

// Deprecated: Use lockfile.*LockFile.
type Locker = lockfile.Locker //nolint:staticcheck // SA1019 lockfile.Locker is deprecated

// Deprecated: Use lockfile.GetLockFile.
func GetLockfile(path string) (lockfile.Locker, error) {
	return lockfile.GetLockfile(path)
}

// Deprecated: Use lockfile.GetROLockFile.
func GetROLockfile(path string) (lockfile.Locker, error) {
	return lockfile.GetROLockfile(path)
}
