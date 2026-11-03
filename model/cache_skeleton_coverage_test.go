package model

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func coverageFixture(at time.Time, n int, baseline, optimized []uint16) *CacheSkeleton {
	hashes := make([]uint64, n)
	lengths := make([]uint32, n)
	kinds := make([]string, n)
	for i := range n {
		hashes[i], lengths[i], kinds[i] = uint64(i+1), 100, "user.text"
	}
	return &CacheSkeleton{
		Ts: at, EndTs: at.Add(time.Second), RequestId: strconv.FormatInt(at.UnixNano(), 10),
		Status: "ok", Provider: SkeletonProviderClaude, Model: "claude-sonnet-4",
		Lane: 1, ParamFp: 7, Input: 10000, TokenId: 1, SessionId: "session",
		BlkHash: hashes, BlkLen: lengths, BlkKind: kinds, BpIdx: optimized,
		BaselineHash: hashes, BaselineLen: lengths, BaselineBpIdx: baseline, BaselineParamFp: 7,
		CoverageHash: hashes, CoverageLen: lengths, CoverageBpIdx: optimized,
	}
}

func TestCacheCoverageIndependentHistories(t *testing.T) {
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	first := coverageFixture(at, 2, nil, []uint16{1})
	next := coverageFixture(at.Add(time.Minute), 3, nil, []uint16{2})
	got := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next})
	require.Len(t, got.Lanes, 1)
	turn := got.Lanes[0].Turns[1]
	assert.True(t, turn.CoverageAvailable)
	assert.Zero(t, turn.BaselineBytes, "baseline must not borrow optimized writes")
	assert.Equal(t, uint64(200), turn.OptimizedBytes)
	assert.Equal(t, int64(200), turn.CoverageDeltaBytes)
	assert.Equal(t, SkeletonCoverage{OptimizedCovered: 200, Delta: 200, TotalBytes: 500, ComparableTurns: 2}, got.Coverage)
}

func TestCacheCoverageTTLAndSignedRegression(t *testing.T) {
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                  string
		baseTTL, optimizedTTL string
		delta                 int64
	}{
		{"extended TTL gain", "5m", "1h", 200},
		{"regression stays negative", "1h", "5m", -200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := coverageFixture(at, 2, []uint16{1}, []uint16{1})
			first.BaselineBpTtl, first.CoverageBpTtl = []string{tc.baseTTL}, []string{tc.optimizedTTL}
			next := coverageFixture(at.Add(10*time.Minute), 3, []uint16{2}, []uint16{2})
			got := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next})
			assert.Equal(t, tc.delta, got.Lanes[0].Turns[1].CoverageDeltaBytes)
			assert.Equal(t, tc.delta, got.Coverage.Delta)
		})
	}
}

func TestCacheCoverageBoundaries(t *testing.T) {
	at := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(first, next *CacheSkeleton)
	}{
		{"pending write", func(a, b *CacheSkeleton) { a.EndTs = b.Ts.Add(time.Second) }},
		{"different channel", func(a, b *CacheSkeleton) { b.ChannelId = a.ChannelId + 1 }},
		{"different token", func(a, b *CacheSkeleton) { b.TokenId = a.TokenId + 1 }},
		{"different key", func(a, b *CacheSkeleton) { b.KeyIdx = a.KeyIdx + 1 }},
		{"parameter change", func(a, b *CacheSkeleton) { b.ParamFp++; b.BaselineParamFp++ }},
		{"too short", func(a, b *CacheSkeleton) { a.Input = 10 }},
		{"failed write", func(a, b *CacheSkeleton) { a.Status = "error" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := coverageFixture(at, 2, nil, []uint16{1})
			next := coverageFixture(at.Add(time.Minute), 3, nil, []uint16{2})
			tc.change(first, next)
			got := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next})
			assert.Zero(t, got.Lanes[0].Turns[1].OptimizedBytes)
		})
	}
	t.Run("legacy data unavailable", func(t *testing.T) {
		legacy := coverageFixture(at, 2, nil, []uint16{1})
		legacy.BaselineHash = nil
		got := AnalyzeCacheSkeletons([]*CacheSkeleton{legacy})
		assert.False(t, got.Lanes[0].Turns[0].CoverageAvailable)
		assert.Zero(t, got.Coverage.ComparableTurns)
		first := coverageFixture(at.Add(time.Minute), 2, nil, []uint16{1})
		next := coverageFixture(at.Add(2*time.Minute), 3, nil, []uint16{2})
		got = AnalyzeCacheSkeletons([]*CacheSkeleton{legacy, first, next})
		assert.Equal(t, int64(2), got.Coverage.ComparableTurns)
		assert.Zero(t, got.Lanes[0].Turns[1].OptimizedBytes)
		assert.Equal(t, uint64(200), got.Lanes[0].Turns[2].OptimizedBytes)
	})
	t.Run("unfinished concurrent write cannot extend expired cache", func(t *testing.T) {
		first := coverageFixture(at, 2, nil, []uint16{1})
		slow := coverageFixture(at.Add(4*time.Minute), 2, nil, []uint16{1})
		slow.EndTs = at.Add(20 * time.Minute)
		next := coverageFixture(at.Add(10*time.Minute), 2, nil, []uint16{1})
		got := AnalyzeCacheSkeletons([]*CacheSkeleton{first, slow, next})
		assert.Equal(t, uint64(200), got.Lanes[0].Turns[1].OptimizedBytes)
		assert.Zero(t, got.Lanes[0].Turns[2].OptimizedBytes)
	})
	t.Run("lookback includes exactly twenty blocks", func(t *testing.T) {
		first := coverageFixture(at, 2, nil, []uint16{1})
		inside := coverageFixture(at.Add(time.Minute), 21, nil, []uint16{20})
		outside := coverageFixture(at.Add(time.Minute), 22, nil, []uint16{21})
		assert.Equal(t, uint64(200), AnalyzeCacheSkeletons([]*CacheSkeleton{first, inside}).Lanes[0].Turns[1].OptimizedBytes)
		assert.Zero(t, AnalyzeCacheSkeletons([]*CacheSkeleton{first, outside}).Lanes[0].Turns[1].OptimizedBytes)
	})
}
