package util

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/bits"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/namespaces"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/signal"
	securejoin "github.com/cyphar/filepath-securejoin"
	ruser "github.com/moby/sys/user"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/directory"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/unshare"
	stypes "go.podman.io/storage/types"
	"golang.org/x/term"
)

// The flags that an [ug]id mapping can have
type idMapFlags struct {
	Extends  bool // The "+" flag
	UserMap  bool // The "u" flag
	GroupMap bool // The "g" flag
}

// Helper function to determine the username/password passed
// in the creds string.  It could be either or both.
func parseCreds(creds string) (string, string) {
	username, password, _ := strings.Cut(creds, ":")
	return username, password
}

// Takes build context and validates `.containerignore` or `.dockerignore`
// if they are symlink outside of buildcontext. Returns list of files to be
// excluded and resolved path to the ignore files inside build context or error
func ParseDockerignore(containerfiles []string, root string) ([]string, string, error) {
	ignoreFile := ""
	path, err := securejoin.SecureJoin(root, ".containerignore")
	if err != nil {
		return nil, ignoreFile, err
	}
	// set resolved ignore file so imagebuildah
	// does not attempts to re-resolve it
	ignoreFile = path
	ignore, err := os.ReadFile(path)
	if err != nil {
		var dockerIgnoreErr error
		path, symlinkErr := securejoin.SecureJoin(root, ".dockerignore")
		if symlinkErr != nil {
			return nil, ignoreFile, symlinkErr
		}
		// set resolved ignore file so imagebuildah
		// does not attempts to re-resolve it
		ignoreFile = path
		ignore, dockerIgnoreErr = os.ReadFile(path)
		if errors.Is(dockerIgnoreErr, fs.ErrNotExist) {
			// In this case either ignorefile was not found
			// or it is a symlink to unexpected file in such
			// case manually set ignorefile to `/dev/null` so
			// internally imagebuildah does not attempts to re-resolve
			// this invalid symlink and instead reads a blank file.
			ignoreFile = "/dev/null"
		}
		// after https://github.com/containers/buildah/pull/4239 build supports
		// <Containerfile>.containerignore or <Containerfile>.dockerignore as ignore file
		// so remote must support parsing that.
		if dockerIgnoreErr != nil {
			for _, containerfile := range containerfiles {
				containerfile = strings.TrimPrefix(containerfile, root)
				if err := fileutils.Exists(filepath.Join(root, containerfile+".containerignore")); err == nil {
					path, symlinkErr = securejoin.SecureJoin(root, containerfile+".containerignore")
					if symlinkErr == nil {
						ignoreFile = path
						ignore, dockerIgnoreErr = os.ReadFile(path)
					}
				}
				if err := fileutils.Exists(filepath.Join(root, containerfile+".dockerignore")); err == nil {
					path, symlinkErr = securejoin.SecureJoin(root, containerfile+".dockerignore")
					if symlinkErr == nil {
						ignoreFile = path
						ignore, dockerIgnoreErr = os.ReadFile(path)
					}
				}
				if dockerIgnoreErr == nil {
					break
				}
			}
		}
		if dockerIgnoreErr != nil && !os.IsNotExist(dockerIgnoreErr) {
			return nil, ignoreFile, err
		}
	}
	rawexcludes := strings.Split(string(ignore), "\n")
	excludes := make([]string, 0, len(rawexcludes))
	for _, e := range rawexcludes {
		if len(e) == 0 || e[0] == '#' {
			continue
		}
		excludes = append(excludes, e)
	}
	return excludes, ignoreFile, nil
}

// ParseRegistryCreds takes a credentials string in the form USERNAME:PASSWORD
// and returns a DockerAuthConfig
func ParseRegistryCreds(creds string) (*types.DockerAuthConfig, error) {
	username, password := parseCreds(creds)
	if username == "" {
		fmt.Print("Username: ")
		_, err := fmt.Scanln(&username)
		if err != nil {
			return nil, fmt.Errorf("could not read username: %w", err)
		}
	}
	if password == "" {
		fmt.Print("Password: ")
		termPassword, err := term.ReadPassword(0)
		if err != nil {
			return nil, fmt.Errorf("could not read password from terminal: %w", err)
		}
		password = string(termPassword)
	}

	return &types.DockerAuthConfig{
		Username: username,
		Password: password,
	}, nil
}

// StringMatchRegexSlice determines if a given string matches one of the given regexes, returns bool
func StringMatchRegexSlice(s string, re []string) bool {
	for _, r := range re {
		m, err := regexp.MatchString(r, s)
		if err == nil && m {
			return true
		}
	}
	return false
}

// ParseSignal parses and validates a signal name or number.
func ParseSignal(rawSignal string) (syscall.Signal, error) {
	// Strip off leading dash, to allow -1 or -HUP
	basename := strings.TrimPrefix(rawSignal, "-")

	sig, err := signal.ParseSignal(basename)
	if err != nil {
		return -1, err
	}
	// 64 is SIGRTMAX; wish we could get this from a standard Go library
	if sig < 1 || sig > 64 {
		return -1, errors.New("valid signals are 1 through 64")
	}
	return sig, nil
}

func getRootlessKeepIDMapping(uid, gid int, uids, gids []idtools.IDMap, maxSize int) (*stypes.IDMappingOptions, int, int, error) {
	options := stypes.IDMappingOptions{
		HostUIDMapping: false,
		HostGIDMapping: false,
	}
	maxUID, maxGID := 0, 0
	for _, u := range uids {
		maxUID += u.Size
	}
	for _, g := range gids {
		maxGID += g.Size
	}
	if maxSize > 0 {
		// If maxSize is set, we need to ensure that the mappings are within the available range
		maxUID = min(maxUID, maxSize-1)
		maxGID = min(maxGID, maxSize-1)
	}

	options.UIDMap, options.GIDMap = nil, nil

	if len(uids) > 0 && uid != 0 {
		options.UIDMap = append(options.UIDMap, idtools.IDMap{ContainerID: 0, HostID: 1, Size: min(uid, maxUID)})
	}
	options.UIDMap = append(options.UIDMap, idtools.IDMap{ContainerID: uid, HostID: 0, Size: 1})
	if maxUID > uid {
		options.UIDMap = append(options.UIDMap, idtools.IDMap{ContainerID: uid + 1, HostID: uid + 1, Size: maxUID - uid})
	}

	if len(gids) > 0 && gid != 0 {
		options.GIDMap = append(options.GIDMap, idtools.IDMap{ContainerID: 0, HostID: 1, Size: min(gid, maxGID)})
	}
	options.GIDMap = append(options.GIDMap, idtools.IDMap{ContainerID: gid, HostID: 0, Size: 1})
	if maxGID > gid {
		options.GIDMap = append(options.GIDMap, idtools.IDMap{ContainerID: gid + 1, HostID: gid + 1, Size: maxGID - gid})
	}

	return &options, uid, gid, nil
}

// GetKeepIDMapping returns the mappings and the user to use when keep-id is used
func GetKeepIDMapping(opts *namespaces.KeepIDUserNsOptions) (*stypes.IDMappingOptions, int, int, error) {
	if !rootless.IsRootless() {
		options := stypes.IDMappingOptions{
			HostUIDMapping: false,
			HostGIDMapping: false,
		}
		uids, gids, err := unshare.GetHostIDMappings("")
		if err != nil {
			return nil, 0, 0, err
		}
		options.UIDMap = RuntimeSpecToIDtools(uids)
		options.GIDMap = RuntimeSpecToIDtools(gids)

		uid, gid := 0, 0
		if opts.UID != nil {
			uid = int(*opts.UID)
		}
		if opts.GID != nil {
			gid = int(*opts.GID)
		}

		return &options, uid, gid, nil
	}

	uid := rootless.GetRootlessUID()
	gid := rootless.GetRootlessGID()
	if opts.UID != nil {
		uid = int(*opts.UID)
	}
	if opts.GID != nil {
		gid = int(*opts.GID)
	}
	maxSize := 0
	if opts.MaxSize != nil {
		maxSize = int(*opts.MaxSize)
	}

	uids, gids, err := rootless.GetConfiguredMappings(true)
	if err != nil {
		return nil, -1, -1, fmt.Errorf("cannot read mappings: %w", err)
	}

	return getRootlessKeepIDMapping(uid, gid, uids, gids, maxSize)
}

// GetNoMapMapping returns the mappings and the user to use when nomap is used
func GetNoMapMapping() (*stypes.IDMappingOptions, int, int, error) {
	if !rootless.IsRootless() {
		return nil, -1, -1, errors.New("nomap is only supported in rootless mode")
	}
	options := stypes.IDMappingOptions{
		HostUIDMapping: false,
		HostGIDMapping: false,
	}
	uids, gids, err := rootless.GetConfiguredMappings(false)
	if err != nil {
		return nil, -1, -1, fmt.Errorf("cannot read mappings: %w", err)
	}
	if len(uids) == 0 || len(gids) == 0 {
		return nil, -1, -1, fmt.Errorf("nomap requires additional UIDs or GIDs defined in /etc/subuid and /etc/subgid to function correctly: %w", err)
	}
	options.UIDMap, options.GIDMap = nil, nil
	uid, gid := 0, 0
	for _, u := range uids {
		options.UIDMap = append(options.UIDMap, idtools.IDMap{ContainerID: uid, HostID: uid + 1, Size: u.Size})
		uid += u.Size
	}
	for _, g := range gids {
		options.GIDMap = append(options.GIDMap, idtools.IDMap{ContainerID: gid, HostID: gid + 1, Size: g.Size})
		gid += g.Size
	}
	return &options, 0, 0, nil
}

// Map a given ID to the Parent/Host ID of a given mapping, and return
// its corresponding ID/ContainerID.
// Returns an error if the given ID is not found on the mapping parents
func mapIDwithMapping(id uint64, mapping []ruser.IDMap, mapSetting string) (mappedid uint64, err error) {
	for _, v := range mapping {
		if v.Count == 0 {
			continue
		}
		if id >= uint64(v.ParentID) && id < uint64(v.ParentID+v.Count) {
			offset := id - uint64(v.ParentID)
			return uint64(v.ID) + offset, nil
		}
	}
	return uint64(0), fmt.Errorf("parent ID %s %d is not mapped/delegated", mapSetting, id)
}

// Parse flags from spec
// The `u` and `g` flags can be used to enforce that the mapping applies
// exclusively to UIDs or GIDs.
//
// The `+` flag is interpreted as if the mapping replaces previous mappings
// removing any conflicting mapping from those before adding this one.
func parseFlags(spec []string) (flags idMapFlags, read int, err error) {
	flags.Extends = false
	flags.UserMap = false
	flags.GroupMap = false
	for read, char := range spec[0] {
		switch {
		case '0' <= char && char <= '9':
			return flags, read, nil
		case char == '+':
			flags.Extends = true
		case char == 'u':
			flags.UserMap = true
		case char == 'g':
			flags.GroupMap = true
		case true:
			return flags, 0, fmt.Errorf("invalid mapping: %v. Unknown flag %v", spec, char)
		}
	}
	return flags, read, fmt.Errorf("invalid mapping: %v, parsing flags", spec)
}

// Extension of idTools.parseTriple that parses idmap triples.
// The triple should be a length 3 string array, containing:
// - Flags and ContainerID
// - HostID
// - Size
//
// parseTriple returns the parsed mapping, the mapping flags and
// any possible error. If the error is not-nil, the mapping and flags
// are not well-defined.
//
// idTools.parseTriple is extended here with the following enhancements:
//
// HostID @ syntax:
// =================
// HostID may use the "@" syntax: The "101001:@1001:1" mapping
// means "take the 1001 id from the parent namespace and map it to 101001"
//
// Flags:
// ======
// Flags can be used to tell the caller how should the mapping be interpreted
func parseTriple(spec []string, parentMapping []ruser.IDMap, mapSetting string) (mappings []idtools.IDMap, flags idMapFlags, err error) {
	if len(spec[0]) == 0 {
		return mappings, flags, fmt.Errorf("invalid empty container id at %s map: %v", mapSetting, spec)
	}
	var cids, hids, sizes []uint64
	var cid, hid uint64
	var hidIsParent bool
	flags, i, err := parseFlags(spec)
	if err != nil {
		return mappings, flags, err
	}
	// If no "u" nor "g" flag is given, assume the mapping applies to both
	if !flags.UserMap && !flags.GroupMap {
		flags.UserMap = true
		flags.GroupMap = true
	}
	// Parse the container ID, which must be an integer:
	cid, err = strconv.ParseUint(spec[0][i:], 10, 32)
	if err != nil {
		return mappings, flags, fmt.Errorf("parsing id map value %q: %w", spec[0], err)
	}
	// Parse the host id, which may be integer or @<integer>
	if len(spec[1]) == 0 {
		return mappings, flags, fmt.Errorf("invalid empty host id at %s map: %v", mapSetting, spec)
	}
	if spec[1][0] != '@' {
		hidIsParent = false
		hid, err = strconv.ParseUint(spec[1], 10, 32)
	} else {
		// Parse @<id>, where <id> is an integer corresponding to the parent mapping
		hidIsParent = true
		hid, err = strconv.ParseUint(spec[1][1:], 10, 32)
	}
	if err != nil {
		return mappings, flags, fmt.Errorf("parsing id map value %q: %w", spec[1], err)
	}
	// Parse the size of the mapping, which must be an integer
	sz, err := strconv.ParseUint(spec[2], 10, 32)
	if err != nil {
		return mappings, flags, fmt.Errorf("parsing id map value %q: %w", spec[2], err)
	}

	if hidIsParent {
		if (mapSetting == "UID" && flags.UserMap) || (mapSetting == "GID" && flags.GroupMap) {
			for i := range sz {
				cids = append(cids, cid+i)
				mappedID, err := mapIDwithMapping(hid+i, parentMapping, mapSetting)
				if err != nil {
					return mappings, flags, err
				}
				hids = append(hids, mappedID)
				sizes = append(sizes, 1)
			}
		}
	} else {
		cids = []uint64{cid}
		hids = []uint64{hid}
		sizes = []uint64{sz}
	}

	// Avoid possible integer overflow on 32bit builds
	if bits.UintSize == 32 {
		for i := range cids {
			if cids[i] > math.MaxInt32 || hids[i] > math.MaxInt32 || sizes[i] > math.MaxInt32 {
				return mappings, flags, fmt.Errorf("initializing ID mappings: %s setting is malformed expected [\"[+ug]uint32:[@]uint32[:uint32]\"] : %q", mapSetting, spec)
			}
		}
	}
	for i := range cids {
		mappings = append(mappings, idtools.IDMap{
			ContainerID: int(cids[i]),
			HostID:      int(hids[i]),
			Size:        int(sizes[i]),
		})
	}
	return mappings, flags, nil
}

// Remove any conflicting mapping from mapping present in extension, so
// extension can be appended to mapping without conflicts.
// Returns the resulting mapping, with extension appended to it.
func breakInsert(mapping []idtools.IDMap, extension idtools.IDMap) (result []idtools.IDMap) {
	// Two steps:
	// 1. Remove extension regions from mapping
	//    For each element in mapping, remove those parts of the mapping
	//    that overlap with the extension, both in the container range
	//    or in the host range.
	// 2. Add extension to mapping
	// Step 1: Remove extension regions from mapping
	for _, mapPiece := range mapping {
		// Make container and host ranges comparable, by computing their
		// extension relative to the start of the mapPiece:
		range1Start := extension.ContainerID - mapPiece.ContainerID
		range2Start := extension.HostID - mapPiece.HostID

		// Range end relative to mapPiece range
		range1End := range1Start + extension.Size
		range2End := range2Start + extension.Size

		// mapPiece range:
		mapPieceStart := 0
		mapPieceEnd := mapPiece.Size

		if range1End < mapPieceStart || range1Start >= mapPieceEnd {
			// out of range, forget about it
			range1End = -1
			range1Start = -1
		} else {
			// clip limits removal to mapPiece
			range1End = min(range1End, mapPieceEnd)
			range1Start = max(range1Start, mapPieceStart)
		}

		if range2End < mapPieceStart || range2Start >= mapPieceEnd {
			// out of range, forget about it
			range2End = -1
			range2Start = -1
		} else {
			// clip limits removal to mapPiece
			range2End = min(range2End, mapPieceEnd)
			range2Start = max(range2Start, mapPieceStart)
		}

		// If there is nothing to remove, append the original and continue:
		if range1Start == -1 && range2Start == -1 {
			result = append(result, mapPiece)
			continue
		}

		// If there is one range to remove, save it at range1:
		if range1Start == -1 && range2Start != -1 {
			range1Start = range2Start
			range1End = range2End
			range2Start = -1
			range2End = -1
		}

		// If we have two valid ranges, merge them into range1 if possible
		if range2Start != -1 {
			// Swap ranges so always range1Start is <= range2Start
			if range2Start < range1Start {
				range1Start, range2Start = range2Start, range1Start
				range1End, range2End = range2End, range1End
			}
			// If there is overlap, merge them:
			if range1End >= range2Start {
				range1End = max(range1End, range2End)
				range2Start = -1
				range2End = -1
			}
		}

		if range1Start > 0 {
			// Append everything before range1Start
			result = append(result, idtools.IDMap{
				ContainerID: mapPiece.ContainerID,
				HostID:      mapPiece.HostID,
				Size:        range1Start,
			})
		}
		if range2Start == -1 {
			// Append everything after range1
			if mapPiece.Size-range1End > 0 {
				result = append(result, idtools.IDMap{
					ContainerID: mapPiece.ContainerID + range1End,
					HostID:      mapPiece.HostID + range1End,
					Size:        mapPiece.Size - range1End,
				})
			}
		} else {
			// Append everything between range1 and range2
			result = append(result, idtools.IDMap{
				ContainerID: mapPiece.ContainerID + range1End,
				HostID:      mapPiece.HostID + range1End,
				Size:        range2Start - range1End,
			})
			// Append everything after range2
			if mapPiece.Size-range2End > 0 {
				result = append(result, idtools.IDMap{
					ContainerID: mapPiece.ContainerID + range2End,
					HostID:      mapPiece.HostID + range2End,
					Size:        mapPiece.Size - range2End,
				})
			}
		}
	}
	// Step 2. Add extension to mapping
	result = append(result, extension)
	return result
}

// A multirange is a list of [start,end) ranges and is expressed as
// an array of length-2 integers.
//
// This function computes availableRanges = fullRanges - usedRanges,
// where all variables are multiranges.
// The subtraction operation is defined as "return the multirange
// containing all integers found in fullRanges and not found in usedRanges.
func getAvailableIDRanges(fullRanges, usedRanges [][2]int) (availableRanges [][2]int) {
	// Sort them
	sort.Slice(fullRanges, func(i, j int) bool {
		return fullRanges[i][0] < fullRanges[j][0]
	})

	if len(usedRanges) == 0 {
		return fullRanges
	}

	sort.Slice(usedRanges, func(i, j int) bool {
		return usedRanges[i][0] < usedRanges[j][0]
	})

	// To traverse usedRanges
	i := 0
	nextUsedID := usedRanges[i][0]
	nextUsedIDEnd := usedRanges[i][1]

	for _, fullRange := range fullRanges {
		currentIDToProcess := fullRange[0]
		for currentIDToProcess < fullRange[1] {
			switch {
			case nextUsedID == -1:
				// No further used ids, append all the remaining ranges
				availableRanges = append(availableRanges, [2]int{currentIDToProcess, fullRange[1]})
				currentIDToProcess = fullRange[1]
			case currentIDToProcess < nextUsedID:
				// currentIDToProcess is not used, append:
				if fullRange[1] <= nextUsedID {
					availableRanges = append(availableRanges, [2]int{currentIDToProcess, fullRange[1]})
					currentIDToProcess = fullRange[1]
				} else {
					availableRanges = append(availableRanges, [2]int{currentIDToProcess, nextUsedID})
					currentIDToProcess = nextUsedID
				}
			case currentIDToProcess == nextUsedID:
				// currentIDToProcess and all ids until nextUsedIDEnd are used
				// Advance currentIDToProcess
				currentIDToProcess = min(fullRange[1], nextUsedIDEnd)
			default: // currentIDToProcess > nextUsedID
				// Increment nextUsedID so it is >= currentIDToProcess
				// Go to next used block if this one is all behind:
				if currentIDToProcess >= nextUsedIDEnd {
					i += 1
					if i == len(usedRanges) {
						// No more used ranges
						nextUsedID = -1
					} else {
						nextUsedID = usedRanges[i][0]
						nextUsedIDEnd = usedRanges[i][1]
					}
					continue
				} else { // currentIDToProcess < nextUsedIDEnd
					currentIDToProcess = min(fullRange[1], nextUsedIDEnd)
				}
			}
		}
	}
	return availableRanges
}

// Gets the multirange of subordinated ids from parentMapping and the
// multirange of already assigned ids from idmap, and returns the
// multirange of unassigned subordinated ids.
func getAvailableIDRangesFromMappings(idmap []idtools.IDMap, parentMapping []ruser.IDMap) (availableRanges [][2]int) {
	// Get all subordinated ids from parentMapping:
	fullRanges := [][2]int{} // {Multirange: [start, end), [start, end), ...}
	for _, mapPiece := range parentMapping {
		fullRanges = append(fullRanges, [2]int{int(mapPiece.ID), int(mapPiece.ID + mapPiece.Count)})
	}

	// Get the ids already mapped:
	usedRanges := [][2]int{}
	for _, mapPiece := range idmap {
		usedRanges = append(usedRanges, [2]int{mapPiece.HostID, mapPiece.HostID + mapPiece.Size})
	}

	// availableRanges = fullRanges - usedRanges
	availableRanges = getAvailableIDRanges(fullRanges, usedRanges)
	return availableRanges
}

// Fills unassigned idmap ContainerIDs, starting from zero with all
// the available ids given by availableRanges.
// Returns the filled idmap.
func fillIDMap(idmap []idtools.IDMap, availableRanges [][2]int) (output []idtools.IDMap) {
	idmapByCid := append([]idtools.IDMap{}, idmap...)
	sort.Slice(idmapByCid, func(i, j int) bool {
		return idmapByCid[i].ContainerID < idmapByCid[j].ContainerID
	})

	if len(availableRanges) == 0 {
		return idmapByCid
	}

	i := 0 // to iterate through availableRanges
	nextCid := 0
	nextAvailHid := availableRanges[i][0]

	for _, mapPiece := range idmapByCid {
		// While there are available IDs to map and unassigned
		// container ids, map the available ids:
		for nextCid < mapPiece.ContainerID && nextAvailHid != -1 {
			size := min(mapPiece.ContainerID-nextCid, availableRanges[i][1]-nextAvailHid)
			output = append(output, idtools.IDMap{
				ContainerID: nextCid,
				HostID:      nextAvailHid,
				Size:        size,
			})
			nextCid += size
			if nextAvailHid+size < availableRanges[i][1] {
				nextAvailHid += size
			} else {
				i += 1
				if i == len(availableRanges) {
					nextAvailHid = -1
					continue
				}
				nextAvailHid = availableRanges[i][0]
			}
		}
		// The given mapping does not change
		output = append(output, mapPiece)
		nextCid += mapPiece.Size
	}
	// After the last given mapping is mapped, we use all the remaining
	// ids to map the rest of the space
	for nextAvailHid != -1 {
		size := availableRanges[i][1] - nextAvailHid
		output = append(output, idtools.IDMap{
			ContainerID: nextCid,
			HostID:      nextAvailHid,
			Size:        size,
		})
		nextCid += size
		i += 1
		if i == len(availableRanges) {
			nextAvailHid = -1
			continue
		}
		nextAvailHid = availableRanges[i][0]
	}
	return output
}

func addOneMapping(idmap []idtools.IDMap, fillMap bool, mapping idtools.IDMap, flags idMapFlags, mapSetting string) ([]idtools.IDMap, bool) {
	// If we are mapping uids and the spec doesn't have the usermap flag, ignore it
	if mapSetting == "UID" && !flags.UserMap {
		return idmap, fillMap
	}
	// If we are mapping gids and the spec doesn't have the groupmap flag, ignore it
	if mapSetting == "GID" && !flags.GroupMap {
		return idmap, fillMap
	}

	// Zero-size mapping is ignored
	if mapping.Size == 0 {
		return idmap, fillMap
	}

	// Not extending, just append:
	if !flags.Extends {
		idmap = append(idmap, mapping)
		return idmap, fillMap
	}
	// Break and extend the last mapping:

	// Extending without any mapping, if rootless, we will fill
	// the space with the remaining IDs:
	if len(idmap) == 0 && rootless.IsRootless() {
		fillMap = true
	}

	idmap = breakInsert(idmap, mapping)
	return idmap, fillMap
}

// Extension of idTools.ParseIDMap that parses idmap triples from string.
// This extension accepts additional flags that control how the mapping is done
func ParseIDMap(mapSpec []string, mapSetting string, parentMapping []ruser.IDMap) (idmap []idtools.IDMap, err error) {
	stdErr := fmt.Errorf("initializing ID mappings: %s setting is malformed expected [\"[+ug]uint32:[@]uint32[:uint32]\"] : %q", mapSetting, mapSpec)
	// When fillMap is true, the given mapping will be filled with the remaining subordinate available ids
	fillMap := false
	for _, idMapSpec := range mapSpec {
		if idMapSpec == "" {
			continue
		}
		idSpec := strings.Split(idMapSpec, ":")
		// if it's a length-2 list assume the size is 1:
		if len(idSpec) == 2 {
			idSpec = append(idSpec, "1")
		}
		if len(idSpec)%3 != 0 {
			return nil, stdErr
		}
		for i := range idSpec {
			if i%3 != 0 {
				continue
			}
			if len(idSpec[i]) == 0 {
				return nil, stdErr
			}
			// Parse this mapping:
			mappings, flags, err := parseTriple(idSpec[i:i+3], parentMapping, mapSetting)
			if err != nil {
				return nil, err
			}
			for _, mapping := range mappings {
				idmap, fillMap = addOneMapping(idmap, fillMap, mapping, flags, mapSetting)
			}
		}
	}
	if fillMap {
		availableRanges := getAvailableIDRangesFromMappings(idmap, parentMapping)
		idmap = fillIDMap(idmap, availableRanges)
	}

	if len(idmap) == 0 {
		return idmap, nil
	}
	idmap = sortAndMergeConsecutiveMappings(idmap)
	return idmap, nil
}

// Given a mapping, sort all entries by their ContainerID then and merge
// entries that are consecutive.
func sortAndMergeConsecutiveMappings(idmap []idtools.IDMap) (finalIDMap []idtools.IDMap) {
	idmapByCid := append([]idtools.IDMap{}, idmap...)
	sort.Slice(idmapByCid, func(i, j int) bool {
		return idmapByCid[i].ContainerID < idmapByCid[j].ContainerID
	})
	for i, mapPiece := range idmapByCid {
		if i == 0 {
			finalIDMap = append(finalIDMap, mapPiece)
			continue
		}
		lastMap := finalIDMap[len(finalIDMap)-1]
		containersMatch := lastMap.ContainerID+lastMap.Size == mapPiece.ContainerID
		hostsMatch := lastMap.HostID+lastMap.Size == mapPiece.HostID
		if containersMatch && hostsMatch {
			finalIDMap[len(finalIDMap)-1].Size += mapPiece.Size
		} else {
			finalIDMap = append(finalIDMap, mapPiece)
		}
	}
	return finalIDMap
}

// Extension of idTools.parseAutoTriple that parses idmap triples.
// The triple should be a length 3 string array, containing:
// - Flags and ContainerID
// - HostID
// - Size
//
// parseAutoTriple returns the parsed mapping and any possible error.
// If the error is not-nil, the mapping is not well-defined.
//
// idTools.parseAutoTriple is extended here with the following enhancements:
//
// HostID @ syntax:
// =================
// HostID may use the "@" syntax: The "101001:@1001:1" mapping
// means "take the 1001 id from the parent namespace and map it to 101001"
func parseAutoTriple(spec []string, parentMapping []ruser.IDMap, mapSetting string) (mappings []idtools.IDMap, err error) {
	if len(spec[0]) == 0 {
		return mappings, fmt.Errorf("invalid empty container id at %s map: %v", mapSetting, spec)
	}
	var cids, hids, sizes []uint64
	var cid, hid uint64
	var hidIsParent bool
	// Parse the container ID, which must be an integer:
	cid, err = strconv.ParseUint(spec[0][0:], 10, 32)
	if err != nil {
		return mappings, fmt.Errorf("parsing id map value %q: %w", spec[0], err)
	}
	// Parse the host id, which may be integer or @<integer>
	if len(spec[1]) == 0 {
		return mappings, fmt.Errorf("invalid empty host id at %s map: %v", mapSetting, spec)
	}
	if spec[1][0] != '@' {
		hidIsParent = false
		hid, err = strconv.ParseUint(spec[1], 10, 32)
	} else {
		// Parse @<id>, where <id> is an integer corresponding to the parent mapping
		hidIsParent = true
		hid, err = strconv.ParseUint(spec[1][1:], 10, 32)
	}
	if err != nil {
		return mappings, fmt.Errorf("parsing id map value %q: %w", spec[1], err)
	}
	// Parse the size of the mapping, which must be an integer
	sz, err := strconv.ParseUint(spec[2], 10, 32)
	if err != nil {
		return mappings, fmt.Errorf("parsing id map value %q: %w", spec[2], err)
	}

	if hidIsParent {
		for i := range sz {
			cids = append(cids, cid+i)
			mappedID, err := mapIDwithMapping(hid+i, parentMapping, mapSetting)
			if err != nil {
				return mappings, err
			}
			hids = append(hids, mappedID)
			sizes = append(sizes, 1)
		}
	} else {
		cids = []uint64{cid}
		hids = []uint64{hid}
		sizes = []uint64{sz}
	}

	// Avoid possible integer overflow on 32bit builds
	if bits.UintSize == 32 {
		for i := range cids {
			if cids[i] > math.MaxInt32 || hids[i] > math.MaxInt32 || sizes[i] > math.MaxInt32 {
				return mappings, fmt.Errorf("initializing ID mappings: %s setting is malformed expected [\"[+ug]uint32:[@]uint32[:uint32]\"] : %q", mapSetting, spec)
			}
		}
	}
	for i := range cids {
		mappings = append(mappings, idtools.IDMap{
			ContainerID: int(cids[i]),
			HostID:      int(hids[i]),
			Size:        int(sizes[i]),
		})
	}
	return mappings, nil
}

// Extension of idTools.ParseIDMap that parses idmap triples from string.
// This extension accepts additional flags that control how the mapping is done
func parseAutoIDMap(mapSpec string, mapSetting string, parentMapping []ruser.IDMap) (idmap []idtools.IDMap, err error) {
	stdErr := fmt.Errorf("initializing ID mappings: %s setting is malformed expected [\"uint32:[@]uint32[:uint32]\"] : %q", mapSetting, mapSpec)
	idSpec := strings.Split(mapSpec, ":")
	// if it's a length-2 list assume the size is 1:
	if len(idSpec) == 2 {
		idSpec = append(idSpec, "1")
	}
	if len(idSpec) != 3 {
		return nil, stdErr
	}
	// Parse this mapping:
	mappings, err := parseAutoTriple(idSpec, parentMapping, mapSetting)
	if err != nil {
		return nil, err
	}
	idmap = sortAndMergeConsecutiveMappings(mappings)
	return idmap, nil
}

// GetAutoOptions returns an AutoUserNsOptions with the settings to automatically set up
// a user namespace.
func GetAutoOptions(n namespaces.UsernsMode) (*stypes.AutoUserNsOptions, error) {
	mode, opts, hasOpts := strings.Cut(string(n), ":")
	if mode != "auto" {
		return nil, fmt.Errorf("wrong user namespace mode")
	}
	options := stypes.AutoUserNsOptions{}
	if !hasOpts {
		return &options, nil
	}

	parentUIDMap, parentGIDMap, err := rootless.GetAvailableIDMaps()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// The kernel-provided files only exist if user namespaces are supported
			logrus.Debugf("User or group ID mappings not available: %s", err)
		} else {
			return nil, err
		}
	}

	for o := range strings.SplitSeq(opts, ",") {
		key, val, hasVal := strings.Cut(o, "=")
		if !hasVal {
			return nil, fmt.Errorf("invalid option specified: %q", o)
		}
		switch key {
		case "size":
			s, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				return nil, err
			}
			options.Size = uint32(s)
		case "uidmapping":
			mapping, err := parseAutoIDMap(val, "UID", parentUIDMap)
			if err != nil {
				return nil, err
			}
			options.AdditionalUIDMappings = append(options.AdditionalUIDMappings, mapping...)
		case "gidmapping":
			mapping, err := parseAutoIDMap(val, "GID", parentGIDMap)
			if err != nil {
				return nil, err
			}
			options.AdditionalGIDMappings = append(options.AdditionalGIDMappings, mapping...)
		default:
			return nil, fmt.Errorf("unknown option specified: %q", key)
		}
	}
	return &options, nil
}

// ParseIDMapping takes idmappings and subuid and subgid maps and returns a storage mapping
func ParseIDMapping(mode namespaces.UsernsMode, uidMapSlice, gidMapSlice []string, subUIDMap, subGIDMap string) (*stypes.IDMappingOptions, error) {
	options := stypes.IDMappingOptions{
		HostUIDMapping: true,
		HostGIDMapping: true,
	}

	if mode.IsAuto() {
		var err error
		options.HostUIDMapping = false
		options.HostGIDMapping = false
		options.AutoUserNs = true
		opts, err := GetAutoOptions(mode)
		if err != nil {
			return nil, err
		}
		options.AutoUserNsOpts = *opts
		return &options, nil
	}
	if mode.IsKeepID() || mode.IsNoMap() {
		options.HostUIDMapping = false
		options.HostGIDMapping = false
		return &options, nil
	}

	if subGIDMap == "" && subUIDMap != "" {
		subGIDMap = subUIDMap
	}
	if subUIDMap == "" && subGIDMap != "" {
		subUIDMap = subGIDMap
	}
	if len(gidMapSlice) == 0 && len(uidMapSlice) != 0 {
		gidMapSlice = uidMapSlice
	}
	if len(uidMapSlice) == 0 && len(gidMapSlice) != 0 {
		uidMapSlice = gidMapSlice
	}

	if subUIDMap != "" && subGIDMap != "" {
		mappings, err := idtools.NewIDMappings(subUIDMap, subGIDMap)
		if err != nil {
			return nil, err
		}
		options.UIDMap = mappings.UIDs()
		options.GIDMap = mappings.GIDs()
	}

	parentUIDMap, parentGIDMap, err := rootless.GetAvailableIDMaps()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// The kernel-provided files only exist if user namespaces are supported
			logrus.Debugf("User or group ID mappings not available: %s", err)
		} else {
			return nil, err
		}
	}

	parsedUIDMap, err := ParseIDMap(uidMapSlice, "UID", parentUIDMap)
	if err != nil {
		return nil, err
	}
	parsedGIDMap, err := ParseIDMap(gidMapSlice, "GID", parentGIDMap)
	if err != nil {
		return nil, err
	}

	// When running rootless, if one of UID/GID mappings is provided, fill the other one:
	if rootless.IsRootless() {
		switch {
		case len(parsedUIDMap) != 0 && len(parsedGIDMap) == 0:
			availableRanges := getAvailableIDRangesFromMappings(parsedGIDMap, parentGIDMap)
			parsedGIDMap = fillIDMap(parsedGIDMap, availableRanges)
		case len(parsedUIDMap) == 0 && len(parsedGIDMap) != 0:
			availableRanges := getAvailableIDRangesFromMappings(parsedUIDMap, parentUIDMap)
			parsedUIDMap = fillIDMap(parsedUIDMap, availableRanges)
		}
	}

	options.UIDMap = append(options.UIDMap, parsedUIDMap...)
	options.GIDMap = append(options.GIDMap, parsedGIDMap...)
	if len(options.UIDMap) > 0 {
		options.HostUIDMapping = false
	}
	if len(options.GIDMap) > 0 {
		options.HostGIDMapping = false
	}
	return &options, nil
}

// ParseInputTime takes the users input and to determine if it is valid and
// returns a time format and error.  The input is compared to known time formats
// or a duration which implies no-duration
func ParseInputTime(inputTime string, since bool) (time.Time, error) {
	timeFormats := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04:05.999999999",
		"2006-01-02Z07:00", "2006-01-02"}
	// iterate the supported time formats
	for _, tf := range timeFormats {
		t, err := time.Parse(tf, inputTime)
		if err == nil {
			return t, nil
		}
	}

	unixTimestamp, err := strconv.ParseFloat(inputTime, 64)
	if err == nil {
		iPart, fPart := math.Modf(unixTimestamp)
		return time.Unix(int64(iPart), int64(fPart*1_000_000_000)).UTC(), nil
	}

	// input might be a duration
	duration, err := time.ParseDuration(inputTime)
	if err != nil {
		return time.Time{}, errors.New("unable to interpret time value")
	}
	if since {
		return time.Now().Add(-duration), nil
	}
	return time.Now().Add(duration), nil
}

func Tmpdir() string {
	tmpdir := os.Getenv("TMPDIR")
	if tmpdir == "" {
		tmpdir = "/var/tmp"
	}

	return tmpdir
}

// ValidateSysctls validates a list of sysctl and returns it.
func ValidateSysctls(strSlice []string) (map[string]string, error) {
	sysctl := make(map[string]string)
	validSysctlMap := map[string]bool{
		"kernel.msgmax":          true,
		"kernel.msgmnb":          true,
		"kernel.msgmni":          true,
		"kernel.sem":             true,
		"kernel.shmall":          true,
		"kernel.shmmax":          true,
		"kernel.shmmni":          true,
		"kernel.shm_rmid_forced": true,
	}
	validSysctlPrefixes := []string{
		"net.",
		"fs.mqueue.",
	}

	for _, val := range strSlice {
		foundMatch := false
		arr := strings.Split(val, "=")
		if len(arr) < 2 {
			return nil, fmt.Errorf("%s is invalid, sysctl values must be in the form of KEY=VALUE", val)
		}

		trimmed := fmt.Sprintf("%s=%s", strings.TrimSpace(arr[0]), strings.TrimSpace(arr[1]))
		if trimmed != val {
			return nil, fmt.Errorf("'%s' is invalid, extra spaces found", val)
		}

		if validSysctlMap[arr[0]] {
			sysctl[arr[0]] = arr[1]
			continue
		}

		for _, prefix := range validSysctlPrefixes {
			if strings.HasPrefix(arr[0], prefix) {
				sysctl[arr[0]] = arr[1]
				foundMatch = true
				break
			}
		}
		if !foundMatch {
			return nil, fmt.Errorf("sysctl '%s' is not allowed", arr[0])
		}
	}
	return sysctl, nil
}

func CreateIDFile(path string, id string) error {
	idFile, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating idfile: %w", err)
	}
	defer idFile.Close()
	if _, err = idFile.WriteString(id); err != nil {
		return fmt.Errorf("writing idfile: %w", err)
	}
	return nil
}

// DefaultCPUPeriod is the default CPU period (100ms) in microseconds, which is
// the same default as Kubernetes.
const DefaultCPUPeriod uint64 = 100000

// CoresToPeriodAndQuota converts a fraction of cores to the equivalent
// Completely Fair Scheduler (CFS) parameters period and quota.
//
// Cores is a fraction of the CFS period that a container may use. Period and
// Quota are in microseconds.
func CoresToPeriodAndQuota(cores float64) (uint64, int64) {
	return DefaultCPUPeriod, int64(cores * float64(DefaultCPUPeriod))
}

// PeriodAndQuotaToCores takes the CFS parameters period and quota and returns
// a fraction that represents the limit to the number of cores that can be
// utilized over the scheduling period.
//
// Cores is a fraction of the CFS period that a container may use. Period and
// Quota are in microseconds.
func PeriodAndQuotaToCores(period uint64, quota int64) float64 {
	return float64(quota) / float64(period)
}

// IDtoolsToRuntimeSpec converts idtools ID mapping to the one of the runtime spec.
func IDtoolsToRuntimeSpec(idMaps []idtools.IDMap) (convertedIDMap []specs.LinuxIDMapping) {
	for _, idmap := range idMaps {
		tempIDMap := specs.LinuxIDMapping{
			ContainerID: uint32(idmap.ContainerID),
			HostID:      uint32(idmap.HostID),
			Size:        uint32(idmap.Size),
		}
		convertedIDMap = append(convertedIDMap, tempIDMap)
	}
	return convertedIDMap
}

// RuntimeSpecToIDtoolsTo converts runtime spec to the one of the idtools ID mapping
func RuntimeSpecToIDtools(idMaps []specs.LinuxIDMapping) (convertedIDMap []idtools.IDMap) {
	for _, idmap := range idMaps {
		tempIDMap := idtools.IDMap{
			ContainerID: int(idmap.ContainerID),
			HostID:      int(idmap.HostID),
			Size:        int(idmap.Size),
		}
		convertedIDMap = append(convertedIDMap, tempIDMap)
	}
	return convertedIDMap
}

func LookupUser(name string) (*user.User, error) {
	// Assume UID lookup first, if it fails look up by username
	if u, err := user.LookupId(name); err == nil {
		return u, nil
	}
	return user.Lookup(name)
}

// SizeOfPath determines the file usage of a given path. it was called volumeSize in v1
// and now is made to be generic and take a path instead of a libpod volume
// Deprecated: use github.com/containers/storage/pkg/directory.Size() instead.
func SizeOfPath(path string) (uint64, error) {
	size, err := directory.Size(path)
	return uint64(size), err
}

// ParseRestartPolicy parses the value given to the --restart flag and returns the policy
// and restart retries value
func ParseRestartPolicy(policy string) (string, uint, error) {
	var (
		retriesUint uint
		policyType  string
	)
	splitRestart := strings.Split(policy, ":")
	switch len(splitRestart) {
	case 1:
		// No retries specified
		policyType = splitRestart[0]
		if strings.ToLower(splitRestart[0]) == "never" {
			policyType = define.RestartPolicyNo
		}
	case 2:
		if strings.ToLower(splitRestart[0]) != "on-failure" {
			return "", 0, errors.New("restart policy retries can only be specified with on-failure restart policy")
		}
		retries, err := strconv.Atoi(splitRestart[1])
		if err != nil {
			return "", 0, fmt.Errorf("parsing restart policy retry count: %w", err)
		}
		if retries < 0 {
			return "", 0, errors.New("must specify restart policy retry count as a number greater than 0")
		}
		retriesUint = uint(retries)
		policyType = splitRestart[0]
	default:
		return "", 0, errors.New("invalid restart policy: may specify retries at most once")
	}
	return policyType, retriesUint, nil
}

// ConvertTimeout converts negative timeout to MaxUint32, which indicates approximately infinity, waiting to stop containers
func ConvertTimeout(timeout int) uint {
	if timeout < 0 {
		return math.MaxUint32
	}
	return uint(timeout)
}

// ExecAddTERM when container does not have a TERM environment variable and
// caller wants a tty, then leak the existing TERM environment into
// the container.
func ExecAddTERM(existingEnv []string, execEnvs map[string]string) {
	if _, ok := execEnvs["TERM"]; ok {
		return
	}

	for _, val := range existingEnv {
		if strings.HasPrefix(val, "TERM=") {
			return
		}
	}

	execEnvs["TERM"] = "xterm"
}
