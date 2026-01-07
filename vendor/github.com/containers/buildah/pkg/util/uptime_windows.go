package util

import (
	"errors"
	"time"
)

func ReadUptime() (time.Duration, error) {
	return 0, errors.New("readUptime not supported on windows")
}
