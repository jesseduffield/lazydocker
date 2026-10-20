package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateContainerCPUPercentage(t *testing.T) {
	container := &ContainerStats{}
	container.CPUStats.CPUUsage.TotalUsage = 10
	container.CPUStats.SystemCPUUsage = 10
	container.CPUStats.OnlineCpus = 1
	container.PrecpuStats.CPUUsage.TotalUsage = 5
	container.PrecpuStats.SystemCPUUsage = 2

	assert.EqualValues(t, 62.5, container.CalculateContainerCPUPercentage())
}

func TestCalculateContainerCPUPercentageMultipleCores(t *testing.T) {
	// one fully busy core on a host with 8 cores should be reported as 100%, like `docker stats` does
	container := &ContainerStats{}
	container.CPUStats.CPUUsage.TotalUsage = 2000
	container.CPUStats.SystemCPUUsage = 16000
	container.CPUStats.OnlineCpus = 8
	container.PrecpuStats.CPUUsage.TotalUsage = 1000
	container.PrecpuStats.SystemCPUUsage = 8000

	assert.EqualValues(t, 100, container.CalculateContainerCPUPercentage())
}

func TestCalculateContainerCPUPercentageWithoutOnlineCpus(t *testing.T) {
	// older daemons don't report online_cpus, so we fall back to the length of percpu_usage
	container := &ContainerStats{}
	container.CPUStats.CPUUsage.TotalUsage = 2000
	container.CPUStats.CPUUsage.PercpuUsage = []int64{500, 500, 500, 500}
	container.CPUStats.SystemCPUUsage = 16000
	container.PrecpuStats.CPUUsage.TotalUsage = 1000
	container.PrecpuStats.SystemCPUUsage = 8000

	assert.EqualValues(t, 50, container.CalculateContainerCPUPercentage())
}

func TestCalculateContainerCPUPercentageWithoutDelta(t *testing.T) {
	container := &ContainerStats{}

	assert.EqualValues(t, 0, container.CalculateContainerCPUPercentage())
}

func TestCalculateContainerMemoryUsage(t *testing.T) {
	container := &ContainerStats{}
	container.MemoryStats.Usage = 300
	container.MemoryStats.Limit = 1000

	assert.EqualValues(t, 30, container.CalculateContainerMemoryUsage())
}

func TestCalculateContainerMemoryUsageExcludesInactiveFileOnCgroupV2(t *testing.T) {
	container := &ContainerStats{}
	container.MemoryStats.Usage = 300
	container.MemoryStats.Stats.InactiveFile = 250
	container.MemoryStats.Limit = 1000

	assert.EqualValues(t, 5, container.CalculateContainerMemoryUsage())
}

func TestCalculateContainerMemoryUsageExcludesTotalInactiveFileOnCgroupV1(t *testing.T) {
	container := &ContainerStats{}
	container.MemoryStats.Usage = 300
	container.MemoryStats.Stats.TotalInactiveFile = 100
	container.MemoryStats.Stats.InactiveFile = 50
	container.MemoryStats.Limit = 1000

	assert.EqualValues(t, 20, container.CalculateContainerMemoryUsage())
}

func TestCalculateContainerMemoryUsageIgnoresInactiveFileLargerThanUsage(t *testing.T) {
	container := &ContainerStats{}
	container.MemoryStats.Usage = 300
	container.MemoryStats.Stats.InactiveFile = 400
	container.MemoryStats.Limit = 1000

	assert.EqualValues(t, 30, container.CalculateContainerMemoryUsage())
}

func TestCalculateContainerMemoryUsageWithoutLimit(t *testing.T) {
	container := &ContainerStats{}

	assert.EqualValues(t, 0, container.CalculateContainerMemoryUsage())
}
