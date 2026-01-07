package util

import (
	"fmt"

	"github.com/docker/go-units"
)

func ParseUlimit(ulimit string) (*units.Ulimit, error) {
	ul, err := units.ParseUlimit(ulimit)
	if err != nil {
		return nil, fmt.Errorf("ulimit option %q requires name=SOFT:HARD, failed to be parsed: %w", ulimit, err)
	}

	return ul, nil
}
