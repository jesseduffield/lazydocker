//go:build linux

package binfmt

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

// MaybeRegister() calls Register() if the current context is a rootless one,
// or if the "container" environment variable suggests that we're in a
// container.
func MaybeRegister(configurationSearchDirectories []string) error {
	if unshare.IsRootless() || os.Getenv("container") != "" { // we _also_ own our own mount namespace
		return Register(configurationSearchDirectories)
	}
	return nil
}

// Register() registers binfmt.d emulators described by configuration files in
// the passed-in slice of directories, or in the union of /etc/binfmt.d,
// /run/binfmt.d, and /usr/lib/binfmt.d if the slice has no items.  If any
// emulators are configured, it will attempt to mount a binfmt_misc filesystem
// in the current mount namespace first, ignoring only EPERM and EACCES errors.
func Register(configurationSearchDirectories []string) error {
	if len(configurationSearchDirectories) == 0 {
		configurationSearchDirectories = []string{"/etc/binfmt.d", "/run/binfmt.d", "/usr/lib/binfmt.d"}
	}
	mounted := false
	for _, searchDir := range configurationSearchDirectories {
		globs, err := filepath.Glob(filepath.Join(searchDir, "*.conf"))
		if err != nil {
			return fmt.Errorf("looking for binfmt.d configuration in %q: %w", searchDir, err)
		}
		for _, conf := range globs {
			f, err := os.Open(conf)
			if err != nil {
				return fmt.Errorf("reading binfmt.d configuration: %w", err)
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if len(line) == 0 || line[0] == ';' || line[0] == '#' {
					continue
				}
				if !mounted {
					if err = unix.Mount("none", "/proc/sys/fs/binfmt_misc", "binfmt_misc", 0, ""); err != nil {
						if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
							// well, we tried. no need to make a stink about it
							return nil
						}
						return fmt.Errorf("mounting binfmt_misc: %w", err)
					}
					mounted = true
				}
				reg, err := os.Create("/proc/sys/fs/binfmt_misc/register")
				if err != nil {
					return fmt.Errorf("registering(open): %w", err)
				}
				if _, err = fmt.Fprintf(reg, "%s\n", line); err != nil {
					return fmt.Errorf("registering(write): %w", err)
				}
				logrus.Tracef("registered binfmt %q", line)
				if err = reg.Close(); err != nil {
					return fmt.Errorf("registering(close): %w", err)
				}
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("reading binfmt.d configuration: %w", err)
			}
		}
	}
	return nil
}
