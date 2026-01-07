package filter

import (
	"fmt"
	"strconv"
	"strings"

	"go.podman.io/common/libimage/define"
	"go.podman.io/image/v5/types"
)

// SearchFilter allows filtering images while searching.
type SearchFilter struct {
	// Stars describes the minimal amount of starts of an image.
	Stars int
	// IsAutomated decides if only images from automated builds are displayed.
	IsAutomated types.OptionalBool
	// IsOfficial decides if only official images are displayed.
	IsOfficial types.OptionalBool
}

// ParseSearchFilter turns the filter into a SearchFilter that can be used for
// searching images.
func ParseSearchFilter(filter []string) (*SearchFilter, error) {
	sFilter := new(SearchFilter)
	for _, f := range filter {
		keyword, value, ok := strings.Cut(f, "=")
		switch keyword {
		case define.SearchFilterStars:
			if !ok {
				return nil, fmt.Errorf("invalid filter %q, should be stars=<value>", filter)
			}
			stars, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("incorrect value type for stars filter: %w", err)
			}
			sFilter.Stars = stars
		case define.SearchFilterAutomated:
			if ok && value == "false" {
				sFilter.IsAutomated = types.OptionalBoolFalse
			} else {
				sFilter.IsAutomated = types.OptionalBoolTrue
			}
		case define.SearchFilterOfficial:
			if ok && value == "false" {
				sFilter.IsOfficial = types.OptionalBoolFalse
			} else {
				sFilter.IsOfficial = types.OptionalBoolTrue
			}
		default:
			return nil, fmt.Errorf("invalid filter type %q", f)
		}
	}
	return sFilter, nil
}
