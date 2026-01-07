//go:build !windows

package homedir

// Copyright 2013-2018 Docker, Inc.
// NOTE: this package has originally been copied from github.com/docker/docker.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/unshare"
)

// Key returns the env var name for the user's home dir based on
// the platform being run on
func Key() string {
	return "HOME"
}

// Get returns the home directory of the current user with the help of
// environment variables depending on the target operating system.
// Returned path should be used with "path/filepath" to form new paths.
//
// If linking statically with cgo enabled against glibc, ensure the
// osusergo build tag is used.
//
// If needing to do nss lookups, do not disable cgo or set osusergo.
func Get() string {
	homedir, _ := unshare.HomeDir()
	return homedir
}

// GetShortcutString returns the string that is shortcut to user's home directory
// in the native shell of the platform running on.
func GetShortcutString() string {
	return "~"
}

// StickRuntimeDirContents sets the sticky bit on files that are under
// XDG_RUNTIME_DIR, so that the files won't be periodically removed by the system.
//
// StickyRuntimeDir returns slice of sticked files.
// StickyRuntimeDir returns nil error if XDG_RUNTIME_DIR is not set.
//
// See also https://standards.freedesktop.org/basedir-spec/latest/ar01s03.html
func StickRuntimeDirContents(files []string) ([]string, error) {
	runtimeDir, err := GetRuntimeDir()
	if err != nil {
		// ignore error if runtimeDir is empty
		return nil, nil //nolint: nilerr
	}
	runtimeDir, err = filepath.Abs(runtimeDir)
	if err != nil {
		return nil, err
	}
	var sticked []string
	for _, f := range files {
		f, err = filepath.Abs(f)
		if err != nil {
			return sticked, err
		}
		if strings.HasPrefix(f, runtimeDir+"/") {
			if err = stick(f); err != nil {
				return sticked, err
			}
			sticked = append(sticked, f)
		}
	}
	return sticked, nil
}

func stick(f string) error {
	st, err := os.Stat(f)
	if err != nil {
		return err
	}
	m := st.Mode()
	m |= os.ModeSticky
	return os.Chmod(f, m)
}

var (
	rootlessConfigHomeDirError error
	rootlessConfigHomeDirOnce  sync.Once
	rootlessConfigHomeDir      string
	rootlessRuntimeDirOnce     sync.Once
	rootlessRuntimeDir         string
)

// isWriteableOnlyByOwner checks that the specified permission mask allows write
// access only to the owner.
func isWriteableOnlyByOwner(perm os.FileMode) bool {
	return (perm & 0o722) == 0o700
}

// GetConfigHome returns XDG_CONFIG_HOME.
// GetConfigHome returns $HOME/.config and nil error if XDG_CONFIG_HOME is not set.
//
// See also https://standards.freedesktop.org/basedir-spec/latest/ar01s03.html
func GetConfigHome() (string, error) {
	rootlessConfigHomeDirOnce.Do(func() {
		cfgHomeDir := os.Getenv("XDG_CONFIG_HOME")
		if cfgHomeDir == "" {
			home := Get()
			resolvedHome, err := filepath.EvalSymlinks(home)
			if err != nil {
				rootlessConfigHomeDirError = fmt.Errorf("cannot resolve %s: %w", home, err)
				return
			}
			tmpDir := filepath.Join(resolvedHome, ".config")
			_ = os.MkdirAll(tmpDir, 0o700)
			st, err := os.Stat(tmpDir)
			if err != nil {
				rootlessConfigHomeDirError = err
				return
			} else if int(st.Sys().(*syscall.Stat_t).Uid) == os.Geteuid() {
				cfgHomeDir = tmpDir
			} else {
				rootlessConfigHomeDirError = fmt.Errorf("path %q exists and it is not owned by the current user", tmpDir)
				return
			}
		}
		rootlessConfigHomeDir = cfgHomeDir
	})

	return rootlessConfigHomeDir, rootlessConfigHomeDirError
}

// GetRuntimeDir returns a directory suitable to store runtime files.
// The function will try to use the XDG_RUNTIME_DIR env variable if it is set.
// XDG_RUNTIME_DIR is typically configured via pam_systemd.
// If XDG_RUNTIME_DIR is not set, GetRuntimeDir will try to find a suitable
// directory for the current user.
//
// See also https://standards.freedesktop.org/basedir-spec/latest/ar01s03.html
func GetRuntimeDir() (string, error) {
	var rootlessRuntimeDirError error

	rootlessRuntimeDirOnce.Do(func() {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")

		if runtimeDir != "" {
			rootlessRuntimeDir, rootlessRuntimeDirError = filepath.EvalSymlinks(runtimeDir)
			return
		}

		uid := strconv.Itoa(unshare.GetRootlessUID())
		if runtimeDir == "" {
			tmpDir := filepath.Join("/run", "user", uid)
			if err := os.MkdirAll(tmpDir, 0o700); err != nil {
				logrus.Debug(err)
			}
			st, err := os.Lstat(tmpDir)
			if err == nil && int(st.Sys().(*syscall.Stat_t).Uid) == os.Geteuid() && isWriteableOnlyByOwner(st.Mode().Perm()) {
				runtimeDir = tmpDir
			}
		}
		if runtimeDir == "" {
			tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("storage-run-%s", uid))
			if err := os.MkdirAll(tmpDir, 0o700); err != nil {
				logrus.Debug(err)
			}
			st, err := os.Lstat(tmpDir)
			if err == nil && int(st.Sys().(*syscall.Stat_t).Uid) == os.Geteuid() && isWriteableOnlyByOwner(st.Mode().Perm()) {
				runtimeDir = tmpDir
			} else {
				rootlessRuntimeDirError = fmt.Errorf("path %q exists and it is not writeable only by the current user", tmpDir)
				return
			}
		}
		rootlessRuntimeDir = runtimeDir
	})

	return rootlessRuntimeDir, rootlessRuntimeDirError
}
