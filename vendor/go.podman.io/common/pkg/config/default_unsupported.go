//go:build !linux && !windows

package config

import "os"

// getDefaultTmpDir for linux.
func getDefaultTmpDir() string {
	// first check the TMPDIR env var
	if path, found := os.LookupEnv("TMPDIR"); found {
		return path
	}
	return "/var/tmp"
}
