//go:build linux

package idmap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/idtools"
	"golang.org/x/sys/unix"
)

// CreateIDMappedMount creates a IDMapped bind mount from SOURCE to TARGET using the user namespace
// for the PID process.
func CreateIDMappedMount(source, target string, pid int) error {
	path := fmt.Sprintf("/proc/%d/ns/user", pid)
	userNsFile, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("unable to get user ns file descriptor for %q: %w", path, err)
	}
	defer userNsFile.Close()

	targetDirFd, err := unix.OpenTree(unix.AT_FDCWD, source, unix.OPEN_TREE_CLONE)
	if err != nil {
		return &os.PathError{Op: "open_tree", Path: source, Err: err}
	}
	defer unix.Close(targetDirFd)

	if err := unix.MountSetattr(targetDirFd, "", unix.AT_EMPTY_PATH|unix.AT_RECURSIVE,
		&unix.MountAttr{
			Attr_set:    unix.MOUNT_ATTR_IDMAP,
			Userns_fd:   uint64(userNsFile.Fd()),
			Propagation: unix.MS_PRIVATE,
		}); err != nil {
		return &os.PathError{Op: "mount_setattr", Path: source, Err: err}
	}
	if err := os.Mkdir(target, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}

	if err := unix.MoveMount(targetDirFd, "", 0, target, unix.MOVE_MOUNT_F_EMPTY_PATH); err != nil {
		return &os.PathError{Op: "move_mount", Path: target, Err: err}
	}
	return nil
}

// CreateUsernsProcess forks the current process and creates a user namespace using the specified
// mappings.  It returns the pid of the new process.
func CreateUsernsProcess(uidMaps []idtools.IDMap, gidMaps []idtools.IDMap) (int, func(), error) {
	var pid uintptr
	var err syscall.Errno

	if runtime.GOARCH == "s390x" {
		pid, _, err = syscall.Syscall6(uintptr(unix.SYS_CLONE), 0, unix.CLONE_NEWUSER|uintptr(unix.SIGCHLD), 0, 0, 0, 0)
	} else {
		pid, _, err = syscall.Syscall6(uintptr(unix.SYS_CLONE), unix.CLONE_NEWUSER|uintptr(unix.SIGCHLD), 0, 0, 0, 0, 0)
	}
	if err != 0 {
		return -1, nil, err
	}
	if pid == 0 {
		_ = unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0)
		// just wait for the SIGKILL
		for {
			_ = syscall.Pause()
		}
	}
	cleanupFunc := func() {
		err1 := unix.Kill(int(pid), unix.SIGKILL)
		if err1 != nil && err1 != syscall.ESRCH {
			logrus.Warnf("kill process pid: %d with SIGKILL ended with error: %v", int(pid), err1)
		}
		if err1 != nil {
			return
		}
		if _, err := unix.Wait4(int(pid), nil, 0, nil); err != nil {
			logrus.Warnf("wait4 pid: %d ended with error: %v", int(pid), err)
		}
	}
	writeMappings := func(fname string, idmap []idtools.IDMap) error {
		mappings := ""
		for _, m := range idmap {
			mappings = mappings + fmt.Sprintf("%d %d %d\n", m.ContainerID, m.HostID, m.Size)
		}
		return os.WriteFile(fmt.Sprintf("/proc/%d/%s", pid, fname), []byte(mappings), 0o600)
	}
	if err := writeMappings("uid_map", uidMaps); err != nil {
		cleanupFunc()
		return -1, nil, err
	}
	if err := writeMappings("gid_map", gidMaps); err != nil {
		cleanupFunc()
		return -1, nil, err
	}

	return int(pid), cleanupFunc, nil
}
