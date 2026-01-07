package volumes

import (
	"errors"
	"fmt"
	"os"

	"github.com/containers/buildah/internal/open"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/mount"
	"golang.org/x/sys/unix"
)

// bindFromChroot opens "path" inside of "root" using a chrooted subprocess
// that returns a descriptor, then creates a uniquely-named temporary directory
// or file under "tmp" and bind-mounts the opened descriptor to it, returning
// the path of the temporary file or directory.  The caller is responsible for
// unmounting and removing the temporary.
func bindFromChroot(root, path, tmp string) (string, error) {
	fd, _, err := open.InChroot(root, "", path, unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		if !errors.Is(err, unix.ENOTDIR) {
			return "", fmt.Errorf("opening directory %q under %q: %w", path, root, err)
		}
		fd, _, err = open.InChroot(root, "", path, unix.O_RDWR, 0)
		if err != nil {
			return "", fmt.Errorf("opening non-directory %q under %q: %w", path, root, err)
		}
	}
	defer func() {
		if err := unix.Close(fd); err != nil {
			logrus.Debugf("closing %q under %q: %v", path, root, err)
		}
	}()

	succeeded := false
	var dest string
	var destF *os.File
	defer func() {
		if !succeeded {
			if destF != nil {
				if err := destF.Close(); err != nil {
					logrus.Debugf("closing bind target %q: %v", dest, err)
				}
			}
			if dest != "" {
				if err := os.Remove(dest); err != nil {
					logrus.Debugf("removing bind target %q: %v", dest, err)
				}
			}
		}
	}()

	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return "", fmt.Errorf("checking if %q under %q was a directory: %w", path, root, err)
	}

	if st.Mode&unix.S_IFDIR == unix.S_IFDIR {
		if dest, err = os.MkdirTemp(tmp, "bind"); err != nil {
			return "", fmt.Errorf("creating a bind target directory: %w", err)
		}
	} else {
		if destF, err = os.CreateTemp(tmp, "bind"); err != nil {
			return "", fmt.Errorf("creating a bind target non-directory: %w", err)
		}
		if err := destF.Close(); err != nil {
			logrus.Debugf("closing bind target %q: %v", dest, err)
		}
		dest = destF.Name()
	}
	defer func() {
		if !succeeded {
			if err := os.Remove(dest); err != nil {
				logrus.Debugf("removing bind target %q: %v", dest, err)
			}
		}
	}()

	if err := unix.Mount(fmt.Sprintf("/proc/self/fd/%d", fd), dest, "bind", unix.MS_BIND, ""); err != nil {
		return "", fmt.Errorf("bind-mounting passed-in descriptor to %q: %w", dest, err)
	}
	defer func() {
		if !succeeded {
			if err := mount.Unmount(dest); err != nil {
				logrus.Debugf("unmounting bound target %q: %v", dest, err)
			}
		}
	}()

	var st2 unix.Stat_t
	if err = unix.Stat(dest, &st2); err != nil {
		return "", fmt.Errorf("looking up device/inode of newly-bind-mounted %q: %w", dest, err)
	}

	if st2.Dev != st.Dev || st2.Ino != st.Ino {
		return "", fmt.Errorf("device/inode weren't what we expected after bind mounting: %w", err)
	}

	succeeded = true
	return dest, nil
}
