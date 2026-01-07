//go:build linux

package cgroups

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/opencontainers/cgroups"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
	"golang.org/x/sys/unix"
)

// WriteFile writes to a cgroup file.
func WriteFile(dir, file, data string) error {
	fd, err := OpenFile(dir, file, unix.O_WRONLY)
	if err != nil {
		return err
	}
	defer fd.Close()
	for {
		_, err := fd.WriteString(data)
		if errors.Is(err, unix.EINTR) {
			logrus.Infof("interrupted while writing %s to %s", data, fd.Name())
			continue
		}
		return err
	}
}

// OpenFile opens a cgroup file with the given flags.
func OpenFile(dir, file string, flags int) (*os.File, error) {
	var resolveFlags uint64
	mode := os.FileMode(0)
	if TestMode && flags&os.O_WRONLY != 0 {
		flags |= os.O_TRUNC | os.O_CREATE
		mode = 0o600
	}
	cgroupPath := path.Join(dir, file)
	relPath := strings.TrimPrefix(cgroupPath, cgroupRoot+"/")

	var stats unix.Statfs_t
	fdTest, errOpen := unix.Openat2(-1, cgroupRoot, &unix.OpenHow{
		Flags: unix.O_DIRECTORY | unix.O_PATH,
	})
	errStat := unix.Fstatfs(fdTest, &stats)
	cgroupFd := fdTest

	resolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS
	if stats.Type == unix.CGROUP2_SUPER_MAGIC {
		// cgroupv2 has a single mountpoint and no "cpu,cpuacct" symlinks
		resolveFlags |= unix.RESOLVE_NO_XDEV | unix.RESOLVE_NO_SYMLINKS
	}

	if errOpen != nil || errStat != nil || (len(relPath) == len(cgroupPath)) { // openat2 not available, use os
		fdTest, err := os.OpenFile(cgroupPath, flags, mode)
		if err != nil {
			return nil, err
		}
		if TestMode {
			return fdTest, nil
		}
		if err := unix.Fstatfs(int(fdTest.Fd()), &stats); err != nil {
			_ = fdTest.Close()
			return nil, &os.PathError{Op: "statfs", Path: cgroupPath, Err: err}
		}
		if stats.Type != unix.CGROUP_SUPER_MAGIC && stats.Type != unix.CGROUP2_SUPER_MAGIC {
			_ = fdTest.Close()
			return nil, &os.PathError{Op: "open", Path: cgroupPath, Err: errors.New("not a cgroup file")}
		}
		return fdTest, nil
	}

	fd, err := unix.Openat2(cgroupFd, relPath,
		&unix.OpenHow{
			Resolve: resolveFlags,
			Flags:   uint64(flags) | unix.O_CLOEXEC,
			Mode:    uint64(mode),
		})
	if err != nil {
		return nil, err
	}

	return os.NewFile(uintptr(fd), cgroupPath), nil
}

// ReadFile reads from a cgroup file, opening it with the read only flag.
func ReadFile(dir, file string) (string, error) {
	fd, err := OpenFile(dir, file, unix.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer fd.Close()
	var buf bytes.Buffer

	_, err = buf.ReadFrom(fd)
	return buf.String(), err
}

// BlkioFiles gets the proper files for blkio weights.
func BlkioFiles(cgroupPath string) (wtFile, wtDevFile string) {
	var weightFile string
	var weightDeviceFile string
	// in this important since runc keeps these variables private, they won't be set
	if cgroups.PathExists(filepath.Join(cgroupPath, "blkio.weight")) {
		weightFile = "blkio.weight"
		weightDeviceFile = "blkio.weight_device"
	} else {
		weightFile = "blkio.bfq.weight"
		weightDeviceFile = "blkio.bfq.weight_device"
	}
	return weightFile, weightDeviceFile
}

// SetBlkioThrottle sets the throttle limits for the cgroup.
func SetBlkioThrottle(res *cgroups.Resources, cgroupPath string) error {
	for _, td := range res.BlkioThrottleReadBpsDevice {
		if err := WriteFile(cgroupPath, "blkio.throttle.read_bps_device", fmt.Sprintf("%d:%d %d", td.Major, td.Minor, td.Rate)); err != nil {
			return err
		}
	}
	for _, td := range res.BlkioThrottleWriteBpsDevice {
		if err := WriteFile(cgroupPath, "blkio.throttle.write_bps_device", fmt.Sprintf("%d:%d %d", td.Major, td.Minor, td.Rate)); err != nil {
			return err
		}
	}
	for _, td := range res.BlkioThrottleReadIOPSDevice {
		if err := WriteFile(cgroupPath, "blkio.throttle.read_iops_device", td.String()); err != nil {
			return err
		}
	}
	for _, td := range res.BlkioThrottleWriteIOPSDevice {
		if err := WriteFile(cgroupPath, "blkio.throttle.write_iops_device", td.String()); err != nil {
			return err
		}
	}
	return nil
}

// Code below was moved from podman/utils/utils_supported.go and should properly better
// integrated here as some parts may be redundant.

func getCgroupProcess(procFile string, allowRoot bool) (string, error) {
	f, err := os.Open(procFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	cgroup := ""
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			return "", fmt.Errorf("cannot parse cgroup line %q", line)
		}
		if strings.HasPrefix(line, "0::") {
			cgroup = line[3:]
			break
		}
		if len(parts[2]) > len(cgroup) {
			cgroup = parts[2]
		}
	}
	if len(cgroup) == 0 || (!allowRoot && cgroup == "/") {
		return "", fmt.Errorf("could not find cgroup mount in %q", procFile)
	}
	return cgroup, nil
}

// GetOwnCgroup returns the cgroup for the current process.
func GetOwnCgroup() (string, error) {
	return getCgroupProcess("/proc/self/cgroup", true)
}

func GetOwnCgroupDisallowRoot() (string, error) {
	return getCgroupProcess("/proc/self/cgroup", false)
}

// GetCgroupProcess returns the cgroup for the specified process process.
func GetCgroupProcess(pid int) (string, error) {
	return getCgroupProcess(fmt.Sprintf("/proc/%d/cgroup", pid), true)
}

// MoveUnderCgroupSubtree moves the PID under a cgroup subtree.
func MoveUnderCgroupSubtree(subtree string) error {
	return MoveUnderCgroup("", subtree, nil)
}

// MoveUnderCgroup moves a group of processes to a new cgroup.
// If cgroup is the empty string, then the current calling process cgroup is used.
// If processes is empty, then the processes from the current cgroup are moved.
func MoveUnderCgroup(cgroup, subtree string, processes []uint32) error {
	procFile := "/proc/self/cgroup"
	f, err := os.Open(procFile)
	if err != nil {
		return err
	}
	defer f.Close()

	unifiedMode, err := IsCgroup2UnifiedMode()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			return fmt.Errorf("cannot parse cgroup line %q", line)
		}

		// root cgroup, skip it
		if parts[2] == "/" && (!unifiedMode || parts[1] != "") {
			continue
		}

		cgroupRoot := "/sys/fs/cgroup"
		// Special case the unified mount on hybrid cgroup and named hierarchies.
		// This works on Fedora 31, but we should really parse the mounts to see
		// where the cgroup hierarchy is mounted.
		if parts[1] == "" && !unifiedMode {
			// If it is not using unified mode, the cgroup v2 hierarchy is
			// usually mounted under /sys/fs/cgroup/unified
			cgroupRoot = filepath.Join(cgroupRoot, "unified")

			// Ignore the unified mount if it doesn't exist
			if err := fileutils.Exists(cgroupRoot); err != nil && os.IsNotExist(err) {
				continue
			}
		} else if parts[1] != "" {
			// Assume the controller is mounted at /sys/fs/cgroup/$CONTROLLER.
			controller := strings.TrimPrefix(parts[1], "name=")
			cgroupRoot = filepath.Join(cgroupRoot, controller)
		}

		parentCgroup := cgroup
		if parentCgroup == "" {
			parentCgroup = parts[2]
		}
		newCgroup := filepath.Join(cgroupRoot, parentCgroup, subtree)
		if err := os.MkdirAll(newCgroup, 0o755); err != nil && !os.IsExist(err) {
			return err
		}

		f, err := os.OpenFile(filepath.Join(newCgroup, "cgroup.procs"), os.O_RDWR, 0o755)
		if err != nil {
			return err
		}
		defer f.Close()

		if len(processes) > 0 {
			for _, pid := range processes {
				if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
					logrus.Debugf("Cannot move process %d to cgroup %q: %v", pid, newCgroup, err)
				}
			}
		} else {
			processesData, err := os.ReadFile(filepath.Join(cgroupRoot, parts[2], "cgroup.procs"))
			if err != nil {
				return err
			}
			for pid := range bytes.SplitSeq(processesData, []byte("\n")) {
				if len(pid) == 0 {
					continue
				}
				if _, err := f.Write(pid); err != nil {
					logrus.Debugf("Cannot move process %s to cgroup %q: %v", string(pid), newCgroup, err)
				}
			}
		}
	}
	return nil
}

var (
	maybeMoveToSubCgroupSync    sync.Once
	maybeMoveToSubCgroupSyncErr error
)

// MaybeMoveToSubCgroup moves the current process in a sub cgroup when
// it is running in the root cgroup on a system that uses cgroupv2.
func MaybeMoveToSubCgroup() error {
	maybeMoveToSubCgroupSync.Do(func() {
		unifiedMode, err := IsCgroup2UnifiedMode()
		if err != nil {
			maybeMoveToSubCgroupSyncErr = err
			return
		}
		if !unifiedMode {
			maybeMoveToSubCgroupSyncErr = nil
			return
		}
		cgroup, err := GetOwnCgroup()
		if err != nil {
			maybeMoveToSubCgroupSyncErr = err
			return
		}
		if cgroup == "/" {
			maybeMoveToSubCgroupSyncErr = MoveUnderCgroupSubtree("init")
		}
	})
	return maybeMoveToSubCgroupSyncErr
}
