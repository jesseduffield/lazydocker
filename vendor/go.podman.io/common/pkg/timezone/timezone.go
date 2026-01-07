//go:build linux || freebsd

package timezone

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
	"golang.org/x/sys/unix"
)

// ConfigureContainerTimeZone configure the time zone for a container.
// It returns the path of the created /etc/localtime file if needed.
func ConfigureContainerTimeZone(timezone, containerRunDir, mountPoint, etcPath, containerID string) (localTimePath string, err error) {
	var timezonePath string
	switch timezone {
	case "":
		return "", nil
	case "local":
		timezonePath, err = filepath.EvalSymlinks("/etc/localtime")
		if err != nil {
			return "", fmt.Errorf("finding local timezone for container %s: %w", containerID, err)
		}
	default:
		// Allow using TZDIR per:
		// https://sourceware.org/git/?p=glibc.git;a=blob;f=time/tzfile.c;h=8a923d0cccc927a106dc3e3c641be310893bab4e;hb=HEAD#l149
		zoneinfo := os.Getenv("TZDIR")
		if zoneinfo == "" {
			// default zoneinfo location
			zoneinfo = "/usr/share/zoneinfo"
		}
		timezonePath = filepath.Join(zoneinfo, timezone)
	}

	etcFd, err := openDirectory(etcPath)
	if err != nil {
		return "", fmt.Errorf("open /etc in the container: %w", err)
	}
	defer unix.Close(etcFd)

	// Make sure to remove any existing localtime file in the container to not create invalid links
	err = unix.Unlinkat(etcFd, "localtime", 0)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("removing /etc/localtime: %w", err)
	}

	hostPath, err := securejoin.SecureJoin(mountPoint, timezonePath)
	if err != nil {
		return "", fmt.Errorf("resolve zoneinfo path in the container: %w", err)
	}

	var localtimePath string
	if err := fileutils.Exists(hostPath); err != nil {
		// File does not exist, which means tzdata is not installed in the container.
		// Create /etc/localtime as a copy from the host.
		logrus.Debugf("Timezone %s does not exist in the container, create our own copy from the host", timezonePath)
		localtimePath, err = copyTimezoneFile(containerRunDir, timezonePath)
		if err != nil {
			return "", fmt.Errorf("setting timezone for container %s: %w", containerID, err)
		}
	} else {
		// File exists, let's create a symlink according to localtime(5)
		logrus.Debugf("Create localtime symlink for %s", timezonePath)
		err = unix.Symlinkat(".."+timezonePath, etcFd, "localtime")
		if err != nil {
			return "", fmt.Errorf("creating /etc/localtime symlink: %w", err)
		}
	}
	return localtimePath, nil
}

// copyTimezoneFile copies the timezone file from the host to the container.
func copyTimezoneFile(containerRunDir, zonePath string) (string, error) {
	localtimeCopy := filepath.Join(containerRunDir, "localtime")
	file, err := os.Stat(zonePath)
	if err != nil {
		return "", err
	}
	if file.IsDir() {
		return "", errors.New("invalid timezone: is a directory")
	}
	src, err := os.Open(zonePath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dest, err := os.Create(localtimeCopy)
	if err != nil {
		return "", err
	}
	defer dest.Close()

	_, err = io.Copy(dest, src)
	if err != nil {
		return "", err
	}
	return localtimeCopy, err
}

func openDirectory(path string) (fd int, err error) {
	return unix.Open(path, unix.O_RDONLY|O_PATH|unix.O_CLOEXEC, 0)
}
