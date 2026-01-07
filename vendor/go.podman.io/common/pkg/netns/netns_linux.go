// Copyright 2018 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This file was originally a part of the containernetworking/plugins
// repository.
// It was copied here and modified for local use by the libpod maintainers.

// Package netns contains functions to manage network namespaces on linux.
package netns

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

// threadNsPath is the /proc path to the current netns handle for the current thread.
const threadNsPath = "/proc/thread-self/ns/net"

var errNoFreeName = errors.New("failed to find free netns path name")

// GetNSRunDir returns the dir of where to create the netNS. When running
// rootless, it needs to be at a location writable by user.
func GetNSRunDir() (string, error) {
	if unshare.IsRootless() {
		rootlessDir, err := homedir.GetRuntimeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(rootlessDir, "netns"), nil
	}
	return "/run/netns", nil
}

func NewNSAtPath(nsPath string) (ns.NetNS, error) {
	return newNSPath(nsPath)
}

// NewNS creates a new persistent (bind-mounted) network namespace and returns
// an object representing that namespace, without switching to it.
func NewNS() (ns.NetNS, error) {
	nsRunDir, err := GetNSRunDir()
	if err != nil {
		return nil, err
	}

	// Create the directory for mounting network namespaces
	// This needs to be a shared mountpoint in case it is mounted in to
	// other namespaces (containers)
	err = makeNetnsDir(nsRunDir)
	if err != nil {
		return nil, err
	}

	for range 10000 {
		nsName, err := getRandomNetnsName()
		if err != nil {
			return nil, err
		}
		nsPath := path.Join(nsRunDir, nsName)
		ns, err := newNSPath(nsPath)
		if err == nil {
			return ns, nil
		}
		// retry when the name already exists
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return nil, err
	}
	return nil, errNoFreeName
}

// NewNSFrom creates a persistent (bind-mounted) network namespace from the
// given netns path, i.e. /proc/<pid>/ns/net, and returns the new full path to
// the bind mounted file in the netns run dir.
func NewNSFrom(fromNetns string) (string, error) {
	nsRunDir, err := GetNSRunDir()
	if err != nil {
		return "", err
	}

	err = makeNetnsDir(nsRunDir)
	if err != nil {
		return "", err
	}

	for range 10000 {
		nsName, err := getRandomNetnsName()
		if err != nil {
			return "", err
		}
		nsPath := filepath.Join(nsRunDir, nsName)

		// create an empty file to use as at the mount point
		err = createNetnsFile(nsPath)
		if err != nil {
			// retry when the name already exists
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}

		err = unix.Mount(fromNetns, nsPath, "none", unix.MS_BIND|unix.MS_SHARED|unix.MS_REC, "")
		if err != nil {
			// Do not leak the ns on errors
			_ = os.RemoveAll(nsPath)
			return "", fmt.Errorf("failed to bind mount ns at %s: %v", nsPath, err)
		}
		return nsPath, nil
	}

	return "", errNoFreeName
}

func getRandomNetnsName() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Reader.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate random netns name: %v", err)
	}
	return fmt.Sprintf("netns-%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func makeNetnsDir(nsRunDir string) error {
	err := os.MkdirAll(nsRunDir, 0o755)
	if err != nil {
		return err
	}
	// Important, the bind mount setup is racy if two process try to set it up in parallel.
	// This can have very bad consequences because we end up with two duplicated mounts
	// for the netns file that then might have a different parent mounts.
	// Also because as root netns dir is also created by ip netns we should not race against them.
	// Use a lock on the netns dir like they do, compare the iproute2 ip netns add code.
	// https://github.com/iproute2/iproute2/blob/8b9d9ea42759c91d950356ca43930a975d0c352b/ip/ipnetns.c#L806-L815

	dirFD, err := unix.Open(nsRunDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: nsRunDir, Err: err}
	}
	// closing the fd will also unlock so we do not have to call flock(fd,LOCK_UN)
	defer unix.Close(dirFD)

	err = unix.Flock(dirFD, unix.LOCK_EX)
	if err != nil {
		return fmt.Errorf("failed to lock %s dir: %w", nsRunDir, err)
	}

	// Remount the namespace directory shared. This will fail with EINVAL
	// if it is not already a mountpoint, so bind-mount it on to itself
	// to "upgrade" it to a mountpoint.
	err = unix.Mount("", nsRunDir, "none", unix.MS_SHARED|unix.MS_REC, "")
	if err == nil {
		return nil
	}
	if err != unix.EINVAL {
		return fmt.Errorf("mount --make-rshared %s failed: %q", nsRunDir, err)
	}

	// Recursively remount /run/netns on itself. The recursive flag is
	// so that any existing netns bindmounts are carried over.
	err = unix.Mount(nsRunDir, nsRunDir, "none", unix.MS_BIND|unix.MS_REC, "")
	if err != nil {
		return fmt.Errorf("mount --rbind %s %s failed: %q", nsRunDir, nsRunDir, err)
	}

	// Now we can make it shared
	err = unix.Mount("", nsRunDir, "none", unix.MS_SHARED|unix.MS_REC, "")
	if err != nil {
		return fmt.Errorf("mount --make-rshared %s failed: %q", nsRunDir, err)
	}

	return nil
}

// createNetnsFile created the file with O_EXCL to ensure there are no conflicts with others
// Callers should check for ErrExist and loop over it to find a free file.
func createNetnsFile(path string) error {
	mountPointFd, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	return mountPointFd.Close()
}

func newNSPath(nsPath string) (ns.NetNS, error) {
	// create an empty file to use as at the mount point
	err := createNetnsFile(nsPath)
	if err != nil {
		return nil, err
	}
	// Ensure the mount point is cleaned up on errors; if the namespace
	// was successfully mounted this will have no effect because the file
	// is in-use
	defer func() {
		_ = os.RemoveAll(nsPath)
	}()

	var wg sync.WaitGroup
	wg.Add(1)

	// do namespace work in a dedicated goroutine, so that we can safely
	// Lock/Unlock OSThread without upsetting the lock/unlock state of
	// the caller of this function
	go (func() {
		defer wg.Done()
		runtime.LockOSThread()
		// Don't unlock. By not unlocking, golang will kill the OS thread when the
		// goroutine is done (for go1.10+)

		// create a new netns on the current thread
		err = unix.Unshare(unix.CLONE_NEWNET)
		if err != nil {
			err = fmt.Errorf("unshare network namespace: %w", err)
			return
		}

		// bind mount the netns from the current thread (from /proc) onto the
		// mount point. This causes the namespace to persist, even when there
		// are no threads in the ns. Make this a shared mount; it needs to be
		// back-propagated to the host
		err = unix.Mount(threadNsPath, nsPath, "none", unix.MS_BIND|unix.MS_SHARED|unix.MS_REC, "")
		if err != nil {
			err = fmt.Errorf("failed to bind mount ns at %s: %v", nsPath, err)
		}
	})()
	wg.Wait()

	if err != nil {
		return nil, fmt.Errorf("failed to create namespace: %v", err)
	}

	return ns.GetNS(nsPath)
}

// UnmountNS unmounts the given netns path.
func UnmountNS(nsPath string) error {
	// Only unmount if it's been bind-mounted (don't touch namespaces in /proc...)
	if strings.HasPrefix(nsPath, "/proc/") {
		return nil
	}
	// EINVAL means the path exists but is not mounted, just try to remove the path below
	if err := unix.Unmount(nsPath, unix.MNT_DETACH); err != nil && !errors.Is(err, unix.EINVAL) {
		// If path does not exists we can return without error as we have nothing to do.
		if errors.Is(err, unix.ENOENT) {
			return nil
		}

		return fmt.Errorf("failed to unmount NS: at %s: %w", nsPath, err)
	}

	var err error
	// wait for up to 60s in the loop
	for range 6000 {
		if err = os.Remove(nsPath); err != nil {
			if errors.Is(err, unix.EBUSY) {
				// mount is still busy, sleep a moment and try again to remove
				logrus.Debugf("Netns %s still busy, try removing it again in 10ms", nsPath)
				time.Sleep(10 * time.Millisecond)
				continue
			}
			// If path does not exists we can return without error.
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return fmt.Errorf("failed to remove ns path: %w", err)
		}
		return nil
	}

	return fmt.Errorf("failed to remove ns path (timeout after 60s): %w", err)
}
