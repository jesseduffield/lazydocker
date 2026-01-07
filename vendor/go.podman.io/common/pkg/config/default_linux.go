package config

import (
	"os"
)

func getDefaultCgroupsMode() string {
	return "enabled"
}

// getDefaultTmpDir for linux.
func getDefaultTmpDir() string {
	// first check the TMPDIR env var
	if path, found := os.LookupEnv("TMPDIR"); found {
		return path
	}
	return "/var/tmp"
}

func getDefaultLockType() string {
	return "shm"
}

func getLibpodTmpDir() string {
	return "/run/libpod"
}

// getDefaultMachineVolumes returns default mounted volumes (possibly with env vars, which will be expanded).
func getDefaultMachineVolumes() []string {
	return []string{"$HOME:$HOME"}
}
