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
// TEST_CLICKHOUSE_DSN at an isolated scratch database, e.g.
// clickhouse://default:pwd@127.0.0.1:9000/skeleton_test.
// Each scenario creates and drops cache_skeletons; an existing table is refused.
func TestCacheSkeletonClickHouseRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TEST_CLICKHOUSE_DSN is not configured")
	}
	db, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	var version string
	require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Logf("ClickHouse version: %s", version)
	require.False(t, db.Migrator().HasTable(&CacheSkeleton{}), "use an isolated empty test database")
	t.Setenv("CACHE_SKELETON_STORAGE_POLICY", "")
	t.Setenv("CACHE_SKELETON_TTL_DAYS", "14")
	prevDB, prevType := LOG_DB, common.LogDatabaseType()
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	t.Cleanup(func() {
		LOG_DB = prevDB
		common.SetLogDatabaseType(prevType)
	})

	for _, scenario := range []string{"fresh", "upgrade"} {
		t.Run(scenario, func(t *testing.T) {
			require.False(t, db.Migrator().HasTable(&CacheSkeleton{}))
			t.Cleanup(func() {
				assert.NoError(t, db.Exec("DROP TABLE IF EXISTS cache_skeletons SYNC").Error)
			})
			now := time.Now().UTC().Truncate(time.Millisecond)
			if scenario == "upgrade" {
				// Explicit pre-coverage schema: the row must exist before ADD COLUMN
				// so defaults are tested on old parts, not just on new inserts.
				require.NoError(t, db.Exec(`CREATE TABLE cache_skeletons (
					ts DateTime64(3), end_ts DateTime64(3), request_id String, attempt UInt8,
					status LowCardinality(String), node LowCardinality(String),
					token_id UInt32, session_id String, lane UInt64,
					channel_id UInt32, key_idx UInt16, model LowCardinality(String),
					param_fp UInt64, thinking LowCardinality(String), effort LowCardinality(String),
					provider LowCardinality(String) DEFAULT 'claude',
					cache_key_hash UInt64 DEFAULT 0, retention LowCardinality(String) DEFAULT '',
					blk_hash Array(UInt64), blk_kind Array(LowCardinality(String)),
					blk_name Array(LowCardinality(String)), blk_len Array(UInt32),
					msg_start Array(UInt16), bp_idx Array(UInt16), bp_ttl Array(LowCardinality(String)),
					fixed LowCardinality(String), cache_read UInt32, cache_creation UInt32, input UInt32,
					parent_request_id String, prefix_len UInt16
				) ENGINE = MergeTree()
				PARTITION BY toYYYYMMDD(ts)
				ORDER BY (token_id, session_id, lane, ts)`).Error)
				require.NoError(t, db.Exec(`INSERT INTO cache_skeletons
					(ts, end_ts, request_id, attempt, status, token_id, session_id,
					 lane, param_fp, blk_hash, blk_len, bp_idx, bp_ttl)
					VALUES (?, ?, 'legacy', 2, 'ok', 7, 'legacy-session',
					 9223372036854775813, 9223372036854775817,
					 [9223372036854775809, 2], [100, 200], [1], ['1h'])`,
					now.Format("2006-01-02 15:04:05.000"), now.Format("2006-01-02 15:04:05.000")).Error)
			}
			// Startup migration must work both initially and on repeat.
			require.NoError(t, MigrateCacheSkeletons())
			require.NoError(t, MigrateCacheSkeletons())

			requestId := "skeleton-test-" + scenario
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
					CoverageHash:  []uint64{1<<63 + 11, 0, 1<<64 - 1},
					CoverageLen:   []uint32{100, 0, 1<<32 - 1},
					CoverageBpIdx: []uint16{0, 2}, CoverageBpTtl: []string{"5m", "1h"},
					BaselineHash:  []uint64{1<<64 - 1, 1<<63 + 17},
					BaselineLen:   []uint32{300, 400},
					BaselineBpIdx: []uint16{1}, BaselineBpTtl: []string{""},
					BaselineParamFp: 1<<63 + 23,
				},
				{
					Ts: now, EndTs: now, RequestId: requestId, Attempt: 0, Status: "error",
					BlkHash: []uint64{}, BlkKind: []string{}, BlkName: []string{}, BlkLen: []uint32{},
					MsgStart: []uint16{}, BpIdx: []uint16{}, BpTtl: []string{},
					CoverageHash: []uint64{}, CoverageLen: []uint32{},
					CoverageBpIdx: []uint16{}, CoverageBpTtl: []string{},
					BaselineHash: []uint64{}, BaselineLen: []uint32{},
					BaselineBpIdx: []uint16{}, BaselineBpTtl: []string{},
				},
			}
			require.NoError(t, LOG_DB.Create(&batch).Error)
			// Repeating migration must preserve populated arrays as well as old rows.
			require.NoError(t, MigrateCacheSkeletons())

			rows, err := GetCacheSkeletons(requestId)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			assert.Equal(t, uint8(0), rows[0].Attempt)
			assert.Equal(t, "error", rows[0].Status)
			assert.Empty(t, rows[0].BlkHash)
			emptyRows := []*CacheSkeleton{rows[0]}
			if scenario == "upgrade" {
				legacy, err := GetCacheSkeletons("legacy")
				require.NoError(t, err)
				require.Len(t, legacy, 1)
				old := legacy[0]
				assert.Equal(t, uint8(2), old.Attempt)
				assert.Equal(t, "ok", old.Status)
				assert.Equal(t, uint32(7), old.TokenId)
				assert.Equal(t, "legacy-session", old.SessionId)
				assert.Equal(t, uint64(1<<63+5), old.Lane)
				assert.Equal(t, uint64(1<<63+9), old.ParamFp)
				assert.Equal(t, []uint64{1<<63 + 1, 2}, old.BlkHash)
				assert.Equal(t, []uint32{100, 200}, old.BlkLen)
				assert.Equal(t, []uint16{1}, old.BpIdx)
				assert.Equal(t, []string{"1h"}, old.BpTtl)
				assert.True(t, now.Equal(old.Ts.UTC()))
				emptyRows = append(emptyRows, old)
			}
			for _, empty := range emptyRows {
				assert.Empty(t, empty.CoverageHash)
				assert.Empty(t, empty.CoverageLen)
				assert.Empty(t, empty.CoverageBpIdx)
				assert.Empty(t, empty.CoverageBpTtl)
				assert.Empty(t, empty.BaselineHash)
				assert.Empty(t, empty.BaselineLen)
				assert.Empty(t, empty.BaselineBpIdx)
				assert.Empty(t, empty.BaselineBpTtl)
				assert.Zero(t, empty.BaselineParamFp)
			}

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
			assert.Equal(t, batch[0].CoverageHash, got.CoverageHash)
			assert.Equal(t, batch[0].CoverageLen, got.CoverageLen)
			assert.Equal(t, batch[0].CoverageBpIdx, got.CoverageBpIdx)
			assert.Equal(t, batch[0].CoverageBpTtl, got.CoverageBpTtl)
			assert.Equal(t, batch[0].BaselineHash, got.BaselineHash)
			assert.Equal(t, batch[0].BaselineLen, got.BaselineLen)
			assert.Equal(t, batch[0].BaselineBpIdx, got.BaselineBpIdx)
			assert.Equal(t, batch[0].BaselineBpTtl, got.BaselineBpTtl)
			assert.Equal(t, batch[0].BaselineParamFp, got.BaselineParamFp)
		})
	}
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
		assert.Equal(t, SkeletonRates{Total: 60000, Actual: 0, Current: 0, Adjusted: 17647, Adjusted1h: 17647, Advanced: 17647, Advanced1h: 17647}, lane.Summary)
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
			name: "previous turn without a breakpoint is not a lookback loss",
			change: func(first, _ *CacheSkeleton) {
				first.BpIdx, first.BpTtl = nil, nil
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
				// Block 0 is stable, but no breakpoint wrote its prefix.
				{SkeletonReasonStableUnexplained, 6000, 1}, {SkeletonReasonPrefixChanged, 12000, 1}, {SkeletonReasonNewContent, 12000, 1},
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
				assert.LessOrEqual(t, item.Turns, summary.AnalyzedTurns)
				sum += item.Tokens
			}
			assert.Equal(t, summary.TotalTokens-summary.ActualRead, sum)
			assert.Equal(t, sum, summary.UncachedTokens)
			assert.Equal(t, summary, analysis.Lanes[0].LossSummary)
		})
	}
}

func TestClaudeCacheLossMergesRepeatedReasons(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	var rows []*CacheSkeleton
	for i, n := range []int{25, 3, 27} {
		hashes, kinds := make([]uint64, n), make([]string, n)
		for j := range hashes {
			hashes[j], kinds[j] = uint64(j+1), "user.text"
		}
		row := skeletonFixture(t0.Add(time.Duration(i)*10*time.Minute), 1, hashes, kinds, []uint16{0}, 1, 0)
		row.Input, row.BpTtl = uint32(n*1000), []string{"5m"}
		rows = append(rows, row)
	}
	analysis := AnalyzeCacheSkeletons(rows)
	require.Len(t, analysis.Lanes, 1)
	lane := analysis.Lanes[0]
	require.Len(t, lane.Turns, 3)
	assert.Equal(t, []SkeletonLossAmount{
		{SkeletonReasonTTLExpired, 24000, 1},
		{SkeletonReasonStalled, 1000, 1},
		{SkeletonReasonNewContent, 2000, 1},
	}, lane.Turns[2].Losses, "merge TTL segments in first-appearance order")
	for _, turn := range lane.Turns {
		seen := map[string]bool{}
		var sum int64
		for _, item := range turn.Losses {
			assert.False(t, seen[item.Reason], "duplicate reason %s", item.Reason)
			seen[item.Reason] = true
			assert.Equal(t, int64(1), item.Turns)
			sum += item.Tokens
		}
		assert.Equal(t, turn.TotalTokens-turn.ActualRead, sum)
	}
	assert.Equal(t, []SkeletonLossAmount{
		{SkeletonReasonFirstTurn, 25000, 1},
		{SkeletonReasonTTLExpired, 26000, 2},
		{SkeletonReasonStalled, 2000, 2},
		{SkeletonReasonNewContent, 2000, 1},
	}, analysis.LossSummary.Items)
	assert.Equal(t, lane.LossSummary, analysis.LossSummary)
	var sum int64
	for _, item := range analysis.LossSummary.Items {
		assert.LessOrEqual(t, item.Turns, analysis.LossSummary.AnalyzedTurns)
		sum += item.Tokens
	}
	assert.Equal(t, analysis.LossSummary.UncachedTokens, sum)
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

// TestClaudeCacheLossStablePrefixSurvivesExhaustedSimulatorCeilings covers a
// turn where every simulated-hit ceiling (current/adjusted/adjusted1h/lookback)
// is exhausted (the gap exceeds both the lookback window and the 1h TTL), yet
// the hash comparison proves the old-history prefix is byte-identical to the
// previous turn. Repeated bytes alone do not establish a live written cache
// entry. Preserve the distinction from new input without blaming upstream.
func TestClaudeCacheLossStablePrefixSurvivesExhaustedSimulatorCeilings(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	first := skeletonFixture(t0, 1, []uint64{1, 2, 3}, []string{"tool", "system.text", "user.text"}, []uint16{2}, 2, 0)
	next := skeletonFixture(t0.Add(2*time.Hour), 1, []uint64{1, 2, 3, 4, 5}, []string{"tool", "system.text", "user.text", "assistant.text", "user.text"}, []uint16{2, 3, 4}, 4, 0)
	analysis := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next})
	require.Len(t, analysis.Lanes, 1)
	turns := analysis.Lanes[0].Turns
	require.Len(t, turns, 2)

	turn := turns[1]
	assert.Zero(t, turn.ExpectedCurrent, "the 2h gap exceeds the 1h adjusted1h ceiling too")
	assert.Equal(t, -1, turn.DivergeIdx, "the old-history prefix (blocks 0-2) is byte-identical")
	// All simulated entries expired: there is no evidence of an upstream miss.
	assert.Equal(t, []SkeletonLossAmount{
		{SkeletonReasonStableUnexplained, 18000, 1}, {SkeletonReasonNewContent, 12000, 1},
	}, turn.Losses)
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

func TestClaudeRollingBreakpointRegression(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                 string
		gap                  time.Duration
		rolling              bool
		read                 uint32
		advanced, advanced1h int64
	}{
		{"fixed", time.Minute, false, 0, 25000, 25000},
		{"fixed 5m expired", 10 * time.Minute, false, 0, 0, 25000},
		{"fixed expired", 2 * time.Hour, false, 0, 0, 0},
		{"observed read exceeds simulations", 2 * time.Hour, false, 20000, 0, 0},
		{"rolling", time.Minute, true, 0, 25000, 25000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rows []*CacheSkeleton
			for i, n := range []int{3, 25, 47} {
				hashes, kinds := make([]uint64, n), make([]string, n)
				for j := range hashes {
					hashes[j], kinds[j] = uint64(j+1), "user.text"
				}
				bp := uint16(1)
				if tc.rolling {
					bp = uint16(n - 1)
				}
				row := skeletonFixture(t0.Add(time.Duration(i)*tc.gap), 1, hashes, kinds, []uint16{0}, bp, 0)
				// Constant token density makes every expected amount exact.
				row.Input = uint32(n * 1000)
				if i == 2 {
					row.CacheRead, row.Input = tc.read, row.Input-tc.read
				}
				rows = append(rows, row)
			}
			analysis := AnalyzeCacheSkeletons(rows)
			require.Len(t, analysis.Lanes, 1)
			turn := analysis.Lanes[0].Turns[2]
			assert.Equal(t, tc.advanced, turn.ExpectedAdvanced)
			assert.Equal(t, tc.advanced1h, turn.ExpectedAdvanced1h)
			assert.Equal(t, analysis.Lanes[0].Summary.Advanced, analysis.Summary.Advanced)
			assert.Equal(t, analysis.Lanes[0].Summary.Advanced1h, analysis.Summary.Advanced1h)
			assert.GreaterOrEqual(t, analysis.Summary.Advanced, analysis.Summary.Actual)
			amounts := map[string]int64{}
			var sum int64
			for _, item := range turn.Losses {
				amounts[item.Reason] += item.Tokens
				sum += item.Tokens
			}
			assert.Equal(t, turn.TotalTokens-turn.ActualRead, sum)
			if tc.rolling {
				assert.Zero(t, amounts["breakpoint_stalled"], "a moved endpoint is a lookback issue, not stalled coverage")
				assert.Zero(t, amounts[SkeletonReasonNoBreakpoint], "normal rolling gains must not fall through to missing breakpoints")
			} else {
				assert.Equal(t, int64(25000)-max(2000, int64(tc.read)), amounts["breakpoint_stalled"])
				assert.Equal(t, max(0, turn.ExpectedCurrent-turn.ActualRead), amounts[SkeletonReasonUpstreamMiss])
				assert.Equal(t, "breakpoint_stalled", turn.Reason)
			}
		})
	}
}

func TestClaudeAdvancedPolicyConstraints(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(first, next *CacheSkeleton)
		want   int64
	}{
		{"unchanged", func(_, _ *CacheSkeleton) {}, 20000},
		{"parameters", func(_, next *CacheSkeleton) { next.ParamFp++ }, 0},
		{"changed before endpoint", func(_, next *CacheSkeleton) { next.BlkHash[3] = 99 }, 5000},
		{"pending write", func(first, _ *CacheSkeleton) { first.EndTs = t0.Add(2 * time.Minute) }, 0},
		{"minimum write length", func(first, _ *CacheSkeleton) { first.Input = 500 }, 0},
		{"original one hour ttl", func(first, next *CacheSkeleton) {
			first.BpTtl, next.BpTtl = []string{"1h"}, []string{"1h"}
			next.Ts, next.EndTs = t0.Add(10*time.Minute), t0.Add(10*time.Minute+time.Second)
		}, 20000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := skeletonFixture(t0, 1, []uint64{1, 2, 3, 4}, []string{"tool", "system.text", "user.text", "assistant.text"}, []uint16{2, 3}, 0, 0)
			next := skeletonFixture(t0.Add(time.Minute), 1, []uint64{1, 2, 3, 4, 5, 6}, []string{"tool", "system.text", "user.text", "assistant.text", "user.text", "assistant.text"}, []uint16{2, 3, 4, 5}, 0, 0)
			tc.change(first, next)
			// Future divergence must not change the preceding turn's writes.
			before := AnalyzeCacheSkeletons([]*CacheSkeleton{first}).Lanes[0].Turns[0]
			analysis := AnalyzeCacheSkeletons([]*CacheSkeleton{first, next})
			assert.Equal(t, before, analysis.Lanes[0].Turns[0])
			assert.Equal(t, tc.want, analysis.Lanes[0].Turns[1].ExpectedAdvanced)
			assert.Equal(t, tc.want, analysis.Lanes[0].Turns[1].ExpectedAdvanced1h)
		})
	}

	t.Run("four slots exclude thinking empty and volatile system blocks", func(t *testing.T) {
		row := skeletonFixture(t0, 1, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9},
			[]string{"tool", "system.text", "user.text", "assistant.text", "user.text", "assistant.thinking", "assistant.redacted_thinking", "assistant.text", "system.text"},
			[]uint16{2, 3, 4, 5, 8}, 8, 0)
		row.BlkLen[7] = 0
		row.BpIdx = []uint16{0, 1, 2, 8}
		row.BpTtl = []string{"1h", "1h", "5m", "5m"}
		bps := rollingSkeletonBreakpoints(row, nil, nil)
		require.Len(t, bps, 4)
		assert.Equal(t, []skeletonBreakpoint{
			{0, time.Hour}, {1, time.Hour}, {2, 5 * time.Minute}, {4, 5 * time.Minute},
		}, bps)
		next := *row
		next.Ts, next.EndTs = t0.Add(time.Minute), t0.Add(time.Minute+time.Second)
		next.BlkHash = append([]uint64{}, row.BlkHash...)
		next.BlkHash[5], next.BlkHash[8] = 100, 101
		turn := AnalyzeCacheSkeletons([]*CacheSkeleton{row, &next}).Lanes[0].Turns[1]
		// Seven nonempty 1000-byte blocks plus the 100-byte tail.
		assert.Equal(t, int64(21126), turn.ExpectedAdvanced)
	})

	t.Run("future completion cannot refresh an expired entry", func(t *testing.T) {
		var rows []*CacheSkeleton
		for _, gap := range []time.Duration{0, 4 * time.Minute, 10 * time.Minute, 21 * time.Minute} {
			rows = append(rows, skeletonFixture(t0.Add(gap), 1, []uint64{1, 2, 3},
				[]string{"tool", "system.text", "user.text"}, []uint16{2}, 0, 0))
		}
		rows[1].EndTs = t0.Add(20 * time.Minute)
		turns := AnalyzeCacheSkeletons(rows).Lanes[0].Turns
		assert.Equal(t, int64(30000), turns[1].ExpectedAdvanced)
		assert.Zero(t, turns[2].ExpectedAdvanced, "the 4m read refreshed only until 9m")
		assert.Equal(t, int64(30000), turns[2].ExpectedAdvanced1h)
		assert.Equal(t, int64(30000), turns[3].ExpectedAdvanced, "the write completed at 20m")
	})
}
