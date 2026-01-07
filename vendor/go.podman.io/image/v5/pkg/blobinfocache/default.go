package blobinfocache

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/internal/rootless"
	"go.podman.io/image/v5/pkg/blobinfocache/memory"
	"go.podman.io/image/v5/pkg/blobinfocache/sqlite"
	"go.podman.io/image/v5/types"
)

const (
	// blobInfoCacheFilename is the file name used for blob info caches.
	// If the format changes in an incompatible way, increase the version number.
	blobInfoCacheFilename = "blob-info-cache-v1.sqlite"
	// systemBlobInfoCacheDir is the directory containing the blob info cache (in blobInfocacheFilename) for root-running processes.
	systemBlobInfoCacheDir = "/var/lib/containers/cache"
)

// blobInfoCacheDir returns a path to a blob info cache appropriate for sys and euid.
// euid is used so that (sudo …) does not write root-owned files into the unprivileged users’ home directory.
func blobInfoCacheDir(sys *types.SystemContext, euid int) (string, error) {
	if sys != nil && sys.BlobInfoCacheDir != "" {
		return sys.BlobInfoCacheDir, nil
	}

	// FIXME? On Windows, os.Geteuid() returns -1.  What should we do?  Right now we treat it as unprivileged
	// and fail (fall back to memory-only) if neither HOME nor XDG_DATA_HOME is set, which is, at least, safe.
	if euid == 0 {
		if sys != nil && sys.RootForImplicitAbsolutePaths != "" {
			return filepath.Join(sys.RootForImplicitAbsolutePaths, systemBlobInfoCacheDir), nil
		}
		return systemBlobInfoCacheDir, nil
	}

	// This is intended to mirror the GraphRoot determination in github.com/containers/libpod/pkg/util.GetRootlessStorageOpts.
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return "", fmt.Errorf("neither XDG_DATA_HOME nor HOME was set non-empty")
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataDir, "containers", "cache"), nil
}

// DefaultCache returns the default BlobInfoCache implementation appropriate for sys.
func DefaultCache(sys *types.SystemContext) types.BlobInfoCache {
	dir, err := blobInfoCacheDir(sys, rootless.GetRootlessEUID())
	if err != nil {
		logrus.Debugf("Error determining a location for %s, using a memory-only cache", blobInfoCacheFilename)
		return memory.New()
	}
	path := filepath.Join(dir, blobInfoCacheFilename)
	if err := os.MkdirAll(dir, 0700); err != nil {
		logrus.Debugf("Error creating parent directories for %s, using a memory-only cache: %v", path, err)
		return memory.New()
	}

	// It might make sense to keep a single sqlite cache object, and a single initialized sqlite connection, open
	// as global singleton, for the vast majority of callers who don’t override thde cache location.
	// OTOH that would keep a file descriptor open forever, even for long-term callers who copy images rarely,
	// and the performance benefit to this over using an Open()/Close() pair for a single image copy is < 10%.

	cache, err := sqlite.New(path)
	if err != nil {
		logrus.Debugf("Error creating a SQLite blob info cache at %s, using a memory-only cache: %v", path, err)
		return memory.New()
	}
	logrus.Debugf("Using SQLite blob info cache at %s", path)
	return cache
}

// CleanupDefaultCache removes the blob info cache directory.
// It deletes the cache directory but it does not affect any file or memory buffer currently
// in use.
func CleanupDefaultCache(sys *types.SystemContext) error {
	dir, err := blobInfoCacheDir(sys, rootless.GetRootlessEUID())
	if err != nil {
		// Mirror the DefaultCache behavior that does not fail in this case
		return nil
	}
	return os.RemoveAll(dir)
}
