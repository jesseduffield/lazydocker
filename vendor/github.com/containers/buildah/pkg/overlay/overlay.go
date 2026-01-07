package overlay

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/system"
	"go.podman.io/storage/pkg/unshare"
)

// Options for MountWithOptions().
type Options struct {
	// The Upper directory is normally writable layer in an overlay mount.
	// Note!! : Following API does not handles escaping or validates correctness of the values
	// passed to UpperDirOptionFragment instead API will try to pass values as is it
	// to the `mount` command. It is user's responsibility to make sure they pre-validate
	// these values. Invalid inputs may lead to undefined behaviour.
	// This is provided as-is, use it if it works for you, we can/will change/break that in the future.
	// See discussion here for more context: https://github.com/containers/buildah/pull/3715#discussion_r786036959
	// TODO: Should we address above comment and handle escaping of metacharacters like
	// `comma`, `backslash` ,`colon` and any other special characters
	UpperDirOptionFragment string
	// The Workdir is used to prepare files as they are switched between the layers.
	// Note!! : Following API does not handles escaping or validates correctness of the values
	// passed to WorkDirOptionFragment instead API will try to pass values as is it
	// to the `mount` command. It is user's responsibility to make sure they pre-validate
	// these values. Invalid inputs may lead to undefined behaviour.
	// This is provided as-is, use it if it works for you, we can/will change/break that in the future.
	// See discussion here for more context: https://github.com/containers/buildah/pull/3715#discussion_r786036959
	// TODO: Should we address above comment and handle escaping of metacharacters like
	// `comma`, `backslash` ,`colon` and any other special characters
	WorkDirOptionFragment string
	// Graph options being used by the caller, will be searched when choosing mount program
	GraphOpts []string
	// Mark if following overlay is read only
	ReadOnly bool
	// Deprecated: RootUID is not used
	RootUID int
	// Deprecated: RootGID is not used
	RootGID int
	// Force overlay mounting and return a bind mount, rather than
	// attempting to optimize by having the runtime actually mount and
	// manage the overlay filesystem.
	ForceMount bool
	// MountLabel is a label to force for the overlay filesystem.
	MountLabel string
}

// TempDir generates a uniquely-named directory under ${containerDir}/overlay
// which can be used as a parent directory for the upper and working
// directories for an overlay mount, creates "upper" and "work" directories
// beneath it, and then returns the path of the new directory.
func TempDir(containerDir string, rootUID, rootGID int) (string, error) {
	contentDir := filepath.Join(containerDir, "overlay")
	if err := idtools.MkdirAllAs(contentDir, 0o700, rootUID, rootGID); err != nil {
		return "", fmt.Errorf("failed to create the overlay %s directory: %w", contentDir, err)
	}

	contentDir, err := os.MkdirTemp(contentDir, "")
	if err != nil {
		return "", fmt.Errorf("failed to create the overlay tmpdir in %s directory: %w", contentDir, err)
	}

	return contentDir, generateOverlayStructure(contentDir, rootUID, rootGID)
}

// GenerateStructure generates an overlay directory structure for container content
func GenerateStructure(containerDir, containerID, name string, rootUID, rootGID int) (string, error) {
	contentDir := filepath.Join(containerDir, "overlay-containers", containerID, name)
	if err := idtools.MkdirAllAs(contentDir, 0o700, rootUID, rootGID); err != nil {
		return "", fmt.Errorf("failed to create the overlay %s directory: %w", contentDir, err)
	}

	return contentDir, generateOverlayStructure(contentDir, rootUID, rootGID)
}

// generateOverlayStructure generates upper, work and merge directories under the specified directory
func generateOverlayStructure(containerDir string, rootUID, rootGID int) error {
	upperDir := filepath.Join(containerDir, "upper")
	workDir := filepath.Join(containerDir, "work")
	if err := idtools.MkdirAllAs(upperDir, 0o700, rootUID, rootGID); err != nil {
		return fmt.Errorf("creating overlay upper directory %s: %w", upperDir, err)
	}
	if err := idtools.MkdirAllAs(workDir, 0o700, rootUID, rootGID); err != nil {
		return fmt.Errorf("creating overlay work directory %s: %w", workDir, err)
	}
	mergeDir := filepath.Join(containerDir, "merge")
	if err := idtools.MkdirAllAs(mergeDir, 0o700, rootUID, rootGID); err != nil {
		return fmt.Errorf("creating overlay merge directory %s: %w", mergeDir, err)
	}
	return nil
}

// Mount creates a subdir of the contentDir based on the source directory
// from the source system.  It then mounts up the source directory on to the
// generated mount point and returns the mount point to the caller.
func Mount(contentDir, source, dest string, rootUID, rootGID int, graphOptions []string) (mount specs.Mount, Err error) {
	overlayOpts := Options{GraphOpts: graphOptions, ReadOnly: false, RootUID: rootUID, RootGID: rootGID}
	return MountWithOptions(contentDir, source, dest, &overlayOpts)
}

// MountReadOnly creates a subdir of the contentDir based on the source directory
// from the source system.  It then mounts up the source directory on to the
// generated mount point and returns the mount point to the caller.  Note that no
// upper layer will be created rendering it a read-only mount
func MountReadOnly(contentDir, source, dest string, rootUID, rootGID int, graphOptions []string) (mount specs.Mount, Err error) {
	overlayOpts := Options{GraphOpts: graphOptions, ReadOnly: true, RootUID: rootUID, RootGID: rootGID}
	return MountWithOptions(contentDir, source, dest, &overlayOpts)
}

// findMountProgram finds if any mount program is specified in the graph options.
func findMountProgram(graphOptions []string) string {
	mountMap := map[string]struct{}{
		".mount_program":         {},
		"overlay.mount_program":  {},
		"overlay2.mount_program": {},
	}

	for _, i := range graphOptions {
		s := strings.SplitN(i, "=", 2)
		if len(s) != 2 {
			continue
		}
		key := s[0]
		val := s[1]
		if _, has := mountMap[key]; has {
			return val
		}
	}

	return ""
}

// mountWithMountProgram mounts an overlay at mergeDir using the specified
// mount program and overlay options.
func mountWithMountProgram(mountProgram, overlayOptions, mergeDir string) error {
	cmd := exec.Command(mountProgram, "-o", overlayOptions, mergeDir)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exec %s: %w", mountProgram, err)
	}
	return nil
}

// mountNatively mounts an overlay at mergeDir using the kernel's mount()
// system call.
func mountNatively(overlayOptions, mergeDir string) error {
	return mount.Mount("overlay", mergeDir, "overlay", overlayOptions)
}

// Convert ":" to "\:", the path which will be overlay mounted need to be escaped
func escapeColon(source string) string {
	return strings.ReplaceAll(source, ":", "\\:")
}

// RemoveTemp unmounts a filesystem mounted at ${contentDir}/merge, and then
// removes ${contentDir}, which is typically a path returned by TempDir(),
// along with any contents it might still have.
func RemoveTemp(contentDir string) error {
	if err := Unmount(contentDir); err != nil {
		return err
	}

	return os.RemoveAll(contentDir)
}

// Unmount the overlay mountpoint at ${contentDir}/merge, where ${contentDir}
// is typically a path returned by TempDir().  The mountpoint itself is left
// unmodified.
func Unmount(contentDir string) error {
	mergeDir := filepath.Join(contentDir, "merge")

	if unshare.IsRootless() {
		// Attempt to unmount the FUSE mount using either fusermount or fusermount3.
		// If they fail, fallback to unix.Unmount
		for _, v := range []string{"fusermount3", "fusermount"} {
			err := exec.Command(v, "-u", mergeDir).Run()
			if err != nil && !errors.Is(err, exec.ErrNotFound) {
				logrus.Debugf("Error unmounting %s with %s - %v", mergeDir, v, err)
			}
			if err == nil {
				return nil
			}
		}
		// If fusermount|fusermount3 failed to unmount the FUSE file system, attempt unmount
	}

	// Ignore EINVAL as the specified merge dir is not a mount point
	if err := system.Unmount(mergeDir); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf("unmount overlay %s: %w", mergeDir, err)
	}
	return nil
}

// recreate removes a directory tree and then recreates the top of that tree
// with the same mode and ownership.
func recreate(contentDir string) error {
	st, err := system.Stat(contentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to stat overlay upper directory: %w", err)
	}

	if err := os.RemoveAll(contentDir); err != nil {
		return err
	}

	if err := idtools.MkdirAllAs(contentDir, os.FileMode(st.Mode()), int(st.UID()), int(st.GID())); err != nil {
		return fmt.Errorf("failed to create overlay directory: %w", err)
	}
	return nil
}

// CleanupMount removes all temporary mountpoint content
func CleanupMount(contentDir string) (Err error) {
	if err := recreate(filepath.Join(contentDir, "upper")); err != nil {
		return err
	}
	if err := recreate(filepath.Join(contentDir, "work")); err != nil {
		return err
	}
	return nil
}

// CleanupContent removes every temporary mountpoint created under
// ${containerDir}/overlay as a result of however many calls to TempDir(),
// roughly equivalent to calling RemoveTemp() for each of the directories whose
// paths it returned, and then removes ${containerDir} itself.
func CleanupContent(containerDir string) (Err error) {
	contentDir := filepath.Join(containerDir, "overlay")

	files, err := os.ReadDir(contentDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read directory: %w", err)
	}
	for _, f := range files {
		dir := filepath.Join(contentDir, f.Name())
		if err := Unmount(dir); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(contentDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to cleanup overlay directory: %w", err)
	}
	return nil
}
