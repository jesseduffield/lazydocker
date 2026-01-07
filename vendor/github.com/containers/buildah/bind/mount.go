//go:build linux

package bind

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/containers/buildah/util"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/mount"
	"golang.org/x/sys/unix"
)

// SetupIntermediateMountNamespace creates a new mount namespace and bind
// mounts all bind-mount sources into a subdirectory of bundlePath that can
// only be reached by the root user of the container's user namespace, except
// for Mounts which include the NoBindOption option in their options list.  The
// NoBindOption will then merely be removed.
func SetupIntermediateMountNamespace(spec *specs.Spec, bundlePath string) (unmountAll func() error, err error) {
	defer stripNoBindOption(spec)

	// We expect a root directory to be defined.
	if spec.Root == nil {
		return nil, errors.New("configuration has no root filesystem?")
	}
	rootPath := spec.Root.Path

	// Create a new mount namespace in which to do the things we're doing.
	if err := unix.Unshare(unix.CLONE_NEWNS); err != nil {
		return nil, fmt.Errorf("creating new mount namespace for %v: %w", spec.Process.Args, err)
	}

	// Make all of our mounts private to our namespace.
	if err := mount.MakeRPrivate("/"); err != nil {
		return nil, fmt.Errorf("making mounts private to mount namespace for %v: %w", spec.Process.Args, err)
	}

	// Make sure the bundle directory is searchable.  We created it with
	// TempDir(), so it should have started with permissions set to 0700.
	info, err := os.Stat(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("checking permissions on %q: %w", bundlePath, err)
	}
	if err = os.Chmod(bundlePath, info.Mode()|0o111); err != nil {
		return nil, fmt.Errorf("loosening permissions on %q: %w", bundlePath, err)
	}

	// Figure out who needs to be able to reach these bind mounts in order
	// for the container to be started.
	rootUID, rootGID, err := util.GetHostRootIDs(spec)
	if err != nil {
		return nil, err
	}

	// Hand back a callback that the caller can use to clean up everything
	// we're doing here.
	unmount := []string{}
	unmountAll = func() (err error) {
		for _, mountpoint := range unmount {
			// Unmount it and anything under it.
			if err2 := UnmountMountpoints(mountpoint, nil); err2 != nil {
				logrus.Warnf("pkg/bind: error unmounting %q: %v", mountpoint, err2)
				if err == nil {
					err = err2
				}
			}
			if err2 := unix.Unmount(mountpoint, unix.MNT_DETACH); err2 != nil {
				if errno, ok := err2.(syscall.Errno); !ok || errno != syscall.EINVAL {
					logrus.Warnf("pkg/bind: error detaching %q: %v", mountpoint, err2)
					if err == nil {
						err = err2
					}
				}
			}
			// Remove just the mountpoint.
			retry := 10
			remove := unix.Unlink
			err2 := remove(mountpoint)
			for err2 != nil && retry > 0 {
				if errno, ok := err2.(syscall.Errno); ok {
					switch errno {
					default:
						retry = 0
						continue
					case syscall.EISDIR:
						remove = unix.Rmdir
						err2 = remove(mountpoint)
					case syscall.EBUSY:
						if err3 := unix.Unmount(mountpoint, unix.MNT_DETACH); err3 == nil {
							err2 = remove(mountpoint)
						}
					}
					retry--
				}
			}
			if err2 != nil {
				logrus.Warnf("pkg/bind: error removing %q: %v", mountpoint, err2)
				if err == nil {
					err = err2
				}
			}
		}
		return err
	}

	// Create a top-level directory that the "root" user will be able to
	// access, that "root" from containers which use different mappings, or
	// other unprivileged users outside of containers, shouldn't be able to
	// access.
	mnt := filepath.Join(bundlePath, "mnt")
	if err = idtools.MkdirAndChown(mnt, 0o100, idtools.IDPair{UID: int(rootUID), GID: int(rootGID)}); err != nil {
		return unmountAll, fmt.Errorf("creating %q owned by the container's root user: %w", mnt, err)
	}

	// Make that directory private, and add it to the list of locations we
	// unmount at cleanup time.
	if err = mount.MakeRPrivate(mnt); err != nil {
		return unmountAll, fmt.Errorf("marking filesystem at %q as private: %w", mnt, err)
	}
	unmount = append([]string{mnt}, unmount...)

	// Create a bind mount for the root filesystem and add it to the list.
	rootfs := filepath.Join(mnt, "rootfs")
	if err = os.Mkdir(rootfs, 0o000); err != nil {
		return unmountAll, fmt.Errorf("creating directory %q: %w", rootfs, err)
	}
	if err = unix.Mount(rootPath, rootfs, "", unix.MS_BIND|unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return unmountAll, fmt.Errorf("bind mounting root filesystem from %q to %q: %w", rootPath, rootfs, err)
	}
	logrus.Debugf("bind mounted %q to %q", rootPath, rootfs)
	unmount = append([]string{rootfs}, unmount...)
	spec.Root.Path = rootfs

	// Do the same for everything we're binding in.
	mounts := make([]specs.Mount, 0, len(spec.Mounts))
	for i := range spec.Mounts {
		// If we're not using an intermediate, leave it in the list.
		if leaveBindMountAlone(spec.Mounts[i]) {
			mounts = append(mounts, spec.Mounts[i])
			continue
		}
		// Check if the source is a directory or something else.
		info, err := os.Stat(spec.Mounts[i].Source)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logrus.Warnf("couldn't find %q on host to bind mount into container", spec.Mounts[i].Source)
				continue
			}
			return unmountAll, fmt.Errorf("checking if %q is a directory: %w", spec.Mounts[i].Source, err)
		}
		stage := filepath.Join(mnt, fmt.Sprintf("buildah-bind-target-%d", i))
		if info.IsDir() {
			// If the source is a directory, make one to use as the
			// mount target.
			if err = os.Mkdir(stage, 0o000); err != nil {
				return unmountAll, fmt.Errorf("creating directory %q: %w", stage, err)
			}
		} else {
			// If the source is not a directory, create an empty
			// file to use as the mount target.
			file, err := os.OpenFile(stage, os.O_WRONLY|os.O_CREATE, 0o000)
			if err != nil {
				return unmountAll, fmt.Errorf("creating file %q: %w", stage, err)
			}
			file.Close()
		}
		// Bind mount the source from wherever it is to a place where
		// we know the runtime helper will be able to get to it...
		if err = unix.Mount(spec.Mounts[i].Source, stage, "", unix.MS_BIND|unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
			return unmountAll, fmt.Errorf("bind mounting bind object from %q to %q: %w", spec.Mounts[i].Source, stage, err)
		}
		logrus.Debugf("bind mounted %q to %q", spec.Mounts[i].Source, stage)
		spec.Mounts[i].Source = stage
		// ... and update the source location that we'll pass to the
		// runtime to our intermediate location.
		mounts = append(mounts, spec.Mounts[i])
		unmount = append([]string{stage}, unmount...)
	}
	spec.Mounts = mounts

	return unmountAll, nil
}

// Decide if the mount should not be redirected to an intermediate location first.
func leaveBindMountAlone(mount specs.Mount) bool {
	// If we know we shouldn't do a redirection for this mount, skip it.
	if slices.Contains(mount.Options, NoBindOption) {
		return true
	}
	// If we're not bind mounting it in, we don't need to do anything for it.
	if mount.Type != "bind" && !slices.Contains(mount.Options, "bind") && !slices.Contains(mount.Options, "rbind") {
		return true
	}
	return false
}

// UnmountMountpoints unmounts the given mountpoints and anything that's hanging
// off of them, rather aggressively.  If a mountpoint also appears in the
// mountpointsToRemove slice, the mountpoints are removed after they are
// unmounted.
func UnmountMountpoints(mountpoint string, mountpointsToRemove []string) error {
	mounts, err := mount.GetMounts()
	if err != nil {
		return fmt.Errorf("retrieving list of mounts: %w", err)
	}
	// getChildren returns the list of mount IDs that hang off of the
	// specified ID.
	getChildren := func(id int) []int {
		var list []int
		for _, info := range mounts {
			if info.Parent == id {
				list = append(list, info.ID)
			}
		}
		return list
	}
	// getTree returns the list of mount IDs that hang off of the specified
	// ID, and off of those mount IDs, etc.
	getTree := func(id int) []int {
		mounts := []int{id}
		i := 0
		for i < len(mounts) {
			children := getChildren(mounts[i])
			mounts = append(mounts, children...)
			i++
		}
		return mounts
	}
	// getMountByID looks up the mount info with the specified ID
	getMountByID := func(id int) *mount.Info {
		for i := range mounts {
			if mounts[i].ID == id {
				return mounts[i]
			}
		}
		return nil
	}
	// getMountByPoint looks up the mount info with the specified mountpoint
	getMountByPoint := func(mountpoint string) *mount.Info {
		for i := range mounts {
			if mounts[i].Mountpoint == mountpoint {
				return mounts[i]
			}
		}
		return nil
	}
	// find the top of the tree we're unmounting
	top := getMountByPoint(mountpoint)
	if top == nil {
		if err != nil {
			return fmt.Errorf("%q is not mounted: %w", mountpoint, err)
		}
		return nil
	}
	// add all of the mounts that are hanging off of it
	tree := getTree(top.ID)
	// unmount each mountpoint, working from the end of the list (leaf nodes) to the top
	for i := range tree {
		var st unix.Stat_t
		id := tree[len(tree)-i-1]
		mount := getMountByID(id)
		// check if this mountpoint is mounted
		if err := unix.Lstat(mount.Mountpoint, &st); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				logrus.Debugf("mountpoint %q is not present(?), skipping", mount.Mountpoint)
				continue
			}
			return fmt.Errorf("checking if %q is mounted: %w", mount.Mountpoint, err)
		}
		if uint64(mount.Major) != uint64(st.Dev) || uint64(mount.Minor) != uint64(st.Dev) { //nolint:unconvert // (required for some OS/arch combinations)
			logrus.Debugf("%q is apparently not really mounted, skipping", mount.Mountpoint)
			continue
		}
		// do the unmount
		if err := unix.Unmount(mount.Mountpoint, 0); err != nil {
			// if it was busy, detach it
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.EBUSY {
				err = unix.Unmount(mount.Mountpoint, unix.MNT_DETACH)
			}
			if err != nil {
				// if it was invalid (not mounted), hide the error, else return it
				if errno, ok := err.(syscall.Errno); !ok || errno != syscall.EINVAL {
					logrus.Warnf("error unmounting %q: %v", mount.Mountpoint, err)
					continue
				}
			}
		}
		// if we're also supposed to remove this thing, do that, too
		if slices.Contains(mountpointsToRemove, mount.Mountpoint) {
			if err := os.Remove(mount.Mountpoint); err != nil {
				return fmt.Errorf("removing %q: %w", mount.Mountpoint, err)
			}
		}
	}
	return nil
}
