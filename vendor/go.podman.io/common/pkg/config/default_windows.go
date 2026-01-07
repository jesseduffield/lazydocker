package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.podman.io/storage/pkg/homedir"
)

// getDefaultTmpDir for windows
func getDefaultTmpDir() string {
	// first check the Temp env var
	// https://answers.microsoft.com/en-us/windows/forum/all/where-is-the-temporary-folder/44a039a5-45ba-48dd-84db-fd700e54fd56
	if val, ok := os.LookupEnv("TEMP"); ok {
		return val
	}
	return os.Getenv("LOCALAPPDATA") + "\\Temp"
}

func getDefaultCgroupsMode() string {
	return "enabled"
}

func getDefaultLockType() string {
	return "shm"
}

func getLibpodTmpDir() string {
	return "/run/libpod"
}

// getDefaultMachineVolumes returns default mounted volumes (possibly with env vars, which will be expanded)
// It is executed only if the machine provider is Hyper-V and it mimics WSL
// behavior where the host %USERPROFILE% drive (e.g. C:\) is automatically
// mounted in the guest under /mnt/ (e.g. /mnt/c/)
func getDefaultMachineVolumes() []string {
	hd := homedir.Get()
	vol := filepath.VolumeName(hd)
	hostMnt := filepath.ToSlash(strings.TrimPrefix(hd, vol))
	return []string{
		fmt.Sprintf("%s:%s", hd, hostMnt),
		fmt.Sprintf("%s:%s", vol+"\\", "/mnt/"+strings.ToLower(vol[0:1])),
	}
}

func getDefaultComposeProviders() []string {
	// Rely on os.LookPath to do the trick on Windows.
	return []string{"docker-compose", "podman-compose"}
}
