package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateContainerCPUPercentage(t *testing.T) {
	container := &ContainerStats{}
	container.CPUStats.CPUUsage.TotalUsage = 10
	container.CPUStats.SystemCPUUsage = 10
	container.PrecpuStats.CPUUsage.TotalUsage = 5
	container.PrecpuStats.SystemCPUUsage = 2

	assert.EqualValues(t, 62.5, container.CalculateContainerCPUPercentage())
}
