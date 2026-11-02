package relay

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
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

const claudeCacheBreakpointStateLimit = 4096

var claudeCacheBreakpointStates = struct {
	sync.Mutex
	items     map[string]claudeCacheBreakpointState
	nextSweep time.Time
}{
	items: make(map[string]claudeCacheBreakpointState),
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

// sweepClaudeCacheBreakpointStates runs under the state lock, at most once per
// minute of activity. Idle stores remain bounded by the hard entry limit.
func sweepClaudeCacheBreakpointStates(now time.Time) {
	if now.Before(claudeCacheBreakpointStates.nextSweep) {
		return
	}
	for key, state := range claudeCacheBreakpointStates.items {
		if !now.Before(state.Expires) {
			delete(claudeCacheBreakpointStates.items, key)
		}
	}
	claudeCacheBreakpointStates.nextSweep = now.Add(time.Minute)
}

func getClaudeCacheBreakpointState(key string, now time.Time) *claudeCacheBreakpointState {
	if key == "" {
		return nil
	}
	claudeCacheBreakpointStates.Lock()
	defer claudeCacheBreakpointStates.Unlock()
	sweepClaudeCacheBreakpointStates(now)
	state, ok := claudeCacheBreakpointStates.items[key]
	if !ok || !now.Before(state.Expires) {
		delete(claudeCacheBreakpointStates.items, key)
		return nil
	}
	return &claudeCacheBreakpointState{
		ParamFp:   state.ParamFp,
		Ts:        state.Ts,
		BlkHash:   append([]uint64(nil), state.BlkHash...),
		BlkKind:   append([]string(nil), state.BlkKind...),
		MsgStart:  append([]uint16(nil), state.MsgStart...),
		BpIdx:     append([]uint16(nil), state.BpIdx...),
		BpTtl:     append([]string(nil), state.BpTtl...),
		BpExpires: append([]time.Time(nil), state.BpExpires...),
		Expires:   state.Expires,
	}
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
	claudeCacheBreakpointStates.Lock()
	defer claudeCacheBreakpointStates.Unlock()
	sweepClaudeCacheBreakpointStates(now)
	if previous, ok := claudeCacheBreakpointStates.items[key]; ok {
		if row.Ts.Before(previous.Ts) {
			return
		}
	} else if len(claudeCacheBreakpointStates.items) >= claudeCacheBreakpointStateLimit {
		// Evict the snapshot that expires first; capacity never grows.
		oldestKey := ""
		var oldest time.Time
		for candidate, previous := range claudeCacheBreakpointStates.items {
			if oldest.IsZero() || previous.Expires.Before(oldest) {
				oldestKey, oldest = candidate, previous.Expires
			}
		}
		delete(claudeCacheBreakpointStates.items, oldestKey)
	}
	claudeCacheBreakpointStates.items[key] = state
}
