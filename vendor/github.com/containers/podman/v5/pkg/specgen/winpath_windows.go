package specgen

import (
	"path/filepath"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
)

func shouldResolveUnixWinVariant(_ string) bool {
	return true
}

func shouldResolveWinPaths() bool {
	return true
}

func resolveRelativeOnWindows(path string) string {
	ret, err := filepath.Abs(path)
	if err != nil {
		logrus.Debugf("problem resolving possible relative path %q: %s", path, err.Error())
		return path
	}

	return ret
}

func winPathExists(path string) bool {
	return fileutils.Exists(path) == nil
}
