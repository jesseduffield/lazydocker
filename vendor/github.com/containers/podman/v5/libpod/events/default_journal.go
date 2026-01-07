//go:build systemd && linux

package events

// DefaultEventerType is journald when systemd is available.
const DefaultEventerType = Journald
