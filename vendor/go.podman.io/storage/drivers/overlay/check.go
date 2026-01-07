//go:build linux

package overlay

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/idmap"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/system"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

// doesSupportNativeDiff checks whether the filesystem has a bug
// which copies up the opaque flag when copying up an opaque
// directory or the kernel enable CONFIG_OVERLAY_FS_REDIRECT_DIR.
// When these exist naive diff should be used.
func doesSupportNativeDiff(d, mountOpts string) error {
	td, err := os.MkdirTemp(d, "opaque-bug-check")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(td); err != nil {
			logrus.Warnf("Failed to remove check directory %v: %v", td, err)
		}
	}()

	// Make directories l1/d, l1/d1, l2/d, l3, work, merged
	if err := os.MkdirAll(filepath.Join(td, "l1", "d"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(td, "l1", "d1"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(td, "l2", "d"), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(td, "l3"), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(td, "work"), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(td, "merged"), 0o755); err != nil {
		return err
	}

	// Mark l2/d as opaque
	if err := system.Lsetxattr(filepath.Join(td, "l2", "d"), archive.GetOverlayXattrName("opaque"), []byte("y"), 0); err != nil {
		return fmt.Errorf("failed to set opaque flag on middle layer: %w", err)
	}

	mountFlags := "lowerdir=%s:%s,upperdir=%s,workdir=%s"
	if unshare.IsRootless() {
		mountFlags = mountFlags + ",userxattr"
	}

	opts := fmt.Sprintf(mountFlags, path.Join(td, "l2"), path.Join(td, "l1"), path.Join(td, "l3"), path.Join(td, "work"))
	flags, data := mount.ParseOptions(mountOpts)
	if data != "" {
		opts = fmt.Sprintf("%s,%s", opts, data)
	}
	if err := unix.Mount("overlay", filepath.Join(td, "merged"), "overlay", uintptr(flags), opts); err != nil {
		return fmt.Errorf("failed to mount overlay: %w", err)
	}
	defer func() {
		if err := unix.Unmount(filepath.Join(td, "merged"), 0); err != nil {
			logrus.Warnf("Failed to unmount check directory %v: %v", filepath.Join(td, "merged"), err)
		}
	}()

	// Touch file in d to force copy up of opaque directory "d" from "l2" to "l3"
	if err := os.WriteFile(filepath.Join(td, "merged", "d", "f"), []byte{}, 0o644); err != nil {
		return fmt.Errorf("failed to write to merged directory: %w", err)
	}

	// Check l3/d does not have opaque flag
	xattrOpaque, err := system.Lgetxattr(filepath.Join(td, "l3", "d"), archive.GetOverlayXattrName("opaque"))
	if err != nil {
		return fmt.Errorf("failed to read opaque flag on upper layer: %w", err)
	}
	if string(xattrOpaque) == "y" {
		return errors.New("opaque flag erroneously copied up, consider update to kernel 4.8 or later to fix")
	}

	// rename "d1" to "d2"
	if err := os.Rename(filepath.Join(td, "merged", "d1"), filepath.Join(td, "merged", "d2")); err != nil {
		// if rename failed with syscall.EXDEV, the kernel doesn't have CONFIG_OVERLAY_FS_REDIRECT_DIR enabled
		if err.(*os.LinkError).Err == syscall.EXDEV {
			return nil
		}
		return fmt.Errorf("failed to rename dir in merged directory: %w", err)
	}
	// get the xattr of "d2"
	xattrRedirect, err := system.Lgetxattr(filepath.Join(td, "l3", "d2"), archive.GetOverlayXattrName("redirect"))
	if err != nil {
		return fmt.Errorf("failed to read redirect flag on upper layer: %w", err)
	}

	if string(xattrRedirect) == "d1" {
		return errors.New("kernel has CONFIG_OVERLAY_FS_REDIRECT_DIR enabled")
	}

	return nil
}

// doesMetacopy checks if the filesystem is going to optimize changes to
// metadata by using nodes marked with an "overlay.metacopy" attribute to avoid
// copying up a file from a lower layer unless/until its contents are being
// modified
func doesMetacopy(d, mountOpts string) (bool, error) {
	td, err := os.MkdirTemp(d, "metacopy-check")
	if err != nil {
		return false, err
	}
	defer func() {
		if err := os.RemoveAll(td); err != nil {
			logrus.Warnf("Failed to remove check directory %v: %v", td, err)
		}
	}()

	// Make directories l1, l2, work, merged
	if err := os.MkdirAll(filepath.Join(td, "l1"), 0o755); err != nil {
		return false, err
	}
	if err := ioutils.AtomicWriteFile(filepath.Join(td, "l1", "f"), []byte{0xff}, 0o700); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Join(td, "l2"), 0o755); err != nil {
		return false, err
	}
	if err := os.Mkdir(filepath.Join(td, "work"), 0o755); err != nil {
		return false, err
	}
	if err := os.Mkdir(filepath.Join(td, "merged"), 0o755); err != nil {
		return false, err
	}
	// Mount using the mandatory options and configured options
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", path.Join(td, "l1"), path.Join(td, "l2"), path.Join(td, "work"))
	if unshare.IsRootless() {
		opts = fmt.Sprintf("%s,userxattr", opts)
	}
	flags, data := mount.ParseOptions(mountOpts)
	if data != "" {
		opts = fmt.Sprintf("%s,%s", opts, data)
	}
	if err := unix.Mount("overlay", filepath.Join(td, "merged"), "overlay", uintptr(flags), opts); err != nil {
		if errors.Is(err, unix.EINVAL) {
			logrus.Infof("overlay: metacopy option not supported on this kernel, checked using options %q", mountOpts)
			return false, nil
		}
		return false, fmt.Errorf("failed to mount overlay for metacopy check with %q options: %w", mountOpts, err)
	}
	defer func() {
		if err := unix.Unmount(filepath.Join(td, "merged"), 0); err != nil {
			logrus.Warnf("Failed to unmount check directory %v: %v", filepath.Join(td, "merged"), err)
		}
	}()
	// Make a change that only impacts the inode, and check if the pulled-up copy is marked
	// as a metadata-only copy
	if err := os.Chmod(filepath.Join(td, "merged", "f"), 0o600); err != nil {
		return false, fmt.Errorf("changing permissions on file for metacopy check: %w", err)
	}
	metacopy, err := system.Lgetxattr(filepath.Join(td, "l2", "f"), archive.GetOverlayXattrName("metacopy"))
	if err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			logrus.Info("metacopy option not supported")
			return false, nil
		}
		return false, fmt.Errorf("metacopy flag was not set on file in upper layer: %w", err)
	}
	return metacopy != nil, nil
}

// doesVolatile checks if the filesystem supports the "volatile" mount option
func doesVolatile(d string) (bool, error) {
	td, err := os.MkdirTemp(d, "volatile-check")
	if err != nil {
		return false, err
	}
	defer func() {
		if err := os.RemoveAll(td); err != nil {
			logrus.Warnf("Failed to remove check directory %v: %v", td, err)
		}
	}()

	if err := os.MkdirAll(filepath.Join(td, "lower"), 0o755); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Join(td, "upper"), 0o755); err != nil {
		return false, err
	}
	if err := os.Mkdir(filepath.Join(td, "work"), 0o755); err != nil {
		return false, err
	}
	if err := os.Mkdir(filepath.Join(td, "merged"), 0o755); err != nil {
		return false, err
	}
	// Mount using the mandatory options and configured options
	opts := fmt.Sprintf("volatile,lowerdir=%s,upperdir=%s,workdir=%s", path.Join(td, "lower"), path.Join(td, "upper"), path.Join(td, "work"))
	if unshare.IsRootless() {
		opts = fmt.Sprintf("%s,userxattr", opts)
	}
	if err := unix.Mount("overlay", filepath.Join(td, "merged"), "overlay", 0, opts); err != nil {
		return false, fmt.Errorf("failed to mount overlay for volatile check: %w", err)
	}
	defer func() {
		if err := unix.Unmount(filepath.Join(td, "merged"), 0); err != nil {
			logrus.Warnf("Failed to unmount check directory %v: %v", filepath.Join(td, "merged"), err)
		}
	}()
	return true, nil
}

// supportsIdmappedLowerLayers checks if the kernel supports mounting overlay on top of
// a idmapped lower layer.
func supportsIdmappedLowerLayers(home string) (bool, error) {
	layerDir, err := os.MkdirTemp(home, "compat")
	if err != nil {
		return false, err
	}
	defer func() {
		_ = os.RemoveAll(layerDir)
	}()

	mergedDir := filepath.Join(layerDir, "merged")
	lowerDir := filepath.Join(layerDir, "lower")
	lowerMappedDir := filepath.Join(layerDir, "lower-mapped")
	upperDir := filepath.Join(layerDir, "upper")
	workDir := filepath.Join(layerDir, "work")

	_ = idtools.MkdirAs(mergedDir, 0o700, 0, 0)
	_ = idtools.MkdirAs(lowerDir, 0o700, 0, 0)
	_ = idtools.MkdirAs(lowerMappedDir, 0o700, 0, 0)
	_ = idtools.MkdirAs(upperDir, 0o700, 0, 0)
	_ = idtools.MkdirAs(workDir, 0o700, 0, 0)

	mapping := []idtools.IDMap{
		{
			ContainerID: 0,
			HostID:      0,
			Size:        1,
		},
	}
	pid, cleanupFunc, err := idmap.CreateUsernsProcess(mapping, mapping)
	if err != nil {
		return false, err
	}
	defer cleanupFunc()

	if err := idmap.CreateIDMappedMount(lowerDir, lowerMappedDir, pid); err != nil {
		return false, fmt.Errorf("create mapped mount: %w", err)
	}
	defer func() {
		if err := unix.Unmount(lowerMappedDir, unix.MNT_DETACH); err != nil {
			logrus.Warnf("Unmount %q: %v", lowerMappedDir, err)
		}
	}()

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lowerMappedDir, upperDir, workDir)
	flags := uintptr(0)
	if err := unix.Mount("overlay", mergedDir, "overlay", flags, opts); err != nil {
		return false, err
	}
	defer func() {
		_ = unix.Unmount(mergedDir, unix.MNT_DETACH)
	}()
	return true, nil
}

// supportsDataOnlyLayers checks if the kernel supports mounting a overlay file system
// that uses data-only layers.
func supportsDataOnlyLayers(home string) (bool, error) {
	layerDir, err := os.MkdirTemp(home, "compat")
	if err != nil {
		return false, err
	}
	defer func() {
		_ = os.RemoveAll(layerDir)
	}()

	mergedDir := filepath.Join(layerDir, "merged")
	lowerDir := filepath.Join(layerDir, "lower")
	lowerDirDataOnly := filepath.Join(layerDir, "lower-data")
	upperDir := filepath.Join(layerDir, "upper")
	workDir := filepath.Join(layerDir, "work")

	_ = idtools.MkdirAs(mergedDir, 0o700, 0, 0)
	_ = idtools.MkdirAs(lowerDir, 0o700, 0, 0)
	_ = idtools.MkdirAs(lowerDirDataOnly, 0o700, 0, 0)
	_ = idtools.MkdirAs(upperDir, 0o700, 0, 0)
	_ = idtools.MkdirAs(workDir, 0o700, 0, 0)

	opts := fmt.Sprintf("lowerdir=%s::%s,upperdir=%s,workdir=%s,metacopy=on", lowerDir, lowerDirDataOnly, upperDir, workDir)
	flags := uintptr(0)
	if err := unix.Mount("overlay", mergedDir, "overlay", flags, opts); err != nil {
		return false, err
	}
	_ = unix.Unmount(mergedDir, unix.MNT_DETACH)

	return true, nil
}
