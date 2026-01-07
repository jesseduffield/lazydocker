//go:build !systemd || !linux

package events

// DefaultEventerType is logfile when systemd is not present
// or not supported.
const DefaultEventerType = LogFile
