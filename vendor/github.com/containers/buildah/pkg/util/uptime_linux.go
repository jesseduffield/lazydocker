package util

import (
	"bytes"
	"errors"
	"os"
	"time"
)

func ReadUptime() (time.Duration, error) {
	buf, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	f := bytes.Fields(buf)
	if len(f) < 1 {
		return 0, errors.New("invalid uptime")
	}

	// Convert uptime in seconds to a human-readable format
	up := string(f[0])
	upSeconds := up + "s"
	upDuration, err := time.ParseDuration(upSeconds)
	if err != nil {
		return 0, err
	}
	return upDuration, nil
}
