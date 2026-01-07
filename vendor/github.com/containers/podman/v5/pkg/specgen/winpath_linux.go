package specgen

import (
	"go.podman.io/common/pkg/machine"
	"go.podman.io/storage/pkg/fileutils"
)

func shouldResolveWinPaths() bool {
	return machine.HostType() == "wsl"
}

func shouldResolveUnixWinVariant(path string) bool {
	return fileutils.Exists(path) != nil
}

func resolveRelativeOnWindows(path string) string {
	return path
}

func winPathExists(_ string) bool {
	return false
}
