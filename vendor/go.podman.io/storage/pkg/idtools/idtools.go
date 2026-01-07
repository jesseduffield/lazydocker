package idtools

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/system"
)

// IDMap contains a single entry for user namespace range remapping. An array
// of IDMap entries represents the structure that will be provided to the Linux
// kernel for creating a user namespace.
type IDMap struct {
	ContainerID int `json:"container_id"`
	HostID      int `json:"host_id"`
	Size        int `json:"size"`
}

type subIDRange struct {
	Start  int
	Length int
}

type ranges []subIDRange

func (e ranges) Len() int           { return len(e) }
func (e ranges) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e ranges) Less(i, j int) bool { return e[i].Start < e[j].Start }

const (
	subuidFileName          string = "/etc/subuid"
	subgidFileName          string = "/etc/subgid"
	ContainersOverrideXattr        = "user.containers.override_stat"
)

// MkdirAllAs creates a directory (include any along the path) and then modifies
// ownership to the requested uid/gid.  If the directory already exists, this
// function will still change ownership to the requested uid/gid pair.
// Deprecated: Use MkdirAllAndChown
func MkdirAllAs(path string, mode os.FileMode, ownerUID, ownerGID int) error {
	return mkdirAs(path, mode, ownerUID, ownerGID, true, true)
}

// MkdirAs creates a directory and then modifies ownership to the requested uid/gid.
// If the directory already exists, this function still changes ownership
// Deprecated: Use MkdirAndChown with a IDPair
func MkdirAs(path string, mode os.FileMode, ownerUID, ownerGID int) error {
	return mkdirAs(path, mode, ownerUID, ownerGID, false, true)
}

// MkdirAllAndChown creates a directory (include any along the path) and then modifies
// ownership to the requested uid/gid.  If the directory already exists, this
// function will still change ownership to the requested uid/gid pair.
func MkdirAllAndChown(path string, mode os.FileMode, ids IDPair) error {
	return mkdirAs(path, mode, ids.UID, ids.GID, true, true)
}

// MkdirAndChown creates a directory and then modifies ownership to the requested uid/gid.
// If the directory already exists, this function still changes ownership
func MkdirAndChown(path string, mode os.FileMode, ids IDPair) error {
	return mkdirAs(path, mode, ids.UID, ids.GID, false, true)
}

// MkdirAllAndChownNew creates a directory (include any along the path) and then modifies
// ownership ONLY of newly created directories to the requested uid/gid. If the
// directories along the path exist, no change of ownership will be performed
func MkdirAllAndChownNew(path string, mode os.FileMode, ids IDPair) error {
	return mkdirAs(path, mode, ids.UID, ids.GID, true, false)
}

// GetRootUIDGID retrieves the remapped root uid/gid pair from the set of maps.
// If the maps are empty, then the root uid/gid will default to "real" 0/0
func GetRootUIDGID(uidMap, gidMap []IDMap) (int, int, error) {
	var uid, gid int
	var err error
	if len(uidMap) == 1 && uidMap[0].Size == 1 {
		uid = uidMap[0].HostID
	} else {
		uid, err = RawToHost(0, uidMap)
		if err != nil {
			return -1, -1, err
		}
	}
	if len(gidMap) == 1 && gidMap[0].Size == 1 {
		gid = gidMap[0].HostID
	} else {
		gid, err = RawToHost(0, gidMap)
		if err != nil {
			return -1, -1, err
		}
	}
	return uid, gid, nil
}

// RawToContainer takes an id mapping, and uses it to translate a host ID to
// the remapped ID. If no map is provided, then the translation assumes a
// 1-to-1 mapping and returns the passed in id.
//
// If you wish to map a (uid,gid) combination you should use the corresponding
// IDMappings methods, which ensure that you are mapping the correct ID against
// the correct mapping.
func RawToContainer(hostID int, idMap []IDMap) (int, error) {
	if idMap == nil {
		return hostID, nil
	}
	for _, m := range idMap {
		if (hostID >= m.HostID) && (hostID <= (m.HostID + m.Size - 1)) {
			contID := m.ContainerID + (hostID - m.HostID)
			return contID, nil
		}
	}
	return -1, fmt.Errorf("host ID %d cannot be mapped to a container ID", hostID)
}

// RawToHost takes an id mapping and a remapped ID, and translates the ID to
// the mapped host ID. If no map is provided, then the translation assumes a
// 1-to-1 mapping and returns the passed in id.
//
// If you wish to map a (uid,gid) combination you should use the corresponding
// IDMappings methods, which ensure that you are mapping the correct ID against
// the correct mapping.
func RawToHost(contID int, idMap []IDMap) (int, error) {
	if idMap == nil {
		return contID, nil
	}
	for _, m := range idMap {
		if (contID >= m.ContainerID) && (contID <= (m.ContainerID + m.Size - 1)) {
			hostID := m.HostID + (contID - m.ContainerID)
			return hostID, nil
		}
	}
	return -1, fmt.Errorf("container ID %d cannot be mapped to a host ID", contID)
}

// IDPair is a UID and GID pair
type IDPair struct {
	UID int
	GID int
}

// IDMappings contains a mappings of UIDs and GIDs
type IDMappings struct {
	uids []IDMap
	gids []IDMap
}

// NewIDMappings takes a requested user and group name and
// using the data from /etc/sub{uid,gid} ranges, creates the
// proper uid and gid remapping ranges for that user/group pair
func NewIDMappings(username, groupname string) (*IDMappings, error) {
	subuidRanges, err := readSubuid(username)
	if err != nil {
		return nil, err
	}
	subgidRanges, err := readSubgid(groupname)
	if err != nil {
		return nil, err
	}
	if len(subuidRanges) == 0 {
		return nil, fmt.Errorf("no subuid ranges found for user %q in %s", username, subuidFileName)
	}
	if len(subgidRanges) == 0 {
		return nil, fmt.Errorf("no subgid ranges found for group %q in %s", groupname, subgidFileName)
	}

	return &IDMappings{
		uids: createIDMap(subuidRanges),
		gids: createIDMap(subgidRanges),
	}, nil
}

// NewIDMappingsFromMaps creates a new mapping from two slices
// Deprecated: this is a temporary shim while transitioning to IDMapping
func NewIDMappingsFromMaps(uids []IDMap, gids []IDMap) *IDMappings {
	return &IDMappings{uids: uids, gids: gids}
}

// RootPair returns a uid and gid pair for the root user. The error is ignored
// because a root user always exists, and the defaults are correct when the uid
// and gid maps are empty.
func (i *IDMappings) RootPair() IDPair {
	uid, gid, _ := GetRootUIDGID(i.uids, i.gids)
	return IDPair{UID: uid, GID: gid}
}

// ToHost returns the host UID and GID for the container uid, gid.
func (i *IDMappings) ToHost(pair IDPair) (IDPair, error) {
	var err error
	var target IDPair

	target.UID, err = RawToHost(pair.UID, i.uids)
	if err != nil {
		return target, err
	}

	target.GID, err = RawToHost(pair.GID, i.gids)
	return target, err
}

var (
	overflowUIDOnce sync.Once
	overflowGIDOnce sync.Once
	overflowUID     int
	overflowGID     int
)

// getOverflowUID returns the UID mapped to the overflow user
func getOverflowUID() int {
	overflowUIDOnce.Do(func() {
		// 65534 is the value on older kernels where /proc/sys/kernel/overflowuid is not present
		overflowUID = 65534
		if content, err := os.ReadFile("/proc/sys/kernel/overflowuid"); err == nil {
			if tmp, err := strconv.Atoi(string(content)); err == nil {
				overflowUID = tmp
			}
		}
	})
	return overflowUID
}

// getOverflowGID returns the GID mapped to the overflow user
func getOverflowGID() int {
	overflowGIDOnce.Do(func() {
		// 65534 is the value on older kernels where /proc/sys/kernel/overflowgid is not present
		overflowGID = 65534
		if content, err := os.ReadFile("/proc/sys/kernel/overflowgid"); err == nil {
			if tmp, err := strconv.Atoi(string(content)); err == nil {
				overflowGID = tmp
			}
		}
	})
	return overflowGID
}

// ToHost returns the host UID and GID for the container uid, gid.
// Remapping is only performed if the ids aren't already the remapped root ids
// If the mapping is not possible because the target ID is not mapped into
// the namespace, then the overflow ID is used.
func (i *IDMappings) ToHostOverflow(pair IDPair) (IDPair, error) {
	var err error
	target := i.RootPair()

	if pair.UID != target.UID {
		target.UID, err = RawToHost(pair.UID, i.uids)
		if err != nil {
			target.UID = getOverflowUID()
			logrus.Debugf("Failed to map UID %v to the target mapping, using the overflow ID %v", pair.UID, target.UID)
		}
	}

	if pair.GID != target.GID {
		target.GID, err = RawToHost(pair.GID, i.gids)
		if err != nil {
			target.GID = getOverflowGID()
			logrus.Debugf("Failed to map GID %v to the target mapping, using the overflow ID %v", pair.GID, target.GID)
		}
	}
	return target, nil
}

// ToContainer returns the container UID and GID for the host uid and gid
func (i *IDMappings) ToContainer(pair IDPair) (int, int, error) {
	uid, err := RawToContainer(pair.UID, i.uids)
	if err != nil {
		return -1, -1, err
	}
	gid, err := RawToContainer(pair.GID, i.gids)
	return uid, gid, err
}

// Empty returns true if there are no id mappings
func (i *IDMappings) Empty() bool {
	return len(i.uids) == 0 && len(i.gids) == 0
}

// UIDs return the UID mapping
// TODO: remove this once everything has been refactored to use pairs
func (i *IDMappings) UIDs() []IDMap {
	return i.uids
}

// GIDs return the UID mapping
// TODO: remove this once everything has been refactored to use pairs
func (i *IDMappings) GIDs() []IDMap {
	return i.gids
}

func createIDMap(subidRanges ranges) []IDMap {
	idMap := []IDMap{}

	// sort the ranges by lowest ID first
	sort.Sort(subidRanges)
	containerID := 0
	for _, idrange := range subidRanges {
		idMap = append(idMap, IDMap{
			ContainerID: containerID,
			HostID:      idrange.Start,
			Size:        idrange.Length,
		})
		containerID = containerID + idrange.Length
	}
	return idMap
}

// parseSubidFile will read the appropriate file (/etc/subuid or /etc/subgid)
// and return all found ranges for a specified username. If the special value
// "ALL" is supplied for username, then all ranges in the file will be returned
func parseSubidFile(path, username string) (ranges, error) {
	var (
		rangeList ranges
		uidstr    string
	)
	if u, err := user.Lookup(username); err == nil {
		uidstr = u.Uid
	}

	subidFile, err := os.Open(path)
	if err != nil {
		return rangeList, err
	}
	defer subidFile.Close()

	s := bufio.NewScanner(subidFile)
	for s.Scan() {
		if err := s.Err(); err != nil {
			return rangeList, err
		}

		text := strings.TrimSpace(s.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		parts := strings.Split(text, ":")
		if len(parts) != 3 {
			return rangeList, fmt.Errorf("cannot parse subuid/gid information: Format not correct for %s file", path)
		}
		if parts[0] == username || username == "ALL" || (parts[0] == uidstr && parts[0] != "") {
			startid, err := strconv.Atoi(parts[1])
			if err != nil {
				return rangeList, fmt.Errorf("string to int conversion failed during subuid/gid parsing of %s: %w", path, err)
			}
			length, err := strconv.Atoi(parts[2])
			if err != nil {
				return rangeList, fmt.Errorf("string to int conversion failed during subuid/gid parsing of %s: %w", path, err)
			}
			rangeList = append(rangeList, subIDRange{startid, length})
		}
	}
	return rangeList, nil
}

func checkChownErr(err error, name string, uid, gid int) error {
	var e *os.PathError
	if errors.As(err, &e) && e.Err == syscall.EINVAL {
		return fmt.Errorf(`potentially insufficient UIDs or GIDs available in user namespace (requested %d:%d for %s): Check /etc/subuid and /etc/subgid if configured locally and run "podman system migrate": %w`, uid, gid, name, err)
	}
	return err
}

// Stat contains file states that can be overridden with ContainersOverrideXattr.
type Stat struct {
	IDs   IDPair
	Mode  os.FileMode
	Major int
	Minor int
}

// FormatContainersOverrideXattr will format the given uid, gid, and mode into a string
// that can be used as the value for the ContainersOverrideXattr xattr.
func FormatContainersOverrideXattr(uid, gid, mode int) string {
	return FormatContainersOverrideXattrDevice(uid, gid, fs.FileMode(mode), 0, 0)
}

// FormatContainersOverrideXattrDevice will format the given uid, gid, and mode into a string
// that can be used as the value for the ContainersOverrideXattr xattr.  For devices, it also
// needs the major and minor numbers.
func FormatContainersOverrideXattrDevice(uid, gid int, mode fs.FileMode, major, minor int) string {
	typ := ""
	switch mode & os.ModeType {
	case os.ModeDir:
		typ = "dir"
	case os.ModeSymlink:
		typ = "symlink"
	case os.ModeNamedPipe:
		typ = "pipe"
	case os.ModeSocket:
		typ = "socket"
	case os.ModeDevice:
		typ = fmt.Sprintf("block-%d-%d", major, minor)
	case os.ModeDevice | os.ModeCharDevice:
		typ = fmt.Sprintf("char-%d-%d", major, minor)
	default:
		typ = "file"
	}
	unixMode := mode & os.ModePerm
	if mode&os.ModeSetuid != 0 {
		unixMode |= 0o4000
	}
	if mode&os.ModeSetgid != 0 {
		unixMode |= 0o2000
	}
	if mode&os.ModeSticky != 0 {
		unixMode |= 0o1000
	}
	return fmt.Sprintf("%d:%d:%04o:%s", uid, gid, unixMode, typ)
}

// GetContainersOverrideXattr will get and decode ContainersOverrideXattr.
func GetContainersOverrideXattr(path string) (Stat, error) {
	xstat, err := system.Lgetxattr(path, ContainersOverrideXattr)
	if err != nil {
		return Stat{}, err
	}
	return parseOverrideXattr(xstat) // This will fail if (xstat, err) == (nil, nil), i.e. the xattr does not exist.
}

func parseOverrideXattr(xstat []byte) (Stat, error) {
	var stat Stat
	attrs := strings.Split(string(xstat), ":")
	if len(attrs) < 3 {
		return stat, fmt.Errorf("the number of parts in %s is less than 3",
			ContainersOverrideXattr)
	}

	value, err := strconv.ParseUint(attrs[0], 10, 32)
	if err != nil {
		return stat, fmt.Errorf("failed to parse UID: %w", err)
	}
	stat.IDs.UID = int(value)

	value, err = strconv.ParseUint(attrs[1], 10, 32)
	if err != nil {
		return stat, fmt.Errorf("failed to parse GID: %w", err)
	}
	stat.IDs.GID = int(value)

	value, err = strconv.ParseUint(attrs[2], 8, 32)
	if err != nil {
		return stat, fmt.Errorf("failed to parse mode: %w", err)
	}
	stat.Mode = os.FileMode(value) & os.ModePerm
	if value&0o1000 != 0 {
		stat.Mode |= os.ModeSticky
	}
	if value&0o2000 != 0 {
		stat.Mode |= os.ModeSetgid
	}
	if value&0o4000 != 0 {
		stat.Mode |= os.ModeSetuid
	}

	if len(attrs) > 3 {
		typ := attrs[3]
		if strings.HasPrefix(typ, "file") {
		} else if strings.HasPrefix(typ, "dir") {
			stat.Mode |= os.ModeDir
		} else if strings.HasPrefix(typ, "symlink") {
			stat.Mode |= os.ModeSymlink
		} else if strings.HasPrefix(typ, "pipe") {
			stat.Mode |= os.ModeNamedPipe
		} else if strings.HasPrefix(typ, "socket") {
			stat.Mode |= os.ModeSocket
		} else if strings.HasPrefix(typ, "block") {
			stat.Mode |= os.ModeDevice
			stat.Major, stat.Minor, err = parseDevice(typ)
			if err != nil {
				return stat, err
			}
		} else if strings.HasPrefix(typ, "char") {
			stat.Mode |= os.ModeDevice | os.ModeCharDevice
			stat.Major, stat.Minor, err = parseDevice(typ)
			if err != nil {
				return stat, err
			}
		} else {
			return stat, fmt.Errorf("invalid file type %s", typ)
		}
	}
	return stat, nil
}

func parseDevice(typ string) (int, int, error) {
	parts := strings.Split(typ, "-")
	// If there are more than 3 parts, just ignore them to be forward compatible
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("invalid device type %s", typ)
	}
	if parts[0] != "block" && parts[0] != "char" {
		return 0, 0, fmt.Errorf("invalid device type %s", typ)
	}
	major, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse major number: %w", err)
	}
	minor, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse minor number: %w", err)
	}
	return major, minor, nil
}

// SetContainersOverrideXattr will encode and set ContainersOverrideXattr.
func SetContainersOverrideXattr(path string, stat Stat) error {
	value := FormatContainersOverrideXattrDevice(stat.IDs.UID, stat.IDs.GID, stat.Mode, stat.Major, stat.Minor)
	return system.Lsetxattr(path, ContainersOverrideXattr, []byte(value), 0)
}

func SafeChown(name string, uid, gid int) error {
	if runtime.GOOS == "darwin" {
		stat := Stat{
			Mode: os.FileMode(0o0700),
		}
		xstat, err := system.Lgetxattr(name, ContainersOverrideXattr)
		if err == nil && xstat != nil {
			stat, err = parseOverrideXattr(xstat)
			if err != nil {
				return err
			}
		} else {
			st, err := os.Stat(name) // Ideally we would share this with system.Stat below, but then we would need to convert Mode.
			if err != nil {
				return err
			}
			stat.Mode = st.Mode()
		}
		stat.IDs = IDPair{UID: uid, GID: gid}
		if err = SetContainersOverrideXattr(name, stat); err != nil {
			return err
		}
		uid = os.Getuid()
		gid = os.Getgid()
	}
	if stat, statErr := system.Stat(name); statErr == nil {
		if stat.UID() == uint32(uid) && stat.GID() == uint32(gid) {
			return nil
		}
	}
	return checkChownErr(os.Chown(name, uid, gid), name, uid, gid)
}

func SafeLchown(name string, uid, gid int) error {
	if runtime.GOOS == "darwin" {
		stat := Stat{
			Mode: os.FileMode(0o0700),
		}
		xstat, err := system.Lgetxattr(name, ContainersOverrideXattr)
		if err == nil && xstat != nil {
			stat, err = parseOverrideXattr(xstat)
			if err != nil {
				return err
			}
		} else {
			st, err := os.Lstat(name) // Ideally we would share this with system.Stat below, but then we would need to convert Mode.
			if err != nil {
				return err
			}
			stat.Mode = st.Mode()
		}
		stat.IDs = IDPair{UID: uid, GID: gid}
		if err = SetContainersOverrideXattr(name, stat); err != nil {
			return err
		}
		uid = os.Getuid()
		gid = os.Getgid()
	}
	if stat, statErr := system.Lstat(name); statErr == nil {
		if stat.UID() == uint32(uid) && stat.GID() == uint32(gid) {
			return nil
		}
	}
	return checkChownErr(os.Lchown(name, uid, gid), name, uid, gid)
}

type sortByHostID []IDMap

func (e sortByHostID) Len() int           { return len(e) }
func (e sortByHostID) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e sortByHostID) Less(i, j int) bool { return e[i].HostID < e[j].HostID }

type sortByContainerID []IDMap

func (e sortByContainerID) Len() int           { return len(e) }
func (e sortByContainerID) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e sortByContainerID) Less(i, j int) bool { return e[i].ContainerID < e[j].ContainerID }

// IsContiguous checks if the specified mapping is contiguous and doesn't
// have any hole.
func IsContiguous(mappings []IDMap) bool {
	if len(mappings) < 2 {
		return true
	}

	var mh sortByHostID = mappings[:]
	sort.Sort(mh)
	for i := 1; i < len(mh); i++ {
		if mh[i].HostID != mh[i-1].HostID+mh[i-1].Size {
			return false
		}
	}

	var mc sortByContainerID = mappings[:]
	sort.Sort(mc)
	for i := 1; i < len(mc); i++ {
		if mc[i].ContainerID != mc[i-1].ContainerID+mc[i-1].Size {
			return false
		}
	}
	return true
}
