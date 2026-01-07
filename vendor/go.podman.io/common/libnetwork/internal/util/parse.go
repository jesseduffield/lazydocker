package util

import (
	"fmt"
	"strconv"
)

// ParseMTU parses the mtu option.
func ParseMTU(mtu string) (int, error) {
	if mtu == "" {
		return 0, nil // default
	}
	m, err := strconv.Atoi(mtu)
	if err != nil {
		return 0, err
	}
	if m < 0 {
		return 0, fmt.Errorf("mtu %d is less than zero", m)
	}
	return m, nil
}

// ParseVlan parses the vlan option.
func ParseVlan(vlan string) (int, error) {
	if vlan == "" {
		return 0, nil // default
	}
	v, err := strconv.Atoi(vlan)
	if err != nil {
		return 0, err
	}
	if v < 0 || v > 4094 {
		return 0, fmt.Errorf("vlan ID %d must be between 0 and 4094", v)
	}
	return v, nil
}

// ParseIsolate parses the isolate option.
func ParseIsolate(isolate string) (string, error) {
	switch isolate {
	case "":
		return "false", nil
	case "strict":
		return isolate, nil
	default:
		// isolate option accepts "strict" and Rust boolean values "true" or "false"
		optIsolateBool, err := strconv.ParseBool(isolate)
		if err != nil {
			return "", fmt.Errorf("failed to parse isolate option: %w", err)
		}
		// Rust boolean only support "true" or "false" while go can parse 1 and 0 as well so we need to change it
		return strconv.FormatBool(optIsolateBool), nil
	}
}
