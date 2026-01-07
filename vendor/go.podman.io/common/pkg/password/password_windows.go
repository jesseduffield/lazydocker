//go:build windows

package password

import (
	terminal "golang.org/x/term"
)

// Read reads a password from the terminal.
func Read(fd int) ([]byte, error) {
	oldState, err := terminal.GetState(fd)
	if err != nil {
		return make([]byte, 0), err
	}
	buf, err := terminal.ReadPassword(fd)
	if oldState != nil {
		_ = terminal.Restore(fd, oldState)
	}
	return buf, err
}
