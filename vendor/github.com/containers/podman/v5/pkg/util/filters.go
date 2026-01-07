package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// filtersFromRequests extracts the "filters" parameter from the specified
// http.Request.  The parameter can either be a `map[string][]string` as done
// in new versions of Docker and libpod, or a `map[string]map[string]bool` as
// done in older versions of Docker.  We have to do a bit of Yoga to support
// both - just as Docker does as well.
//
// Please refer to https://github.com/containers/podman/issues/6899 for some
// background.
func FiltersFromRequest(r *http.Request) ([]string, error) {
	var (
		compatFilters map[string]map[string]bool
		filters       map[string][]string
		libpodFilters []string
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

	for filterKey, filterSlice := range filters {
		for _, filterValue := range filterSlice {
			libpodFilters = append(libpodFilters, fmt.Sprintf("%s=%s", filterKey, filterValue))
		}
	}

	return libpodFilters, nil
}

// PrepareFilters prepares a *map[string][]string of filters to be later searched
// in lipod and compat API to get desired filters
func PrepareFilters(r *http.Request) (*map[string][]string, error) {
	filtersList, err := FiltersFromRequest(r)
	if err != nil {
		return nil, err
	}
	filterMap := map[string][]string{}
	for _, filter := range filtersList {
		fname, filter, hasFilter := strings.Cut(filter, "=")
		if hasFilter {
			filterMap[fname] = append(filterMap[fname], filter)
		}
	}
	return &filterMap, nil
}
