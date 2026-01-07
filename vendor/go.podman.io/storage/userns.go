//go:build linux

package storage

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	securejoin "github.com/cyphar/filepath-securejoin"
	libcontainerUser "github.com/moby/sys/user"
	"github.com/sirupsen/logrus"
	drivers "go.podman.io/storage/drivers"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/unshare"
	"go.podman.io/storage/types"
	"golang.org/x/sys/unix"
)

// getAdditionalSubIDs looks up the additional IDs configured for
// the specified user.
// The argument USERNAME is ignored for rootless users, as it is not
// possible to use an arbitrary entry in /etc/sub*id.
// Differently, if the username is not specified for root users, a
// default name is used.
func getAdditionalSubIDs(username string) (*idSet, *idSet, error) {
	var uids, gids *idSet

	if unshare.IsRootless() {
		username = os.Getenv("USER")
		if username == "" {
			var id string
			if os.Geteuid() == 0 {
				id = strconv.Itoa(unshare.GetRootlessUID())
			} else {
				id = strconv.Itoa(os.Geteuid())
			}
			userID, err := user.LookupId(id)
			if err == nil {
				username = userID.Username
			}
		}
	} else if username == "" {
		username = RootAutoUserNsUser
	}
	mappings, err := idtools.NewIDMappings(username, username)
	if err != nil {
		logrus.Errorf("Cannot find mappings for user %q: %v", username, err)
	} else {
		uids = getHostIDs(mappings.UIDs())
		gids = getHostIDs(mappings.GIDs())
	}
	return uids, gids, nil
}

// getAvailableIDs returns the list of ranges that are usable by the current user.
// When running as root, it looks up the additional IDs assigned to the specified user.
// When running as rootless, the mappings assigned to the unprivileged user are converted
// to the IDs inside of the initial rootless user namespace.
func (s *store) getAvailableIDs() (*idSet, *idSet, error) {
	if s.additionalUIDs == nil {
		uids, gids, err := getAdditionalSubIDs(s.autoUsernsUser)
		if err != nil {
			return nil, nil, err
		}
		// Store the result so we don't need to look it up again next time
		s.additionalUIDs, s.additionalGIDs = uids, gids
	}

	if !unshare.IsRootless() {
		// No mapping to inner namespace needed
		return s.additionalUIDs, s.additionalGIDs, nil
	}

	// We are already inside of the rootless user namespace.
	// We need to remap the configured mappings to what is available
	// inside of the rootless userns.
	u := newIDSet([]interval{{start: 1, end: s.additionalUIDs.size() + 1}})
	g := newIDSet([]interval{{start: 1, end: s.additionalGIDs.size() + 1}})
	return u, g, nil
}

// nobodyUser returns the UID and GID of the "nobody" user.  Hardcode its value
// for simplicity.
const nobodyUser = 65534

// parseMountedFiles returns the maximum UID and GID found in the /etc/passwd and
// /etc/group files.
func parseMountedFiles(containerMount, passwdFile, groupFile string) uint32 {
	var (
		passwd *os.File
		group  *os.File
		size   int
		err    error
	)
	if passwdFile == "" {
		passwd, err = secureOpen(containerMount, "/etc/passwd")
	} else {
		// User-specified override from a volume. Will not be in
		// container root.
		passwd, err = os.Open(passwdFile)
	}
	if err == nil {
		defer passwd.Close()

		users, err := libcontainerUser.ParsePasswd(passwd)
		if err == nil {
			for _, u := range users {
				// Skip the "nobody" user otherwise we end up with 65536
				// ids with most images
				if u.Name == "nobody" || u.Name == "nogroup" {
					continue
				}
				if u.Uid > size && u.Uid != nobodyUser {
					size = u.Uid + 1
				}
				if u.Gid > size && u.Gid != nobodyUser {
					size = u.Gid + 1
				}
			}
		}
	}

	if groupFile == "" {
		group, err = secureOpen(containerMount, "/etc/group")
	} else {
		// User-specified override from a volume. Will not be in
		// container root.
		group, err = os.Open(groupFile)
	}
	if err == nil {
		defer group.Close()

		groups, err := libcontainerUser.ParseGroup(group)
		if err == nil {
			for _, g := range groups {
				if g.Name == "nobody" || g.Name == "nogroup" {
					continue
				}
				if g.Gid > size && g.Gid != nobodyUser {
					size = g.Gid + 1
				}
			}
		}
	}

	return uint32(size)
}

// getMaxSizeFromImage returns the maximum ID used by the specified image.
// On entry, rlstore must be locked for writing, and lstores must be locked for reading.
func (s *store) getMaxSizeFromImage(image *Image, rlstore rwLayerStore, lstores []roLayerStore, passwdFile, groupFile string) (_ uint32, retErr error) {
	layerStores := append([]roLayerStore{rlstore}, lstores...)

	size := uint32(0)

	var topLayer *Layer
	layerName := image.TopLayer
outer:
	for {
		for _, ls := range layerStores {
			layer, err := ls.Get(layerName)
			if err != nil {
				continue
			}
			if image.TopLayer == layerName {
				topLayer = layer
			}
			for _, uid := range layer.UIDs {
				if uid >= size {
					size = uid + 1
				}
			}
			for _, gid := range layer.GIDs {
				if gid >= size {
					size = gid + 1
				}
			}
			layerName = layer.Parent
			if layerName == "" {
				break outer
			}
			continue outer
		}
		return 0, fmt.Errorf("cannot find layer %q", layerName)
	}

	layerOptions := &LayerOptions{
		IDMappingOptions: types.IDMappingOptions{
			HostUIDMapping: true,
			HostGIDMapping: true,
			UIDMap:         nil,
			GIDMap:         nil,
		},
	}

	// We need to create a temporary layer so we can mount it and lookup the
	// maximum IDs used.
	clayer, _, err := rlstore.create("", topLayer, nil, "", nil, layerOptions, false, nil, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err2 := rlstore.deleteWhileHoldingLock(clayer.ID); err2 != nil {
			if retErr == nil {
				retErr = fmt.Errorf("deleting temporary layer %#v: %w", clayer.ID, err2)
			} else {
				logrus.Errorf("Error deleting temporary layer %#v: %v", clayer.ID, err2)
			}
		}
	}()

	mountOptions := drivers.MountOpts{
		MountLabel: "",
		UidMaps:    nil,
		GidMaps:    nil,
		Options:    nil,
	}

	mountpoint, err := rlstore.Mount(clayer.ID, mountOptions)
	if err != nil {
		return 0, err
	}
	defer func() {
		if _, err2 := rlstore.unmount(clayer.ID, true, false); err2 != nil {
			if retErr == nil {
				retErr = fmt.Errorf("unmounting temporary layer %#v: %w", clayer.ID, err2)
			} else {
				logrus.Errorf("Error unmounting temporary layer %#v: %v", clayer.ID, err2)
			}
		}
	}()

	userFilesSize := parseMountedFiles(mountpoint, passwdFile, groupFile)
	if userFilesSize > size {
		size = userFilesSize
	}

	return size, nil
}

// getAutoUserNS creates an automatic user namespace
// If image != nil, On entry, rlstore must be locked for writing, and lstores must be locked for reading.
func (s *store) getAutoUserNS(options *types.AutoUserNsOptions, image *Image, rlstore rwLayerStore, lstores []roLayerStore) ([]idtools.IDMap, []idtools.IDMap, error) {
	requestedSize := uint32(0)
	initialSize := uint32(1)
	if options.Size > 0 {
		requestedSize = options.Size
	}
	if options.InitialSize > 0 {
		initialSize = options.InitialSize
	}

	availableUIDs, availableGIDs, err := s.getAvailableIDs()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read mappings: %w", err)
	}

	// Look at every container that is using a user namespace and store
	// the intervals that are already used.
	containers, err := s.Containers()
	if err != nil {
		return nil, nil, err
	}
	var usedUIDs, usedGIDs []idtools.IDMap
	for _, c := range containers {
		usedUIDs = append(usedUIDs, c.UIDMap...)
		usedGIDs = append(usedGIDs, c.GIDMap...)
	}

	size := requestedSize

	// If there is no requestedSize, lookup the maximum used IDs in the layers
	// metadata.  Make sure the size is at least s.autoNsMinSize and it is not
	// bigger than s.autoNsMaxSize.
	// This is a best effort heuristic.
	if requestedSize == 0 {
		size = max(s.autoNsMinSize, initialSize)
		if image != nil {
			sizeFromImage, err := s.getMaxSizeFromImage(image, rlstore, lstores, options.PasswdFile, options.GroupFile)
			if err != nil {
				return nil, nil, err
			}
			if sizeFromImage > size {
				size = sizeFromImage
			}
		}
		if s.autoNsMaxSize > 0 && size > s.autoNsMaxSize {
			return nil, nil, fmt.Errorf("the container needs a user namespace with size %v that is bigger than the maximum value allowed with userns=auto %v", size, s.autoNsMaxSize)
		}
	}

	return getAutoUserNSIDMappings(
		int(size),
		availableUIDs, availableGIDs,
		usedUIDs, usedGIDs,
		options.AdditionalUIDMappings, options.AdditionalGIDMappings,
	)
}

// getAutoUserNSIDMappings computes the user/group id mappings for the automatic user namespace.
func getAutoUserNSIDMappings(
	size int,
	availableUIDs, availableGIDs *idSet,
	usedUIDMappings, usedGIDMappings, additionalUIDMappings, additionalGIDMappings []idtools.IDMap,
) ([]idtools.IDMap, []idtools.IDMap, error) {
	usedUIDs := getHostIDs(append(usedUIDMappings, additionalUIDMappings...))
	usedGIDs := getHostIDs(append(usedGIDMappings, additionalGIDMappings...))

	// Exclude additional uids and gids from requested range.
	targetIDs := newIDSet([]interval{{start: 0, end: size}})
	requestedContainerUIDs := targetIDs.subtract(getContainerIDs(additionalUIDMappings))
	requestedContainerGIDs := targetIDs.subtract(getContainerIDs(additionalGIDMappings))

	// Make sure the specified additional IDs are not used as part of the automatic
	// mapping
	availableUIDs, err := availableUIDs.subtract(usedUIDs).findAvailable(requestedContainerUIDs.size())
	if err != nil {
		return nil, nil, err
	}
	availableGIDs, err = availableGIDs.subtract(usedGIDs).findAvailable(requestedContainerGIDs.size())
	if err != nil {
		return nil, nil, err
	}

	uidMap := append(availableUIDs.zip(requestedContainerUIDs), additionalUIDMappings...)
	gidMap := append(availableGIDs.zip(requestedContainerGIDs), additionalGIDMappings...)
	return uidMap, gidMap, nil
}

// Securely open (read-only) a file in a container mount.
func secureOpen(containerMount, file string) (*os.File, error) {
	tmpFile, err := securejoin.OpenInRoot(containerMount, file)
	if err != nil {
		return nil, err
	}
	defer tmpFile.Close()

	return securejoin.Reopen(tmpFile, unix.O_RDONLY)
}
