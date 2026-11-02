package model

import (
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// TestCacheSkeletonSchemaParses guards the array columns: an untyped slice
// field makes GORM treat it as an association, and every query on the model
// then fails with "unsupported data type".
func TestCacheSkeletonSchemaParses(t *testing.T) {
	s, err := schema.Parse(&[]*CacheSkeleton{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	assert.Equal(t, "cache_skeletons", s.Table)
	assert.Empty(t, s.Relationships.Relations)
	for _, column := range []string{"blk_hash", "blk_kind", "blk_name", "blk_len", "msg_start", "bp_idx", "bp_ttl"} {
		assert.NotNil(t, s.LookUpField(column), column)
	}
}

// TestCacheSkeletonClickHouseRoundTrip needs a real ClickHouse; point
// TEST_CLICKHOUSE_DSN at a scratch database, e.g.
// clickhouse://default:pwd@127.0.0.1:9000/skeleton_test.
func TestCacheSkeletonClickHouseRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TEST_CLICKHOUSE_DSN is not configured")
	}
	db, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	prevDB, prevType := LOG_DB, common.LogDatabaseType()
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	t.Cleanup(func() {
		LOG_DB = prevDB
		common.SetLogDatabaseType(prevType)
	})

	// Migration is idempotent: startup runs it on every boot.
	require.NoError(t, MigrateCacheSkeletons())
	require.NoError(t, MigrateCacheSkeletons())

	requestId := "skeleton-test-" + time.Now().Format("150405.000000")
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch := []*CacheSkeleton{
		{
			Ts: now, EndTs: now.Add(time.Second), RequestId: requestId, Attempt: 1, Status: "ok",
			Node: "n1", TokenId: 7, SessionId: "s1", Lane: 1<<63 + 5, ChannelId: 12, KeyIdx: 3,
			Model: "claude-sonnet-5-5", ParamFp: 1<<63 + 9, Thinking: "adaptive", Effort: "high",
			BlkHash:   []uint64{1<<63 + 1, 2, 3},
			BlkKind:   []string{"tool", "system.text", "user.text"},
			BlkName:   []string{"Bash", "", ""},
			BlkLen:    []uint32{100, 200, 30},
			MsgStart:  []uint16{2},
			BpIdx:     []uint16{1},
			BpTtl:     []string{"1h"},
			CacheRead: 25100, CacheCreation: 48900, Input: 3,
		},
		{
			Ts: now, EndTs: now, RequestId: requestId, Attempt: 0, Status: "error",
			BlkHash: []uint64{}, BlkKind: []string{}, BlkName: []string{}, BlkLen: []uint32{},
			MsgStart: []uint16{}, BpIdx: []uint16{}, BpTtl: []string{},
		},
	}
	require.NoError(t, LOG_DB.Create(&batch).Error)

	rows, err := GetCacheSkeletons(requestId)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, uint8(0), rows[0].Attempt)
	assert.Equal(t, "error", rows[0].Status)
	assert.Empty(t, rows[0].BlkHash)

	got := rows[1]
	assert.Equal(t, batch[0].BlkHash, got.BlkHash)
	assert.Equal(t, batch[0].BlkKind, got.BlkKind)
	assert.Equal(t, batch[0].BlkName, got.BlkName)
	assert.Equal(t, batch[0].BlkLen, got.BlkLen)
	assert.Equal(t, batch[0].MsgStart, got.MsgStart)
	assert.Equal(t, batch[0].BpIdx, got.BpIdx)
	assert.Equal(t, batch[0].BpTtl, got.BpTtl)
	assert.Equal(t, batch[0].Lane, got.Lane)
	assert.Equal(t, batch[0].ParamFp, got.ParamFp)
	assert.Equal(t, uint32(25100), got.CacheRead)
	assert.True(t, now.Equal(got.Ts.UTC()))
}

// skeletonFixture builds one successful turn: every block is 1000 bytes
// except a trailing "system.text" message (100 bytes), and the turn reports
// totalTokens of input with cacheRead read from cache.
func skeletonFixture(ts time.Time, channelId uint32, hashes []uint64, kinds []string, msgStart []uint16, bp uint16, cacheRead uint32) *CacheSkeleton {
	lens := make([]uint32, len(hashes))
	for i := range lens {
		lens[i] = 1000
	}
	last := len(kinds) - 1
	if int(msgStart[len(msgStart)-1]) == last && kinds[last] == "system.text" {
		lens[last] = 100
	}
	return &CacheSkeleton{
		Ts: ts, EndTs: ts.Add(10 * time.Second), RequestId: ts.Format("150405"), Status: "ok",
		Lane: 1, ChannelId: channelId, Model: "claude-sonnet-4-5", ParamFp: 9,
		BlkHash: hashes, BlkKind: kinds, BlkLen: lens, MsgStart: msgStart,
		BpIdx: []uint16{bp}, BpTtl: []string{""},
		CacheRead: cacheRead, Input: 30000 - cacheRead,
	}
}

func TestAnalyzeCacheSkeletons(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	stable := []string{"tool", "system.text", "user.text"}
	followUp := append(append([]string{}, stable...), "assistant.text", "user.text")

	t.Run("misplaced tail breakpoint is recovered by the adjustment", func(t *testing.T) {
		// The client marks a trailing <total_tokens> system message that the
		// next turn rewrites, so its cache entry is never read again.
		rows := []*CacheSkeleton{
			skeletonFixture(t0, 1, []uint64{1, 2, 3, 100}, append(append([]string{}, stable...), "system.text"), []uint16{2, 3}, 3, 0),
			skeletonFixture(t0.Add(time.Minute), 1, []uint64{1, 2, 3, 4, 5, 101}, append(append([]string{}, followUp...), "system.text"), []uint16{2, 3, 4, 5}, 5, 0),
		}
		lane := AnalyzeCacheSkeletons(rows).Lanes[0]
		require.Len(t, lane.Turns, 2)
		assert.Equal(t, SkeletonReasonFirstTurn, lane.Turns[0].Reason)
		assert.True(t, lane.Turns[0].TailVolatile)

		turn := lane.Turns[1]
		assert.Equal(t, SkeletonReasonMisplaced, turn.Reason)
		assert.Equal(t, 3, turn.DivergeIdx)
		assert.Zero(t, turn.ExpectedCurrent)
		// Three of 5100 bytes are the stable prefix: 30000*3000/5100.
		assert.Equal(t, int64(17647), turn.ExpectedAdjusted)
		require.NotEmpty(t, turn.Losses)
		assert.Equal(t, SkeletonLossAmount{SkeletonReasonMisplaced, 17647, 1}, turn.Losses[0])
		assert.Equal(t, []SkeletonSegment{{State: "same", Bytes: 3000}, {State: "changed", Bytes: 1000}, {State: "new", Bytes: 1100}}, turn.Segments)
		assert.Equal(t, SkeletonRates{Total: 60000, Actual: 0, Current: 0, Adjusted: 17647, Adjusted1h: 17647}, lane.Summary)
	})

	t.Run("gap beyond 5m is ttl_expired and recovered by 1h", func(t *testing.T) {
		rows := []*CacheSkeleton{
			skeletonFixture(t0, 1, []uint64{1, 2, 3}, stable, []uint16{2}, 2, 0),
			skeletonFixture(t0.Add(10*time.Minute), 1, []uint64{1, 2, 3, 4, 5}, followUp, []uint16{2, 3, 4}, 4, 0),
		}
		turn := AnalyzeCacheSkeletons(rows).Lanes[0].Turns[1]
		assert.Equal(t, SkeletonReasonTTLExpired, turn.Reason)
		assert.InDelta(t, 590, turn.GapSeconds, 0.001)
		assert.Zero(t, turn.ExpectedCurrent)
		assert.Zero(t, turn.ExpectedAdjusted)
		assert.Equal(t, int64(18000), turn.ExpectedAdjusted1h)
	})

	t.Run("expected hit missed upstream after a channel switch", func(t *testing.T) {
		rows := []*CacheSkeleton{
			skeletonFixture(t0, 1, []uint64{1, 2, 3}, stable, []uint16{2}, 2, 0),
			skeletonFixture(t0.Add(time.Minute), 2, []uint64{1, 2, 3, 4, 5}, followUp, []uint16{2, 3, 4}, 4, 0),
			skeletonFixture(t0.Add(2*time.Minute), 2, []uint64{1, 2, 3, 4, 5, 6, 7}, append(append([]string{}, followUp...), "assistant.text", "user.text"), []uint16{2, 3, 4, 5, 6}, 6, 21428),
		}
		turns := AnalyzeCacheSkeletons(rows).Lanes[0].Turns
		assert.Equal(t, int64(18000), turns[1].ExpectedCurrent)
		assert.Equal(t, SkeletonReasonChannelSwitch, turns[1].Reason)
		assert.Equal(t, SkeletonReasonHit, turns[2].Reason)
	})

	t.Run("lanes are kept apart", func(t *testing.T) {
		main := skeletonFixture(t0, 1, []uint64{1, 2, 3}, stable, []uint16{2}, 2, 0)
		sub := skeletonFixture(t0.Add(time.Second), 1, []uint64{7, 8, 9}, stable, []uint16{2}, 2, 0)
		sub.Lane = 2
		analysis := AnalyzeCacheSkeletons([]*CacheSkeleton{main, sub})
		require.Len(t, analysis.Lanes, 2)
		assert.Equal(t, SkeletonReasonFirstTurn, analysis.Lanes[1].Turns[0].Reason)
		assert.Equal(t, int64(60000), analysis.Summary.Total)
	})
}

func openAISkeletonFixture(ts time.Time, cacheKey uint64, hashes []uint64, cacheRead uint32) *CacheSkeleton {
	kinds := []string{"tool", "instructions", "user.input_text", "assistant.function_call", "tool.function_call_output"}[:len(hashes)]
	row := skeletonFixture(ts, 1, hashes, kinds, []uint16{2}, 0, cacheRead)
	row.Provider = SkeletonProviderOpenAI
	row.Model = "gpt-5.5"
	row.CacheKeyHash = cacheKey
	row.BpIdx, row.BpTtl = nil, nil
	return row
}

func TestAnalyzeOpenAICacheSkeletons(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

	t.Run("matching prefix with the same key is expected to hit in 128-token steps", func(t *testing.T) {
		rows := []*CacheSkeleton{
			openAISkeletonFixture(t0, 7, []uint64{1, 2, 3}, 0),
			openAISkeletonFixture(t0.Add(time.Minute), 7, []uint64{1, 2, 3, 4, 5}, 17920),
		}
		lane := AnalyzeCacheSkeletons(rows).Lanes[0]
		assert.Equal(t, SkeletonProviderOpenAI, lane.Provider)
		turn := lane.Turns[1]
		// 30000 tokens * 3000/5000 bytes = 18000, rounded down to 17920.
		assert.Equal(t, int64(17920), turn.ExpectedCurrent)
		assert.Equal(t, SkeletonReasonHit, turn.Reason)
		assert.False(t, turn.CacheKeyChanged)
		assert.Empty(t, turn.Breakpoints)
	})

	t.Run("a new prompt_cache_key every turn is recovered by one key", func(t *testing.T) {
		rows := []*CacheSkeleton{
			openAISkeletonFixture(t0, 7, []uint64{1, 2, 3}, 0),
			openAISkeletonFixture(t0.Add(time.Minute), 8, []uint64{1, 2, 3, 4, 5}, 0),
		}
		turn := AnalyzeCacheSkeletons(rows).Lanes[0].Turns[1]
		assert.True(t, turn.CacheKeyChanged)
		assert.Zero(t, turn.ExpectedCurrent)
		assert.Equal(t, int64(17920), turn.ExpectedAdjusted)
		assert.Equal(t, SkeletonReasonCacheKeyChanged, turn.Reason)
	})

	t.Run("a gap beyond in-memory retention is recovered by 24h retention", func(t *testing.T) {
		rows := []*CacheSkeleton{
			openAISkeletonFixture(t0, 0, []uint64{1, 2, 3}, 0),
			openAISkeletonFixture(t0.Add(30*time.Minute), 0, []uint64{1, 2, 3, 4, 5}, 0),
		}
		turn := AnalyzeCacheSkeletons(rows).Lanes[0].Turns[1]
		assert.True(t, turn.NoCacheKey)
		assert.Zero(t, turn.ExpectedAdjusted)
		assert.Equal(t, int64(17920), turn.ExpectedAdjusted1h)
		assert.Equal(t, SkeletonReasonTTLExpired, turn.Reason)
	})

	t.Run("previous_response_id turns are counted as observed", func(t *testing.T) {
		follow := openAISkeletonFixture(t0.Add(time.Minute), 7, []uint64{9}, 20000)
		follow.ParentRequestId = "resp_1"
		lane := AnalyzeCacheSkeletons([]*CacheSkeleton{openAISkeletonFixture(t0, 7, []uint64{1, 2, 3}, 0), follow}).Lanes[0]
		assert.Equal(t, SkeletonReasonPreviousResponse, lane.Turns[1].Reason)
		assert.Equal(t, SkeletonRates{Total: 60000, Actual: 20000, Current: 20000, Adjusted: 20000, Adjusted1h: 20000}, lane.Summary)
	})
}

func TestClaudeCacheLossSummary(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(first, next *CacheSkeleton)
		want   []SkeletonLossAmount
	}{
		{
			name: "partial upstream miss excludes new input",
			change: func(_, next *CacheSkeleton) {
				next.CacheRead, next.Input = 10000, 20000
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonUpstreamMiss, 8000, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "small replay gaps are counted below the primary reason threshold",
			change: func(_, next *CacheSkeleton) {
				next.CacheRead, next.Input = 17500, 12500
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonUpstreamMiss, 500, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "channel switch is correlated with only the replay gap",
			change: func(_, next *CacheSkeleton) {
				next.ChannelId = 2
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonChannelSwitch, 18000, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "key rotation has the same correlation semantics",
			change: func(_, next *CacheSkeleton) {
				next.KeyIdx = 1
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonChannelSwitch, 18000, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "no breakpoint estimates reuse with a hypothetical 5m endpoint",
			change: func(first, next *CacheSkeleton) {
				first.BpIdx, next.BpIdx = nil, nil
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonNoBreakpoint, 18000, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "expired prefix within 1h is separated from new input",
			change: func(_, next *CacheSkeleton) {
				next.Ts, next.EndTs = t0.Add(10*time.Minute), t0.Add(10*time.Minute+time.Second)
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonTTLExpired, 18000, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "overlapping request is a possible pending write",
			change: func(_, next *CacheSkeleton) {
				next.Ts = t0.Add(5 * time.Second)
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonConcurrent, 18000, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "parameter change accounts only for old history",
			change: func(_, next *CacheSkeleton) {
				next.ParamFp++
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonParamChanged, 18000, 1}, {SkeletonReasonNewContent, 12000, 1},
			},
		},
		{
			name: "prefix change excludes stable prefix and new blocks",
			change: func(_, next *CacheSkeleton) {
				next.BlkHash[1] = 99
			},
			want: []SkeletonLossAmount{
				{SkeletonReasonPrefixChanged, 12000, 1}, {SkeletonReasonNewContent, 18000, 1},
			},
		},
		{
			name: "short input is structural rather than recoverable",
			change: func(_, next *CacheSkeleton) {
				next.Input = 500
			},
			want: []SkeletonLossAmount{{SkeletonReasonTooSmall, 500, 1}},
		},
		{
			name: "full observed hit overrides lower estimated ceilings",
			change: func(_, next *CacheSkeleton) {
				next.CacheRead, next.Input = 30000, 0
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := skeletonFixture(t0, 1, []uint64{1, 2, 3}, []string{"tool", "system.text", "user.text"}, []uint16{2}, 2, 0)
			next := skeletonFixture(t0.Add(time.Minute), 1, []uint64{1, 2, 3, 4, 5}, []string{"tool", "system.text", "user.text", "assistant.text", "user.text"}, []uint16{2, 3, 4}, 4, 0)
			tc.change(first, next)
			analysis := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next})
			require.Len(t, analysis.Lanes, 1)
			turns := analysis.Lanes[0].Turns
			require.Len(t, turns, 2)
			assert.Equal(t, tc.want, turns[1].Losses)
			assert.Equal(t, []SkeletonLossAmount{{SkeletonReasonFirstTurn, 30000, 1}}, turns[0].Losses)
			summary := analysis.LossSummary
			assert.Equal(t, int64(2), summary.AnalyzedTurns)
			assert.Equal(t, analysis.Summary.Total, summary.TotalTokens)
			assert.Equal(t, analysis.Summary.Actual, summary.ActualRead)
			var sum int64
			for _, item := range summary.Items {
				assert.Positive(t, item.Tokens)
				sum += item.Tokens
			}
			assert.Equal(t, summary.TotalTokens-summary.ActualRead, sum)
			assert.Equal(t, sum, summary.UncachedTokens)
			assert.Equal(t, summary, analysis.Lanes[0].LossSummary)
		})
	}
}

func TestClaudeCacheLossLookbackBoundary(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	for _, blocks := range []int{22, 23} {
		t.Run(strconv.Itoa(blocks)+" blocks", func(t *testing.T) {
			first := skeletonFixture(t0, 1, []uint64{1, 2, 3}, []string{"tool", "system.text", "user.text"}, []uint16{2}, 2, 0)
			hashes := make([]uint64, blocks)
			kinds := make([]string, blocks)
			for i := range hashes {
				hashes[i], kinds[i] = uint64(i+1), "user.text"
			}
			next := skeletonFixture(t0.Add(time.Minute), 1, hashes, kinds, []uint16{2}, uint16(blocks-1), 0)
			turn := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next}).Lanes[0].Turns[1]
			prefixTokens := int64(30000 * 3 / blocks)
			reason := SkeletonReasonUpstreamMiss
			if blocks == 23 {
				reason = SkeletonReasonLookback
				assert.Zero(t, turn.ExpectedCurrent)
			} else {
				assert.Equal(t, prefixTokens, turn.ExpectedCurrent)
			}
			assert.Equal(t, []SkeletonLossAmount{
				{reason, prefixTokens, 1}, {SkeletonReasonNewContent, 30000 - prefixTokens, 1},
			}, turn.Losses)
		})
	}
}

func TestClaudeCacheLossExcludesErrorsAndOpenAI(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	bad := skeletonFixture(t0, 1, []uint64{1, 2, 3}, []string{"tool", "system.text", "user.text"}, []uint16{2}, 2, 0)
	bad.Status = "error"
	openAI := openAISkeletonFixture(t0, 7, []uint64{1, 2, 3}, 0)
	openAI.Lane = 2
	analysis := AnalyzeCacheSkeletons([]*CacheSkeleton{bad, openAI})
	assert.Zero(t, analysis.LossSummary.AnalyzedTurns)
	assert.Zero(t, analysis.LossSummary.TotalTokens)
	assert.Empty(t, analysis.LossSummary.Items)
	assert.Equal(t, int64(30000), analysis.Summary.Total)
}

func TestClaudeCacheLossAggregatesLanes(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	first := skeletonFixture(t0, 1, []uint64{1, 2, 3}, []string{"tool", "system.text", "user.text"}, []uint16{2}, 2, 0)
	next := skeletonFixture(t0.Add(time.Minute), 1, []uint64{1, 2, 3, 4, 5}, []string{"tool", "system.text", "user.text", "assistant.text", "user.text"}, []uint16{2, 3, 4}, 4, 0)
	other := skeletonFixture(t0, 1, []uint64{6, 7, 8}, []string{"tool", "system.text", "user.text"}, []uint16{2}, 2, 0)
	other.Lane = 2
	analysis := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next, other})
	require.Len(t, analysis.Lanes, 2)
	assert.Equal(t, SkeletonLossSummary{
		AnalyzedTurns: 3, TotalTokens: 90000, UncachedTokens: 90000,
		Items: []SkeletonLossAmount{
			{SkeletonReasonFirstTurn, 60000, 2},
			{SkeletonReasonUpstreamMiss, 18000, 1},
			{SkeletonReasonNewContent, 12000, 1},
		},
	}, analysis.LossSummary)
}
