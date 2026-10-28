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

			for {
				config := GetPerformanceMonitorConfig()
				if config.Enabled {
					var status ProcessStatus
					if value, err := processInfo.CPUPercent(); err == nil {
						status.CPUUsagePercent = &value
					}
					if value, err := processInfo.NumThreads(); err == nil {
						status.Threads = &value
					}
					if value, err := processInfo.NumFDs(); err == nil {
						status.FileDescriptors = &value
					}
					latestProcessStatus.Store(status)
				} else {
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
