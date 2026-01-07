//go:build !remote

package libpod

import (
	"fmt"
	"unsafe"

	"github.com/containers/podman/v5/libpod/define"
	"golang.org/x/sys/unix"
)

func (r *Runtime) setPlatformHostInfo(_ *define.HostInfo) error {
	return nil
}

func timeToPercent(time uint64, total uint64) float64 {
	return 100.0 * float64(time) / float64(total)
}

// getCPUUtilization Returns a CPUUsage object that summarizes CPU
// usage for userspace, system, and idle time.
func getCPUUtilization() (*define.CPUUsage, error) {
	buf, err := unix.SysctlRaw("kern.cp_time")
	if err != nil {
		return nil, fmt.Errorf("reading sysctl kern.cp_time: %w", err)
	}

	var total uint64 = 0
	var times [unix.CPUSTATES]uint64

	for i := 0; i < unix.CPUSTATES; i++ {
		val := *(*uint64)(unsafe.Pointer(&buf[8*i]))
		times[i] = val
		total += val
	}
	return &define.CPUUsage{
		UserPercent:   timeToPercent(times[unix.CP_USER], total),
		SystemPercent: timeToPercent(times[unix.CP_SYS], total),
		IdlePercent:   timeToPercent(times[unix.CP_IDLE], total),
	}, nil
}
