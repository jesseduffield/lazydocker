package capabilities

// Copyright 2013-2018 Docker, Inc.

// NOTE: this package has been copied from github.com/docker/docker but been
//       changed significantly to fit the needs of libpod.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/moby/sys/capability"
)

var (
	// ErrUnknownCapability is thrown when an unknown capability is processed.
	ErrUnknownCapability = errors.New("unknown capability")

	// ContainerImageLabels - label can indicate the required
	// capabilities required by containers to run the container image.
	ContainerImageLabels = []string{"io.containers.capabilities"}
)

// All is a special value used to add/drop all known capabilities.
// Useful on the CLI for `--cap-add=all` etc.
const All = "ALL"

func capName(c capability.Cap) string {
	return "CAP_" + strings.ToUpper(c.String())
}

// capStrList returns all capabilities supported by the currently running kernel,
// or an error if the list can not be obtained.
var capStrList = sync.OnceValues(func() ([]string, error) {
	list, err := capability.ListSupported()
	if err != nil {
		return nil, err
	}
	caps := make([]string, len(list))
	for i, c := range list {
		caps[i] = capName(c)
	}
	slices.Sort(caps)
	return caps, nil
})

// BoundingSet returns the capabilities in the current bounding set.
func BoundingSet() ([]string, error) {
	return boundingSet()
}

var boundingSet = sync.OnceValues(func() ([]string, error) {
	currentCaps, err := capability.NewPid2(0)
	if err != nil {
		return nil, err
	}
	err = currentCaps.Load()
	if err != nil {
		return nil, err
	}
	list, err := capability.ListSupported()
	if err != nil {
		return nil, err
	}
	var r []string
	for _, c := range list {
		if !currentCaps.Get(capability.BOUNDING, c) {
			continue
		}
		r = append(r, capName(c))
	}
	slices.Sort(r)
	return r, nil
})

// AllCapabilities returns all capabilities supported by the running kernel.
func AllCapabilities() []string {
	list, _ := capStrList()
	return list
}

// NormalizeCapabilities normalizes caps by adding a "CAP_" prefix (if not yet
// present).
func NormalizeCapabilities(caps []string) ([]string, error) {
	all, err := capStrList()
	if err != nil {
		return nil, err
	}
	normalized := make([]string, 0, len(caps))
	for _, c := range caps {
		c = strings.ToUpper(c)
		if c == All {
			normalized = append(normalized, c)
			continue
		}
		if !strings.HasPrefix(c, "CAP_") {
			c = "CAP_" + c
		}
		if !slices.Contains(all, c) {
			return nil, fmt.Errorf("%q: %w", c, ErrUnknownCapability)
		}
		normalized = append(normalized, c)
	}
	slices.Sort(normalized)
	return normalized, nil
}

// ValidateCapabilities validates if caps only contains valid capabilities.
func ValidateCapabilities(caps []string) error {
	all, err := capStrList()
	if err != nil {
		return err
	}
	for _, c := range caps {
		if !slices.Contains(all, c) {
			return fmt.Errorf("%q: %w", c, ErrUnknownCapability)
		}
	}
	return nil
}

// MergeCapabilities computes a set of capabilities by adding capabilities
// to or dropping them from base.
//
// Note that:
// "ALL" in capAdd adds returns known capabilities
// "All" in capDrop returns only the capabilities specified in capAdd.
func MergeCapabilities(base, adds, drops []string) ([]string, error) {
	// Normalize the base capabilities
	base, err := NormalizeCapabilities(base)
	if err != nil {
		return nil, err
	}
	if len(adds) == 0 && len(drops) == 0 {
		// Nothing to tweak; we're done
		return base, nil
	}
	capDrop, err := NormalizeCapabilities(drops)
	if err != nil {
		return nil, err
	}
	capAdd, err := NormalizeCapabilities(adds)
	if err != nil {
		return nil, err
	}

	if slices.Contains(capDrop, All) {
		if slices.Contains(capAdd, All) {
			return nil, errors.New("adding all caps and removing all caps not allowed")
		}
		// "Drop" all capabilities; return what's in capAdd instead
		slices.Sort(capAdd)
		return capAdd, nil
	}

	if slices.Contains(capAdd, All) {
		base, err = BoundingSet()
		if err != nil {
			return nil, err
		}
		capAdd = []string{}
	} else {
		for _, add := range capAdd {
			if slices.Contains(capDrop, add) {
				return nil, fmt.Errorf("capability %q cannot be dropped and added", add)
			}
		}
	}

	for _, drop := range capDrop {
		if slices.Contains(capAdd, drop) {
			return nil, fmt.Errorf("capability %q cannot be dropped and added", drop)
		}
	}

	caps := make([]string, 0, len(base)+len(capAdd))
	// Drop any capabilities in capDrop that are in base
	for _, cap := range base {
		if slices.Contains(capDrop, cap) {
			continue
		}
		caps = append(caps, cap)
	}

	// Add any capabilities in capAdd that are not in base
	for _, cap := range capAdd {
		if slices.Contains(base, cap) {
			continue
		}
		caps = append(caps, cap)
	}
	slices.Sort(caps)
	return caps, nil
}
