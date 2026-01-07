// Package parsers provides helper functions to parse and validate different type
// of string. It can be hosts, unix addresses, tcp addresses, filters, kernel
// operating system versions.
package parsers

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseKeyValueOpt parses and validates the specified string as a key/value pair (key=value)
func ParseKeyValueOpt(opt string) (string, string, error) {
	k, v, ok := strings.Cut(opt, "=")
	if !ok {
		return "", "", fmt.Errorf("unable to parse key/value option: %s", opt)
	}
	return strings.TrimSpace(k), strings.TrimSpace(v), nil
}

// ParseUintList parses and validates the specified string as the value
// found in some cgroup file (e.g. `cpuset.cpus`, `cpuset.mems`), which could be
// one of the formats below. Note that duplicates are actually allowed in the
// input string. It returns a `map[int]bool` with available elements from `val`
// set to `true`.
// Supported formats:
//
//	7
//	1-6
//	0,3-4,7,8-10
//	0-0,0,1-7
//	03,1-3      <- this is gonna get parsed as [1,2,3]
//	3,2,1
//	0-2,3,1
func ParseUintList(val string) (map[int]bool, error) {
	if val == "" {
		return map[int]bool{}, nil
	}

	availableInts := make(map[int]bool)
	errInvalidFormat := fmt.Errorf("invalid format: %s", val)

	for r := range strings.SplitSeq(val, ",") {
		minS, maxS, ok := strings.Cut(r, "-")
		if !ok {
			v, err := strconv.Atoi(r)
			if err != nil {
				return nil, errInvalidFormat
			}
			availableInts[v] = true
		} else {
			min, err := strconv.Atoi(minS)
			if err != nil {
				return nil, errInvalidFormat
			}
			max, err := strconv.Atoi(maxS)
			if err != nil {
				return nil, errInvalidFormat
			}
			if max < min {
				return nil, errInvalidFormat
			}
			for i := min; i <= max; i++ {
				availableInts[i] = true
			}
		}
	}
	return availableInts, nil
}
