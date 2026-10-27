package common

import (
	"sync"
	"sync/atomic"
	"time"
)

const requestRateWindowSeconds = 60

type HTTPRuntimeStats struct {
	ActiveRequests    int64
	RequestsPerMinute int64
}

var (
	activeRelayRequests atomic.Int64
	requestRate         requestRateWindow
)

type requestRateWindow struct {
	mu      sync.Mutex
	buckets [requestRateWindowSeconds]int64
	seconds [requestRateWindowSeconds]int64
}

func (w *requestRateWindow) record(at time.Time) {
	second := at.Unix()
	index := second % requestRateWindowSeconds
	w.mu.Lock()
	if w.seconds[index] != second {
		w.seconds[index] = second
		w.buckets[index] = 0
	}
	w.buckets[index]++
	w.mu.Unlock()
}

func (w *requestRateWindow) count(at time.Time) int64 {
	now := at.Unix()
	var count int64
	w.mu.Lock()
	for i, second := range w.seconds {
		if second > 0 && second <= now && now-second < requestRateWindowSeconds {
			count += w.buckets[i]
		}
	}
	w.mu.Unlock()
	return count
}

func BeginRelayRequest() {
	activeRelayRequests.Add(1)
}

func CompleteRelayRequest() {
	activeRelayRequests.Add(-1)
	requestRate.record(time.Now())
}

func GetHTTPRuntimeStats() HTTPRuntimeStats {
	stats := HTTPRuntimeStats{
		ActiveRequests: activeRelayRequests.Load(),
	}
	stats.RequestsPerMinute = requestRate.count(time.Now())
	return stats
}
