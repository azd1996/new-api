package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdateOverloadStateRequiresConsecutiveSamples(t *testing.T) {
	config := PerformanceMonitorConfig{
		OverloadConsecutiveSamples: 3,
		CPUThreshold:               90,
		MemoryThreshold:            90,
		DiskThreshold:              95,
	}
	counts := overloadSampleCounts{}

	for sample := 1; sample <= 2; sample++ {
		status := SystemStatus{CPUUsage: 95}
		updateOverloadState(&status, config, &counts)
		assert.False(t, status.CPUOverloaded)
	}

	status := SystemStatus{CPUUsage: 95}
	updateOverloadState(&status, config, &counts)
	assert.True(t, status.CPUOverloaded)

	status = SystemStatus{CPUUsage: 80}
	updateOverloadState(&status, config, &counts)
	assert.False(t, status.CPUOverloaded)
	assert.Zero(t, counts.cpu)
}

func TestUpdateOverloadStateDisablesProtectionAtZero(t *testing.T) {
	counts := overloadSampleCounts{cpu: 4, memory: 2, disk: 1}
	status := SystemStatus{CPUUsage: 99, MemoryUsage: 99, DiskUsage: 99}

	updateOverloadState(&status, PerformanceMonitorConfig{}, &counts)

	assert.False(t, status.CPUOverloaded)
	assert.False(t, status.MemoryOverloaded)
	assert.False(t, status.DiskOverloaded)
	assert.Equal(t, overloadSampleCounts{}, counts)
}
