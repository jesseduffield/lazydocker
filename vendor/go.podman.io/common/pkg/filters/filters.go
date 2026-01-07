package filters

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/timetype"
)

// ComputeUntilTimestamp extracts until timestamp from filters.
func ComputeUntilTimestamp(filterValues []string) (time.Time, error) {
	invalid := time.Time{}
	if len(filterValues) != 1 {
		return invalid, errors.New("specify exactly one timestamp for until")
	}
	ts, err := timetype.GetTimestamp(filterValues[0], time.Now())
	if err != nil {
		return invalid, err
	}
	seconds, nanoseconds, err := timetype.ParseTimestamps(ts, 0)
	if err != nil {
		return invalid, err
	}
	return time.Unix(seconds, nanoseconds), nil
}

// FiltersFromRequest extracts the "filters" parameter from the specified
// http.Request.  The parameter can either be a `map[string][]string` as done
// in new versions of Docker and libpod, or a `map[string]map[string]bool` as
// done in older versions of Docker.  We have to do a bit of Yoga to support
// both - just as Docker does as well.
//
// Please refer to https://github.com/containers/podman/issues/6899 for some
// background.
//
// revive does not like the name because the package is already called filters
//
//nolint:revive
func FiltersFromRequest(r *http.Request) ([]string, error) {
	var (
		compatFilters map[string]map[string]bool
		filters       map[string][]string
		raw           []byte
	)

	if _, found := r.URL.Query()["filters"]; found {
		raw = []byte(r.Form.Get("filters"))
	} else if _, found := r.URL.Query()["Filters"]; found {
		raw = []byte(r.Form.Get("Filters"))
	} else {
		return []string{}, nil
	}

	// Backwards compat with older versions of Docker.
	if err := json.Unmarshal(raw, &compatFilters); err == nil {
		libpodFilters := make([]string, 0, len(compatFilters))
		for filterKey, filterMap := range compatFilters {
			for filterValue, toAdd := range filterMap {
				if toAdd {
					libpodFilters = append(libpodFilters, fmt.Sprintf("%s=%s", filterKey, filterValue))
				}
			}
		}
		return libpodFilters, nil
	}

	if err := json.Unmarshal(raw, &filters); err != nil {
		return nil, err
	}

	libpodFilters := make([]string, 0, len(filters))
	for filterKey, filterSlice := range filters {
		for _, filterValue := range filterSlice {
			libpodFilters = append(libpodFilters, fmt.Sprintf("%s=%s", filterKey, filterValue))
		}
	}
	return libpodFilters, nil
}

// PrepareFilters prepares a *map[string][]string of filters to be later searched
// in lipod and compat API to get desired filters.
func PrepareFilters(r *http.Request) (map[string][]string, error) {
	filtersList, err := FiltersFromRequest(r)
	if err != nil {
		return nil, err
	}
	filterMap := map[string][]string{}
	for _, filter := range filtersList {
		key, val, found := strings.Cut(filter, "=")
		if found {
			filterMap[key] = append(filterMap[key], val)
		}
	}
	return filterMap, nil
}

// MatchLabelFilters matches labels and returns true if they are valid.
func MatchLabelFilters(filterValues []string, labels map[string]string) bool {
outer:
	for _, filterValue := range filterValues {
		filterKey, filterValue, _ := strings.Cut(filterValue, "=")
		for labelKey, labelValue := range labels {
			if filterValue == "" || labelValue == filterValue {
				if labelKey == filterKey || matchPattern(filterKey, labelKey) {
					continue outer
				}
			}
		}
		return false
	}
	return true
}

// MatchNegatedLabelFilters matches negated labels and returns true if they are valid.
func MatchNegatedLabelFilters(filterValues []string, labels map[string]string) bool {
	for _, filterValue := range filterValues {
		filterKey, filterValue, _ := strings.Cut(filterValue, "=")
		for labelKey, labelValue := range labels {
			if filterValue == "" || labelValue == filterValue {
				if labelKey == filterKey || matchPattern(filterKey, labelKey) {
					return false
				}
			}
		}
	}
	return true
}

func matchPattern(pattern string, value string) bool {
	if strings.Contains(pattern, "*") {
		filter := fmt.Sprintf("*%s*", pattern)
		filter = strings.ReplaceAll(filter, string(filepath.Separator), "|")
		newName := strings.ReplaceAll(value, string(filepath.Separator), "|")
		match, _ := filepath.Match(filter, newName)
		return match
	}
	return false
}

// FilterID is a function used to compare an id against a set of ids, if the
// input is hex we check if the prefix matches. Otherwise we assume it is a
// regex and try to match that.
// see https://github.com/containers/podman/issues/18471 for why we do this.
func FilterID(id string, filters []string) bool {
	for _, want := range filters {
		isRegex := types.NotHexRegex.MatchString(want)
		if isRegex {
			match, err := regexp.MatchString(want, id)
			if err == nil && match {
				return true
			}
		} else if strings.HasPrefix(id, strings.ToLower(want)) {
			return true
		}
	}
	return false
}
