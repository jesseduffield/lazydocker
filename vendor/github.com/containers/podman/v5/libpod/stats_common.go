//go:build !remote && (linux || freebsd)

package libpod

import (
	"fmt"

	"github.com/containers/podman/v5/libpod/define"
)

// GetContainerStats gets the running stats for a given container.
// The previousStats is used to correctly calculate cpu percentages. You
// should pass nil if there is no previous stat for this container.
func (c *Container) GetContainerStats(previousStats *define.ContainerStats) (*define.ContainerStats, error) {
	stats := new(define.ContainerStats)
	stats.ContainerID = c.ID()
	stats.Name = c.Name()

	if c.config.NoCgroups {
		return nil, fmt.Errorf("cannot run top on container %s as it did not create a cgroup: %w", c.ID(), define.ErrNoCgroups)
	}

	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()
		if err := c.syncContainer(); err != nil {
			return stats, err
		}
	}

	// returns stats with the fields' default values respective of their type
	if c.state.State != define.ContainerStateRunning && c.state.State != define.ContainerStatePaused {
		return stats, nil
	}

	if previousStats == nil {
		previousStats = &define.ContainerStats{
			// if we have no prev stats use the container start time as prev time
			// otherwise we cannot correctly calculate the CPU percentage
			SystemNano: uint64(c.state.StartedTime.UnixNano()),
		}
	}

	netStats, err := getContainerNetIO(c)
	if err != nil {
		return nil, err
	}
	stats.Network = netStats

	if err := c.getPlatformContainerStats(stats, previousStats); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetOnlineCPUs returns the number of online CPUs as set in the container cpu-set using sched_getaffinity
func GetOnlineCPUs(container *Container) (int, error) {
	return getOnlineCPUs(container)
}
