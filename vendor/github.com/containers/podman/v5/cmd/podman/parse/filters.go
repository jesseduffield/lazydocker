package parse

import (
	"fmt"
	"net/url"
	"strings"
)

func FilterArgumentsIntoFilters(filters []string) (url.Values, error) {
	parsedFilters := make(url.Values)
	for _, f := range filters {
		fname, filter, hasFilter := strings.Cut(f, "=")
		if !hasFilter {
			return parsedFilters, fmt.Errorf("filter input must be in the form of filter=value: %s is invalid", f)
		}
		parsedFilters.Add(fname, filter)
	}
	return parsedFilters, nil
}
