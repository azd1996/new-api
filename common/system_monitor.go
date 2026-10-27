package common

import (
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

// DiskSpaceInfo 磁盘空间信息
type DiskSpaceInfo struct {
	// 总空间（字节）
	Total uint64 `json:"total"`
	// 可用空间（字节）
	Free uint64 `json:"free"`
	// 已用空间（字节）
	Used uint64 `json:"used"`
	// 使用百分比
	UsedPercent float64 `json:"used_percent"`
}

// SystemStatus 系统状态信息
type SystemStatus struct {
	CPUUsage         float64
	MemoryUsage      float64
	DiskUsage        float64
	CPUOverloaded    bool
	MemoryOverloaded bool
	DiskOverloaded   bool
}

var latestSystemStatus atomic.Value

func init() {
	latestSystemStatus.Store(SystemStatus{})
}

// StartSystemMonitor 启动系统监控
func StartSystemMonitor() {
	go func() {
		var consecutive overloadSampleCounts
		for {
			config := GetPerformanceMonitorConfig()
			if !config.Enabled && config.OverloadConsecutiveSamples <= 0 {
				consecutive = overloadSampleCounts{}
				latestSystemStatus.Store(SystemStatus{})
				time.Sleep(30 * time.Second)
				continue
			}

			updateSystemStatus(config, &consecutive)
			time.Sleep(5 * time.Second)
		}
	}()
}

type overloadSampleCounts struct {
	cpu    int
	memory int
	disk   int
}

func updateSystemStatus(config PerformanceMonitorConfig, consecutive *overloadSampleCounts) {
	var status SystemStatus

	// CPU
	// 注意：cpu.Percent(0, false) 返回自上次调用以来的 CPU 使用率
	// 如果是第一次调用，可能会返回错误或不准确的值，但在循环中会逐渐正常
	percents, err := cpu.Percent(0, false)
	if err == nil && len(percents) > 0 {
		status.CPUUsage = percents[0]
	}

	// Memory
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		status.MemoryUsage = memInfo.UsedPercent
	}

	// Disk
	diskInfo := GetDiskSpaceInfo()
	if diskInfo.Total > 0 {
		status.DiskUsage = diskInfo.UsedPercent
	}

	updateOverloadState(&status, config, consecutive)
	latestSystemStatus.Store(status)
}

func updateOverloadState(status *SystemStatus, config PerformanceMonitorConfig, consecutive *overloadSampleCounts) {
	if config.OverloadConsecutiveSamples > 0 {
		if config.CPUThreshold > 0 && int(status.CPUUsage) > config.CPUThreshold {
			consecutive.cpu++
		} else {
			consecutive.cpu = 0
		}
		if config.MemoryThreshold > 0 && int(status.MemoryUsage) > config.MemoryThreshold {
			consecutive.memory++
		} else {
			consecutive.memory = 0
		}
		if config.DiskThreshold > 0 && int(status.DiskUsage) > config.DiskThreshold {
			consecutive.disk++
		} else {
			consecutive.disk = 0
		}
		status.CPUOverloaded = consecutive.cpu >= config.OverloadConsecutiveSamples
		status.MemoryOverloaded = consecutive.memory >= config.OverloadConsecutiveSamples
		status.DiskOverloaded = consecutive.disk >= config.OverloadConsecutiveSamples
	} else {
		*consecutive = overloadSampleCounts{}
	}
}

// GetSystemStatus 获取当前系统状态
func GetSystemStatus() SystemStatus {
	return latestSystemStatus.Load().(SystemStatus)
}
