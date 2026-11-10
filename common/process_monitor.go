package common

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/process"
)

type ProcessStatus struct {
	CPUUsagePercent *float64
	Threads         *int32
	FileDescriptors *int32
}

var (
	latestProcessStatus atomic.Value
	processMonitorOnce  sync.Once
)

func init() {
	latestProcessStatus.Store(ProcessStatus{})
}

func StartProcessMonitor() {
	processMonitorOnce.Do(func() {
		go func() {
			processInfo, err := process.NewProcess(int32(os.Getpid()))
			if err != nil {
				return
			}

			// gopsutil's Process.CPUPercent averages over the whole process
			// lifetime, so CPU usage is derived from the CPU time delta between
			// two samples instead. The first sample after (re-)enabling only
			// records the baseline and reports no value.
			var lastCPUTime float64
			var lastSampleAt time.Time

			for {
				config := GetPerformanceMonitorConfig()
				if config.Enabled {
					var status ProcessStatus
					if times, err := processInfo.Times(); err == nil {
						now := time.Now()
						totalCPUTime := times.Total()
						elapsed := now.Sub(lastSampleAt).Seconds()
						if !lastSampleAt.IsZero() && elapsed > 0 {
							value := 100 * (totalCPUTime - lastCPUTime) / elapsed
							status.CPUUsagePercent = &value
						}
						lastCPUTime, lastSampleAt = totalCPUTime, now
					}
					if value, err := processInfo.NumThreads(); err == nil {
						status.Threads = &value
					}
					if value, err := processInfo.NumFDs(); err == nil {
						status.FileDescriptors = &value
					}
					latestProcessStatus.Store(status)
				} else {
					lastSampleAt = time.Time{}
					latestProcessStatus.Store(ProcessStatus{})
				}
				time.Sleep(5 * time.Second)
			}
		}()
	})
}

func GetProcessStatus() ProcessStatus {
	status, ok := latestProcessStatus.Load().(ProcessStatus)
	if !ok {
		return ProcessStatus{}
	}
	return status
}
