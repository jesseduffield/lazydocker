package unshare

import (
	"fmt"
	"os"
	"os/user"
	"sync"
)

var (
	homeDirOnce sync.Once
	homeDirErr  error
	homeDir     string
)

// HomeDir returns the home directory for the current user.
func HomeDir() (string, error) {
	homeDirOnce.Do(func() {
		home := os.Getenv("HOME")
		if home == "" {
			usr, err := user.LookupId(fmt.Sprintf("%d", GetRootlessUID()))
			if err != nil {
				homeDir, homeDirErr = "", fmt.Errorf("unable to resolve HOME directory: %w", err)
				return
			}
			homeDir, homeDirErr = usr.HomeDir, nil
			return
		}
		homeDir, homeDirErr = home, nil
	})
	return homeDir, homeDirErr
}
