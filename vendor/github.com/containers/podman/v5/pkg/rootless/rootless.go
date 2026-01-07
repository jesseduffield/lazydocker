package rootless

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/moby/sys/user"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/lockfile"
)

// TryJoinPauseProcess attempts to join the namespaces of the pause PID via
// TryJoinFromFilePaths.  If joining fails, it attempts to delete the specified
// file.
func TryJoinPauseProcess(pausePidPath string) (bool, int, error) {
	if err := fileutils.Exists(pausePidPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, -1, nil
		}
		return false, -1, err
	}

	became, ret, err := TryJoinFromFilePaths("", []string{pausePidPath})
	if err == nil {
		return became, ret, nil
	}

	// It could not join the pause process, let's lock the file before trying to delete it.
	pidFileLock, err := lockfile.GetLockFile(pausePidPath)
	if err != nil {
		// The file was deleted by another process.
		if os.IsNotExist(err) {
			return false, -1, nil
		}
		return false, -1, fmt.Errorf("acquiring lock on %s: %w", pausePidPath, err)
	}

	pidFileLock.Lock()
	defer func() {
		pidFileLock.Unlock()
	}()

	// Now the pause PID file is locked.  Try to join once again in case it changed while it was not locked.
	became, ret, err = TryJoinFromFilePaths("", []string{pausePidPath})
	if err != nil {
		// It is still failing.  We can safely remove it.
		os.Remove(pausePidPath)
		return false, -1, nil
	}
	return became, ret, err
}

var (
	uidMap      []user.IDMap
	uidMapError error
	uidMapOnce  sync.Once

	gidMap      []user.IDMap
	gidMapError error
	gidMapOnce  sync.Once
)

// GetAvailableUIDMap returns the UID mappings in the
// current user namespace.
func GetAvailableUIDMap() ([]user.IDMap, error) {
	uidMapOnce.Do(func() {
		var err error
		uidMap, err = user.ParseIDMapFile("/proc/self/uid_map")
		if err != nil {
			uidMapError = err
			return
		}
	})
	return uidMap, uidMapError
}

// GetAvailableGIDMap returns the GID mappings in the
// current user namespace.
func GetAvailableGIDMap() ([]user.IDMap, error) {
	gidMapOnce.Do(func() {
		var err error
		gidMap, err = user.ParseIDMapFile("/proc/self/gid_map")
		if err != nil {
			gidMapError = err
			return
		}
	})
	return gidMap, gidMapError
}

// GetAvailableIDMaps returns the UID and GID mappings in the
// current user namespace.
func GetAvailableIDMaps() ([]user.IDMap, []user.IDMap, error) {
	u, err := GetAvailableUIDMap()
	if err != nil {
		return nil, nil, err
	}
	g, err := GetAvailableGIDMap()
	if err != nil {
		return nil, nil, err
	}
	return u, g, nil
}

func countAvailableIDs(mappings []user.IDMap) int64 {
	availableUids := int64(0)
	for _, r := range mappings {
		availableUids += r.Count
	}
	return availableUids
}

// GetAvailableGids returns how many GIDs are available in the
// current user namespace.
func GetAvailableGids() (int64, error) {
	gids, err := GetAvailableGIDMap()
	if err != nil {
		return -1, err
	}

	return countAvailableIDs(gids), nil
}

// findIDInMappings find the mapping that contains the specified ID.
// It assumes availableMappings is sorted by ID.
func findIDInMappings(id int64, availableMappings []user.IDMap) *user.IDMap {
	i := sort.Search(len(availableMappings), func(i int) bool {
		return availableMappings[i].ID <= id
	})
	if i < 0 || i >= len(availableMappings) {
		return nil
	}
	r := &availableMappings[i]
	if id >= r.ID && id < r.ID+r.Count {
		return r
	}
	return nil
}

// MaybeSplitMappings checks whether the specified OCI mappings are possible
// in the current user namespace or the specified ranges must be split.
func MaybeSplitMappings(mappings []spec.LinuxIDMapping, availableMappings []user.IDMap) []spec.LinuxIDMapping {
	var ret []spec.LinuxIDMapping
	var overflow spec.LinuxIDMapping
	overflow.Size = 0
	consumed := 0
	sort.Slice(availableMappings, func(i, j int) bool {
		return availableMappings[i].ID > availableMappings[j].ID
	})
	for {
		cur := overflow
		// if there is no overflow left from the previous request, get the next one
		if cur.Size == 0 {
			if consumed == len(mappings) {
				// all done
				return ret
			}
			cur = mappings[consumed]
			consumed++
		}

		// Find the range where the first specified ID is present
		r := findIDInMappings(int64(cur.HostID), availableMappings)
		if r == nil {
			// The requested range is not available.  Just return the original request
			// and let other layers deal with it.
			return mappings
		}

		offsetInRange := cur.HostID - uint32(r.ID)

		usableIDs := uint32(r.Count) - offsetInRange

		// the current range can satisfy the whole request
		if usableIDs >= cur.Size {
			// reset the overflow
			overflow.Size = 0
		} else {
			// the current range can satisfy the request partially
			// so move the rest to overflow
			overflow.Size = cur.Size - usableIDs
			overflow.ContainerID = cur.ContainerID + usableIDs
			overflow.HostID = cur.HostID + usableIDs

			// and cap to the usableIDs count
			cur.Size = usableIDs
		}
		ret = append(ret, cur)
	}
}
