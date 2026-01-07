//go:build (linux && !systemd) || freebsd

package events

// newJournalDEventer always returns an error if libsystemd not found
func newJournalDEventer(_ EventerOptions) (Eventer, error) {
	return nil, ErrNoJournaldLogging
}
