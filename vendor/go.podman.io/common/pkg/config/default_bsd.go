//go:build freebsd || netbsd || openbsd

package config

// DefaultInitPath is the default path to the container-init binary.
var DefaultInitPath = "/usr/local/libexec/podman/catatonit"

func getDefaultCgroupsMode() string {
	return "enabled"
}

// In theory, FreeBSD should be able to use shm locks but in practice,
// this causes cryptic error messages from the kernel that look like:
//
//	comm podman pid 90813: handling rb error 22
//
// These seem to be related to fork/exec code paths. Fall back to
// file-based locks.
func getDefaultLockType() string {
	return "file"
}

func getLibpodTmpDir() string {
	return "/var/run/libpod"
}

// getDefaultMachineVolumes returns default mounted volumes (possibly with env vars, which will be expanded)
func getDefaultMachineVolumes() []string {
	return []string{"$HOME:$HOME"}
}
