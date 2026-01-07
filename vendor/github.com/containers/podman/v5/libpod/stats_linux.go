//go:build !remote

package libpod

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	runccgroup "github.com/opencontainers/cgroups"
	"go.podman.io/common/pkg/cgroups"
	"golang.org/x/sys/unix"
)

// getPlatformContainerStats gets the platform-specific running stats
// for a given container.  The previousStats is used to correctly
// calculate cpu percentages. You should pass nil if there is no
// previous stat for this container.
func (c *Container) getPlatformContainerStats(stats *define.ContainerStats, previousStats *define.ContainerStats) error {
	if c.config.NoCgroups {
		return fmt.Errorf("cannot run top on container %s as it did not create a cgroup: %w", c.ID(), define.ErrNoCgroups)
	}

	cgroupPath, err := c.cGroupPath()
	if err != nil {
		return err
	}
	cgroup, err := cgroups.Load(cgroupPath)
	if err != nil {
		return fmt.Errorf("unable to load cgroup at %s: %w", cgroupPath, err)
	}

	// Ubuntu does not have swap memory in cgroups because swap is often not enabled.
	cgroupStats, err := cgroup.Stat()
	if err != nil {
		// cgroup.Stat() is not an atomic operation, so it is possible that the cgroup is removed
		// while Stat() is running.  Try to catch this case and return a more specific error.
		if (errors.Is(err, cgroups.ErrStatCgroup) || errors.Is(err, unix.ENODEV)) && !cgroupExist(cgroupPath) {
			return fmt.Errorf("cgroup %s does not exist: %w", cgroupPath, define.ErrCtrStopped)
		}
		return fmt.Errorf("unable to obtain cgroup stats: %w", err)
	}
	conState := c.state.State

	// If the current total usage in the cgroup is less than what was previously
	// recorded then it means the container was restarted and runs in a new cgroup
	if previousStats.Duration > cgroupStats.CpuStats.CpuUsage.TotalUsage {
		previousStats = &define.ContainerStats{}
	}

	previousCPU := previousStats.CPUNano
	now := uint64(time.Now().UnixNano())
	stats.Duration = cgroupStats.CpuStats.CpuUsage.TotalUsage
	stats.UpTime = time.Duration(stats.Duration)
	stats.CPU = calculateCPUPercent(cgroupStats, previousCPU, now, previousStats.SystemNano)
	// calc the average cpu usage for the time the container is running
	stats.AvgCPU = calculateCPUPercent(cgroupStats, 0, now, uint64(c.state.StartedTime.UnixNano()))
	stats.MemUsage = cgroupStats.MemoryStats.Usage.Usage
	stats.MemLimit = c.getMemLimit(cgroupStats.MemoryStats.Usage.Limit)
	stats.MemPerc = (float64(stats.MemUsage) / float64(stats.MemLimit)) * 100
	stats.PIDs = 0
	if conState == define.ContainerStateRunning || conState == define.ContainerStatePaused {
		stats.PIDs = cgroupStats.PidsStats.Current
	}
	stats.BlockInput, stats.BlockOutput = calculateBlockIO(cgroupStats)
	stats.CPUNano = cgroupStats.CpuStats.CpuUsage.TotalUsage
	stats.CPUSystemNano = cgroupStats.CpuStats.CpuUsage.UsageInKernelmode
	stats.SystemNano = now
	stats.PerCPU = cgroupStats.CpuStats.CpuUsage.PercpuUsage

	return nil
}

// getMemLimit returns the memory limit for a container
func (c *Container) getMemLimit(memLimit uint64) uint64 {
	si := &syscall.Sysinfo_t{}
	err := syscall.Sysinfo(si)
	if err != nil {
		return memLimit
	}

	//nolint:unconvert
	physicalLimit := uint64(si.Totalram)

	if memLimit <= 0 || memLimit > physicalLimit {
		return physicalLimit
	}

	return memLimit
}

// calculateCPUPercent calculates the cpu usage using the latest measurement in stats.
// previousCPU is the last value of stats.CPU.Usage.Total measured at the time previousSystem.
//
//	(now - previousSystem) is the time delta in nanoseconds, between the measurement in previousCPU
//
// and the updated value in stats.
func calculateCPUPercent(stats *runccgroup.Stats, previousCPU, now, previousSystem uint64) float64 {
	var (
		cpuPercent  = 0.0
		cpuDelta    = float64(stats.CpuStats.CpuUsage.TotalUsage - previousCPU)
		systemDelta = float64(now - previousSystem)
	)
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		// gets a ratio of container cpu usage total, and multiplies that by 100 to get a percentage
		cpuPercent = (cpuDelta / systemDelta) * 100
	}
	return cpuPercent
}

func calculateBlockIO(stats *runccgroup.Stats) (read uint64, write uint64) {
	for _, blkIOEntry := range stats.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(blkIOEntry.Op) {
		case "read":
			read += blkIOEntry.Value
		case "write":
			write += blkIOEntry.Value
		}
	}
	return
}

func getOnlineCPUs(container *Container) (int, error) {
	ctrPID, err := container.PID()
	if err != nil {
		return -1, fmt.Errorf("failed to obtain Container %s PID: %w", container.Name(), err)
	}
	if ctrPID == 0 {
		return ctrPID, define.ErrCtrStopped
	}
	var cpuSet unix.CPUSet
	if err := unix.SchedGetaffinity(ctrPID, &cpuSet); err != nil {
		return -1, fmt.Errorf("failed to obtain Container %s online cpus: %w", container.Name(), err)
	}
	return cpuSet.Count(), nil
}
