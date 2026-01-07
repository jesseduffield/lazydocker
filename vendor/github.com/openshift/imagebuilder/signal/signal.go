// Package signal provides helper functions for dealing with signals across
// various operating systems.
package signal

import (
	"fmt"
	"strconv"
	"strings"
)

// CheckSignal translates a string to a valid syscall signal.
// It returns an error if the signal map doesn't include the given signal.
func CheckSignal(rawSignal string) error {
	s, err := strconv.Atoi(rawSignal)
	if err == nil {
		if s == 0 {
			return fmt.Errorf("Invalid signal: %s", rawSignal)
		}
		return nil
	}
	if _, ok := SignalMap[strings.TrimPrefix(strings.ToUpper(rawSignal), "SIG")]; !ok {
		return fmt.Errorf("Invalid signal: %s", rawSignal)
	}
	return nil
}
