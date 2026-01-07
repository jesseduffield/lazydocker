package cgroupv2

import (
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	isCgroupV2Once sync.Once
	isCgroupV2     bool
	isCgroupV2Err  error
)

// Enabled returns whether we are running on cgroup v2.
func Enabled() (bool, error) {
	isCgroupV2Once.Do(func() {
		var st syscall.Statfs_t
		if err := syscall.Statfs("/sys/fs/cgroup", &st); err != nil {
			isCgroupV2, isCgroupV2Err = false, err
		} else {
			isCgroupV2, isCgroupV2Err = st.Type == unix.CGROUP2_SUPER_MAGIC, nil
		}
	})
	return isCgroupV2, isCgroupV2Err
}
