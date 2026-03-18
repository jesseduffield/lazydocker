package commands

import (
	"math"
	"time"
)

type RecordedStats struct {
	ClientStats  ContainerStats
	DerivedStats DerivedStats
	RecordedAt   time.Time
}

type DerivedStats struct {
	CPUPercentage    float64
	MemoryPercentage float64
}

type ContainerStats struct {
	ID               string `json:"id"`
	CPUUsageUsec     int64  `json:"cpuUsageUsec"`
	MemoryUsageBytes int64  `json:"memoryUsageBytes"`
	MemoryLimitBytes int64  `json:"memoryLimitBytes"`
	NetworkRxBytes   int64  `json:"networkRxBytes"`
	NetworkTxBytes   int64  `json:"networkTxBytes"`
	BlockReadBytes   int64  `json:"blockReadBytes"`
	BlockWriteBytes  int64  `json:"blockWriteBytes"`
	NumProcesses     int    `json:"numProcesses"`

	PrevCPUUsageUsec int64 `json:"-"`
	TimeDeltaUsec    int64 `json:"-"`
}

func (s *ContainerStats) CalculateContainerCPUPercentage() float64 {
	if s.TimeDeltaUsec == 0 {
		return 0
	}
	cpuDelta := s.CPUUsageUsec - s.PrevCPUUsageUsec
	if cpuDelta < 0 {
		cpuDelta = s.CPUUsageUsec
	}
	value := float64(cpuDelta*1000000) / float64(s.TimeDeltaUsec)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func (s *ContainerStats) CalculateContainerMemoryUsage() float64 {
	if s.MemoryLimitBytes == 0 {
		return 0
	}
	value := float64(s.MemoryUsageBytes*100) / float64(s.MemoryLimitBytes)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func (c *Container) appendStats(stats *RecordedStats, maxDuration time.Duration) {
	c.StatsMutex.Lock()
	defer c.StatsMutex.Unlock()

	c.StatHistory = append(c.StatHistory, stats)
	c.eraseOldHistory(maxDuration)
}

func (c *Container) eraseOldHistory(maxDuration time.Duration) {
	if maxDuration == 0 {
		return
	}

	for i, stat := range c.StatHistory {
		if time.Since(stat.RecordedAt) < maxDuration {
			c.StatHistory = c.StatHistory[i:]
			return
		}
	}
}

func (c *Container) GetLastStats() (*RecordedStats, bool) {
	c.StatsMutex.Lock()
	defer c.StatsMutex.Unlock()
	history := c.StatHistory
	if len(history) == 0 {
		return nil, false
	}
	return history[len(history)-1], true
}

func ConvertAppleStatsToContainerStats(appleStats AppleContainerStats, prevStats *ContainerStats, timeDelta time.Duration) *ContainerStats {
	stats := &ContainerStats{
		ID:               appleStats.ID,
		CPUUsageUsec:     appleStats.CPUUsageUsec,
		MemoryUsageBytes: appleStats.MemoryUsageBytes,
		MemoryLimitBytes: appleStats.MemoryLimitBytes,
		NetworkRxBytes:   appleStats.NetworkRxBytes,
		NetworkTxBytes:   appleStats.NetworkTxBytes,
		BlockReadBytes:   appleStats.BlockReadBytes,
		BlockWriteBytes:  appleStats.BlockWriteBytes,
		NumProcesses:     appleStats.NumProcesses,
	}

	if prevStats != nil {
		stats.PrevCPUUsageUsec = prevStats.CPUUsageUsec
		stats.TimeDeltaUsec = int64(timeDelta.Microseconds())
	}

	return stats
}
