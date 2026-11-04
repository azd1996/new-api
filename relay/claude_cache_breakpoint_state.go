package relay

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samber/hot"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

type claudeCacheBreakpointState struct {
	ParamFp   uint64
	Ts        time.Time
	BlkHash   []uint64
	BlkKind   []string
	MsgStart  []uint16
	BpIdx     []uint16
	BpTtl     []string
	BpExpires []time.Time
	Expires   time.Time
}

const (
	claudeCacheBreakpointStateLimit     = 4096
	claudeCacheBreakpointStateNamespace = "new-api:claude_cache_breakpoint_state:v1"
)

// claudeCacheBreakpointStates is a Redis-backed cache when Redis is enabled,
// falling back to a process-local, capacity-bounded cache otherwise. A
// process-local cache only lets a retained breakpoint survive across requests
// that land on the same instance: on a multi-instance deployment it reflects
// state this instance alone wrote, not a global view, unless Redis is
// enabled or the load balancer keeps a session on one instance.
var claudeCacheBreakpointStatesOnce sync.Once
var claudeCacheBreakpointStatesCache *cachex.HybridCache[claudeCacheBreakpointState]

func getClaudeCacheBreakpointStatesCache() *cachex.HybridCache[claudeCacheBreakpointState] {
	claudeCacheBreakpointStatesOnce.Do(func() {
		claudeCacheBreakpointStatesCache = cachex.NewHybridCache[claudeCacheBreakpointState](cachex.HybridCacheConfig[claudeCacheBreakpointState]{
			Namespace: cachex.Namespace(claudeCacheBreakpointStateNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[claudeCacheBreakpointState]{},
			Memory: func() *hot.HotCache[string, claudeCacheBreakpointState] {
				return hot.NewHotCache[string, claudeCacheBreakpointState](hot.LRU, claudeCacheBreakpointStateLimit).
					// WithTTL only sets the janitor's sweep interval and the
					// default used by Set(); every write here goes through
					// SetWithTTL with its own per-entry expiry instead.
					WithTTL(5 * time.Minute).
					WithJanitor().
					Build()
			},
		})
	})
	return claudeCacheBreakpointStatesCache
}

// The caller scopes this key with the trace customer ID.
func claudeCacheBreakpointStateKey(info *relaycommon.RelayInfo, row *model.CacheSkeleton) string {
	if info == nil || info.ChannelMeta == nil || row == nil || row.SessionId == "" || row.Model == "" || row.Lane == 0 {
		return ""
	}
	return strings.Join([]string{
		strconv.Itoa(max(info.TokenId, 0)),
		strconv.FormatUint(row.ParamFp, 10),
		row.SessionId,
		strconv.FormatUint(uint64(max(info.ChannelId, 0)), 10),
		strconv.FormatUint(uint64(max(info.ChannelMultiKeyIndex, 0)), 10),
		row.Model,
		strconv.FormatUint(row.Lane, 10),
	}, "\x1f")
}

func getClaudeCacheBreakpointState(key string, now time.Time) *claudeCacheBreakpointState {
	if key == "" {
		return nil
	}
	state, found, err := getClaudeCacheBreakpointStatesCache().Get(key)
	if err != nil || !found || !now.Before(state.Expires) {
		return nil
	}
	return &state
}

func rememberClaudeCacheBreakpointState(key string, row *model.CacheSkeleton) {
	if key == "" || row == nil || row.Status != "ok" || row.CacheCreation == 0 || len(row.BpIdx) == 0 || row.Ts.IsZero() {
		return
	}
	now := time.Now()
	state := claudeCacheBreakpointState{ParamFp: row.ParamFp, Ts: row.Ts}
	lastIndex := -1
	for i, bp := range row.BpIdx {
		if len(state.BpIdx) == claudeMaxCacheBreakpoints {
			break
		}
		if int(bp) >= len(row.BlkHash) {
			continue
		}
		ttl := claudeDefaultCacheTTL
		if i < len(row.BpTtl) && row.BpTtl[i] != "" {
			ttl = row.BpTtl[i]
		}
		duration := 5 * time.Minute
		switch ttl {
		case claudeExtendedCacheTTL:
			duration = time.Hour
		case claudeDefaultCacheTTL:
		default:
			continue
		}
		// Request start is a conservative lower bound on cache-write time.
		expires := row.Ts.Add(duration)
		if !now.Before(expires) {
			continue
		}
		state.BpIdx = append(state.BpIdx, bp)
		state.BpTtl = append(state.BpTtl, ttl)
		state.BpExpires = append(state.BpExpires, expires)
		lastIndex = max(lastIndex, int(bp))
		if expires.After(state.Expires) {
			state.Expires = expires
		}
	}
	if lastIndex < 0 {
		return
	}
	state.BlkHash = append([]uint64(nil), row.BlkHash[:lastIndex+1]...)
	if len(row.BlkKind) >= lastIndex+1 {
		state.BlkKind = append([]string(nil), row.BlkKind[:lastIndex+1]...)
	}
	for _, start := range row.MsgStart {
		if int(start) <= lastIndex {
			state.MsgStart = append(state.MsgStart, start)
		}
	}

	cache := getClaudeCacheBreakpointStatesCache()
	ttl := time.Until(state.Expires)
	if ttl <= 0 {
		return
	}
	// A newer write for the same key must not be clobbered by a stale,
	// slower-arriving request still in flight for an older attempt. This
	// read-modify-write is not atomic against a concurrent writer on another
	// instance when Redis is enabled; losing that narrow race only means an
	// older snapshot is kept a little longer; it never serves a snapshot
	// newer than what was actually written.
	if previous, found, err := cache.Get(key); err == nil && found && row.Ts.Before(previous.Ts) {
		return
	}
	_ = cache.SetWithTTL(key, state, ttl)
}
