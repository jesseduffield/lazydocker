package idtools

import (
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

func parseTriple(spec []string) (container, host, size uint32, err error) {
	cid, err := strconv.ParseUint(spec[0], 10, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parsing id map value %q: %w", spec[0], err)
	}
	hid, err := strconv.ParseUint(spec[1], 10, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parsing id map value %q: %w", spec[1], err)
	}
	sz, err := strconv.ParseUint(spec[2], 10, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parsing id map value %q: %w", spec[2], err)
	}
	return uint32(cid), uint32(hid), uint32(sz), nil
}

// ParseIDMap parses idmap triples from string.
func ParseIDMap(mapSpec []string, mapSetting string) (idmap []IDMap, err error) {
	stdErr := fmt.Errorf("initializing ID mappings: %s setting is malformed expected [\"uint32:uint32:uint32\"]: %q", mapSetting, mapSpec)
	for _, idMapSpec := range mapSpec {
		if idMapSpec == "" {
			continue
		}
		idSpec := strings.Split(idMapSpec, ":")
		if len(idSpec)%3 != 0 {
			return nil, stdErr
		}
		for i := range idSpec {
			if i%3 != 0 {
				continue
			}
			cid, hid, size, err := parseTriple(idSpec[i : i+3])
			if err != nil {
				return nil, stdErr
			}
			// Avoid possible integer overflow on 32bit builds
			if bits.UintSize == 32 && (cid > math.MaxInt32 || hid > math.MaxInt32 || size > math.MaxInt32) {
				return nil, stdErr
			}
			mapping := IDMap{
				ContainerID: int(cid),
				HostID:      int(hid),
				Size:        int(size),
			}
			idmap = append(idmap, mapping)
		}
	}
	return idmap, nil
}
