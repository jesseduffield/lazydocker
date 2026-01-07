//go:build linux || freebsd || darwin

package util

import (
	"fmt"
	"syscall"

	"github.com/docker/go-units"
)

func ParseUlimit(ulimit string) (*units.Ulimit, error) {
	ul, err := units.ParseUlimit(ulimit)
	if err != nil {
		return nil, fmt.Errorf("ulimit option %q requires name=SOFT:HARD, failed to be parsed: %w", ulimit, err)
	}

	if ul.Hard != -1 && ul.Soft == -1 {
		return ul, nil
	}

	rl, err := ul.GetRlimit()
	if err != nil {
		return nil, err
	}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(rl.Type, &limit); err != nil {
		return nil, err
	}
	if ul.Soft == -1 {
		ul.Soft = int64(limit.Cur)
	}
	if ul.Hard == -1 {
		ul.Hard = int64(limit.Max)
	}
	return ul, nil
}
