package chrootarchive

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"

	"github.com/moby/sys/capability"
	"go.podman.io/storage/pkg/mount"
	"golang.org/x/sys/unix"
)

// chroot on linux uses pivot_root instead of chroot
// pivot_root takes a new root and an old root.
// Old root must be a sub-dir of new root, it is where the current rootfs will reside after the call to pivot_root.
// New root is where the new rootfs is set to.
// Old root is removed after the call to pivot_root so it is no longer available under the new root.
// This is similar to how libcontainer sets up a container's rootfs
func chroot(path string) (err error) {
	caps, err := capability.NewPid2(0)
	if err != nil {
		return err
	}
	if err := caps.Load(); err != nil {
		return err
	}

	// initialize nss libraries in Glibc so that the dynamic libraries are loaded in the host
	// environment not in the chroot from untrusted files.
	_, _ = user.Lookup("storage")
	_, _ = net.LookupHost("localhost")

	// if the process doesn't have CAP_SYS_ADMIN, but does have CAP_SYS_CHROOT, we need to use the actual chroot
	if !caps.Get(capability.EFFECTIVE, capability.CAP_SYS_ADMIN) && caps.Get(capability.EFFECTIVE, capability.CAP_SYS_CHROOT) {
		return realChroot(path)
	}

	if err := unix.Unshare(unix.CLONE_NEWNS); err != nil {
		return fmt.Errorf("creating mount namespace before pivot: %w", err)
	}

	// make everything in new ns private
	if err := mount.MakeRPrivate("/"); err != nil {
		return err
	}

	if mounted, _ := mount.Mounted(path); !mounted {
		if err := mount.Mount(path, path, "bind", "rbind,rw"); err != nil {
			return realChroot(path)
		}
	}

	// setup oldRoot for pivot_root
	pivotDir, err := os.MkdirTemp(path, ".pivot_root")
	if err != nil {
		return fmt.Errorf("setting up pivot dir: %w", err)
	}

	var mounted bool
	defer func() {
		if mounted {
			// make sure pivotDir is not mounted before we try to remove it
			if errCleanup := unix.Unmount(pivotDir, unix.MNT_DETACH); errCleanup != nil {
				if err == nil {
					err = errCleanup
				}
				return
			}
		}

		errCleanup := os.Remove(pivotDir)
		// pivotDir doesn't exist if pivot_root failed and chroot+chdir was successful
		// because we already cleaned it up on failed pivot_root
		if errCleanup != nil && !os.IsNotExist(errCleanup) {
			errCleanup = fmt.Errorf("cleaning up after pivot: %w", errCleanup)
			if err == nil {
				err = errCleanup
			}
		}
	}()

	if err := unix.PivotRoot(path, pivotDir); err != nil {
		// If pivot fails, fall back to the normal chroot after cleaning up temp dir
		if err := os.Remove(pivotDir); err != nil {
			return fmt.Errorf("cleaning up after failed pivot: %w", err)
		}
		return realChroot(path)
	}
	mounted = true

	// This is the new path for where the old root (prior to the pivot) has been moved to
	// This dir contains the rootfs of the caller, which we need to remove so it is not visible during extraction
	pivotDir = filepath.Join("/", filepath.Base(pivotDir))

	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("changing to new root: %w", err)
	}

	// Make the pivotDir (where the old root lives) private so it can be unmounted without propagating to the host
	if err := unix.Mount("", pivotDir, "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("making old root private after pivot: %w", err)
	}

	// Now unmount the old root so it's no longer visible from the new root
	if err := unix.Unmount(pivotDir, unix.MNT_DETACH); err != nil {
		return fmt.Errorf("while unmounting old root after pivot: %w", err)
	}
	mounted = false

	return nil
}

func realChroot(path string) error {
	if err := unix.Chroot(path); err != nil {
		return fmt.Errorf("after fallback to chroot: %w", err)
	}
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("changing to new root after chroot: %w", err)
	}
	return nil
}
