package common

import (
	"context"
	"io"
	"sync"
)

// ProcessShutdownInfo identifies the forced shutdown that interrupted a request.
type ProcessShutdownInfo struct {
	Instance string `json:"instance"`
	BatchID  string `json:"batch_id"`
	At       int64  `json:"at"`
	Reason   string `json:"reason"`
}

func (info *ProcessShutdownInfo) Error() string {
	return "request interrupted by process shutdown"
}

type relayShutdownContextKey struct{}

type relayShutdownRequest struct {
	upstream context.Context
	cancel   context.CancelCauseFunc
}

// RelayRequestRegistry tracks handlers through their synchronous log/settlement
// cleanup. It deliberately does not cancel upstream work on client disconnect:
// existing streaming accounting must still have a chance to collect usage.
type RelayRequestRegistry struct {
	mu       sync.Mutex
	active   map[*relayShutdownRequest]struct{}
	idle     chan struct{}
	draining bool
}

var RelayRequests = NewRelayRequestRegistry()

func NewRelayRequestRegistry() *RelayRequestRegistry {
	idle := make(chan struct{})
	close(idle)
	return &RelayRequestRegistry{
		active: make(map[*relayShutdownRequest]struct{}),
		idle:   idle,
	}
}

func (registry *RelayRequestRegistry) Begin(ctx context.Context) (context.Context, func(), bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.draining {
		return ctx, nil, false
	}
	upstream, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	request := &relayShutdownRequest{upstream: upstream, cancel: cancel}
	if len(registry.active) == 0 {
		registry.idle = make(chan struct{})
	}
	registry.active[request] = struct{}{}
	var once sync.Once
	done := func() {
		once.Do(func() {
			registry.mu.Lock()
			delete(registry.active, request)
			if len(registry.active) == 0 {
				close(registry.idle)
			}
			registry.mu.Unlock()
			cancel(nil)
		})
	}
	return context.WithValue(ctx, relayShutdownContextKey{}, request), done, true
}

func (registry *RelayRequestRegistry) Drain() {
	registry.mu.Lock()
	registry.draining = true
	registry.mu.Unlock()
}

// ForceCancel publishes the cause before waking readers and websocket closers.
func (registry *RelayRequestRegistry) ForceCancel(info ProcessShutdownInfo) int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.draining = true
	for request := range registry.active {
		request.cancel(&info)
	}
	return len(registry.active)
}

func (registry *RelayRequestRegistry) Wait(ctx context.Context) error {
	registry.mu.Lock()
	idle := registry.idle
	registry.mu.Unlock()
	select {
	case <-idle:
		return nil
	default:
	}
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RelayUpstreamContext only responds to process shutdown, preserving the
// existing behavior of upstream requests that outlive a disconnected client.
func RelayUpstreamContext(ctx context.Context) context.Context {
	if ctx != nil {
		if request, ok := ctx.Value(relayShutdownContextKey{}).(*relayShutdownRequest); ok {
			return request.upstream
		}
	}
	return context.Background()
}

func ProcessShutdownFromContext(ctx context.Context) *ProcessShutdownInfo {
	info, _ := context.Cause(RelayUpstreamContext(ctx)).(*ProcessShutdownInfo)
	return info
}

// TrackRelayCloser unblocks hijacked sockets and request-body reads on forced
// shutdown. Stop the callback before releasing the resource on normal return.
func TrackRelayCloser(ctx context.Context, closer io.Closer) func() {
	stop := context.AfterFunc(RelayUpstreamContext(ctx), func() { _ = closer.Close() })
	return func() { stop() }
}

func AttachProcessShutdownInfo(ctx context.Context, other map[string]interface{}) map[string]interface{} {
	info := ProcessShutdownFromContext(ctx)
	if info == nil {
		return other
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	adminInfo, _ := other["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["process_shutdown"] = info
	return other
}
