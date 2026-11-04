package relay

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newClaudeCacheBreakpointStateRedisCacheForTest builds a standalone
// HybridCache wired to a miniredis instance, independent of the
// sync.Once-protected production singleton (getClaudeCacheBreakpointStatesCache),
// which captures common.RDB only once per process and so cannot be
// re-pointed at a per-test Redis instance. This exercises the same
// Namespace/RedisCodec wiring the production cache uses. Returns the cache
// and the underlying miniredis server, so a test can fast-forward its clock
// to exercise TTL expiry deterministically.
func newClaudeCacheBreakpointStateRedisCacheForTest(t *testing.T) (*cachex.HybridCache[claudeCacheBreakpointState], *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	cache := cachex.NewHybridCache[claudeCacheBreakpointState](cachex.HybridCacheConfig[claudeCacheBreakpointState]{
		Namespace:    cachex.Namespace(claudeCacheBreakpointStateNamespace),
		Redis:        client,
		RedisEnabled: func() bool { return true },
		RedisCodec:   cachex.JSONCodec[claudeCacheBreakpointState]{},
	})
	return cache, server
}

// TestClaudeCacheBreakpointStateRedisRoundTrip verifies the claudeCacheBreakpointState
// migration to cachex.HybridCache: the struct (including its []time.Time and
// []string slices) survives a JSON-over-Redis round trip intact, and Redis's
// native TTL expires the entry as configured.
func TestClaudeCacheBreakpointStateRedisRoundTrip(t *testing.T) {
	cache, _ := newClaudeCacheBreakpointStateRedisCacheForTest(t)

	now := time.Now().Truncate(time.Millisecond)
	want := claudeCacheBreakpointState{
		ParamFp:   12345,
		Ts:        now,
		BlkHash:   []uint64{1, 2, 3},
		BlkKind:   []string{"system", "tools", "text"},
		MsgStart:  []uint16{0, 2},
		BpIdx:     []uint16{0, 2},
		BpTtl:     []string{"1h", "5m"},
		BpExpires: []time.Time{now.Add(time.Hour), now.Add(5 * time.Minute)},
		Expires:   now.Add(time.Hour),
	}

	require.NoError(t, cache.SetWithTTL("session-key", want, time.Hour))

	got, found, err := cache.Get("session-key")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, want.ParamFp, got.ParamFp)
	assert.True(t, want.Ts.Equal(got.Ts))
	assert.Equal(t, want.BlkHash, got.BlkHash)
	assert.Equal(t, want.BlkKind, got.BlkKind)
	assert.Equal(t, want.MsgStart, got.MsgStart)
	assert.Equal(t, want.BpIdx, got.BpIdx)
	assert.Equal(t, want.BpTtl, got.BpTtl)
	require.Len(t, got.BpExpires, len(want.BpExpires))
	for i := range want.BpExpires {
		assert.True(t, want.BpExpires[i].Equal(got.BpExpires[i]))
	}
	assert.True(t, want.Expires.Equal(got.Expires))

	_, found, err = cache.Get("missing-key")
	require.NoError(t, err)
	assert.False(t, found)
}

// TestClaudeCacheBreakpointStateRedisExpires verifies that a short TTL
// actually expires the entry in Redis, matching the semantics
// rememberClaudeCacheBreakpointState relies on (time.Until(state.Expires)).
func TestClaudeCacheBreakpointStateRedisExpires(t *testing.T) {
	cache, server := newClaudeCacheBreakpointStateRedisCacheForTest(t)

	require.NoError(t, cache.SetWithTTL("expiring-key", claudeCacheBreakpointState{ParamFp: 1}, 50*time.Millisecond))

	_, found, err := cache.Get("expiring-key")
	require.NoError(t, err)
	assert.True(t, found)

	server.FastForward(100 * time.Millisecond)

	_, found, err = cache.Get("expiring-key")
	require.NoError(t, err)
	assert.False(t, found)
}
