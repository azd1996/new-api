package model

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
)

// Turn reasons, first match wins (see classifySkeletonTurn).
const (
	SkeletonReasonError         = "error"
	SkeletonReasonFirstTurn     = "first_turn"
	SkeletonReasonTooSmall      = "too_small"
	SkeletonReasonNoBreakpoint  = "no_breakpoint"
	SkeletonReasonParamChanged  = "param_changed"
	SkeletonReasonPrefixChanged = "prefix_changed"
	SkeletonReasonConcurrent    = "concurrent"
	SkeletonReasonTTLExpired    = "ttl_expired"
	SkeletonReasonMisplaced     = "breakpoint_misplaced"
	SkeletonReasonChannelSwitch = "channel_switch"
	SkeletonReasonUpstreamMiss  = "upstream_miss"
	SkeletonReasonHit           = "hit"
	// OpenAI only: a changed prompt_cache_key routes the turn away from the
	// machine holding its prefix; previous_response_id hides the prefix.
	SkeletonReasonCacheKeyChanged  = "cache_key_changed"
	SkeletonReasonPreviousResponse = "previous_response"
)

const (
	// skeletonLookback mirrors Anthropic's cache read lookback: a breakpoint
	// also matches entries written up to ~20 blocks before it.
	skeletonLookback       = 20
	skeletonMaxBreakpoints = 4
	// A loss below this share of the turn's input is treated as noise.
	skeletonLossThreshold = 0.05
	// skeletonSessionRowLimit caps one session analysis.
	skeletonSessionRowLimit = 2000

	// OpenAI caches prompts of at least 1024 tokens and reports hits in
	// 128-token steps. In-memory entries are dropped after 5-10 minutes of
	// inactivity (the upper bound is used); "24h" retention keeps them a day.
	openaiCacheMinTokens   = 1024
	openaiCacheGranularity = 128
	openaiInMemoryTTL      = 10 * time.Minute
	openaiExtendedTTL      = 24 * time.Hour
)

// SkeletonSegment is a run of blocks in one turn compared with the previous
// turn of its lane: "same" (unchanged prefix), "changed" (diverged from the
// previous turn) or "new" (beyond the previous turn's length).
type SkeletonSegment struct {
	State string `json:"state"`
	Bytes uint64 `json:"bytes"`
}

type SkeletonBreakpointMark struct {
	Offset uint64 `json:"offset"`
	TTL    string `json:"ttl"`
}

// SkeletonTurn is one recorded attempt with its simulated cache outcomes.
// Expected* are token estimates: the turn's real input total split across
// blocks by byte share. For Claude lanes ExpectedCurrent / ExpectedAdjusted /
// ExpectedAdjusted1h are the ceilings with the client's breakpoints, with the
// gateway breakpoint adjustment and with the adjustment plus a 1h TTL; for
// OpenAI lanes they are the prefix-match ceiling, the ceiling with one
// prompt_cache_key for the whole lane and that plus 24h retention.
type SkeletonTurn struct {
	RequestId          string                   `json:"request_id"`
	Ts                 time.Time                `json:"ts"`
	Status             string                   `json:"status"`
	Attempt            uint8                    `json:"attempt"`
	ChannelId          uint32                   `json:"channel_id"`
	KeyIdx             uint16                   `json:"key_idx"`
	GapSeconds         float64                  `json:"gap_s"`
	TotalTokens        int64                    `json:"total_tokens"`
	ActualRead         int64                    `json:"actual_read"`
	ExpectedCurrent    int64                    `json:"expected_current"`
	ExpectedAdjusted   int64                    `json:"expected_adjusted"`
	ExpectedAdjusted1h int64                    `json:"expected_adjusted_1h"`
	Reason             string                   `json:"reason"`
	DivergeIdx         int                      `json:"diverge_idx"`
	TailVolatile       bool                     `json:"tail_volatile"`
	CacheKeyChanged    bool                     `json:"cache_key_changed"`
	NoCacheKey         bool                     `json:"no_cache_key"`
	TotalBytes         uint64                   `json:"total_bytes"`
	Segments           []SkeletonSegment        `json:"segments"`
	Breakpoints        []SkeletonBreakpointMark `json:"breakpoints"`
}

// SkeletonRates sums tokens of successful turns: Actual is what upstream
// read from cache; Current/Adjusted/Adjusted1h are the three simulated
// ceilings described on SkeletonTurn.
type SkeletonRates struct {
	Total      int64 `json:"total"`
	Actual     int64 `json:"actual"`
	Current    int64 `json:"current"`
	Adjusted   int64 `json:"adjusted"`
	Adjusted1h int64 `json:"adjusted_1h"`
}

func (r *SkeletonRates) add(o SkeletonRates) {
	r.Total += o.Total
	r.Actual += o.Actual
	r.Current += o.Current
	r.Adjusted += o.Adjusted
	r.Adjusted1h += o.Adjusted1h
}

type SkeletonLane struct {
	Lane     string         `json:"lane"`
	Model    string         `json:"model"`
	Provider string         `json:"provider"`
	Turns    []SkeletonTurn `json:"turns"`
	Summary  SkeletonRates  `json:"summary"`
}

type SessionCacheAnalysis struct {
	Lanes     []SkeletonLane `json:"lanes"`
	Summary   SkeletonRates  `json:"summary"`
	Truncated bool           `json:"truncated"`
}

// GetSessionCacheAnalysis loads a session's skeletons and simulates them.
func GetSessionCacheAnalysis(sessionId string) (*SessionCacheAnalysis, error) {
	rows := make([]*CacheSkeleton, 0)
	err := LOG_DB.Where("session_id = ?", sessionId).Order("ts ASC").Limit(skeletonSessionRowLimit + 1).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	truncated := len(rows) > skeletonSessionRowLimit
	if truncated {
		rows = rows[:skeletonSessionRowLimit]
	}
	analysis := AnalyzeCacheSkeletons(rows)
	analysis.Truncated = truncated
	return analysis, nil
}

// AnalyzeCacheSkeletons groups time-ordered skeletons by lane and replays
// each lane through four cache simulations.
func AnalyzeCacheSkeletons(rows []*CacheSkeleton) *SessionCacheAnalysis {
	result := &SessionCacheAnalysis{Lanes: []SkeletonLane{}}
	laneIndex := map[uint64]int{}
	laneRows := [][]*CacheSkeleton{}
	for _, row := range rows {
		i, ok := laneIndex[row.Lane]
		if !ok {
			i = len(laneRows)
			laneIndex[row.Lane] = i
			laneRows = append(laneRows, nil)
		}
		laneRows[i] = append(laneRows[i], row)
	}
	for _, lr := range laneRows {
		lane := analyzeSkeletonLane(lr)
		result.Summary.add(lane.Summary)
		result.Lanes = append(result.Lanes, lane)
	}
	return result
}

type skeletonBreakpoint struct {
	idx int
	ttl time.Duration
}

type skeletonCacheEntry struct {
	availableAt time.Time
	expireAt    time.Time
	ttl         time.Duration
}

// skeletonCacheSim models the upstream prompt cache for one lane: entries
// keyed by the rolling prefix hash at a cache point, readable once the writing
// request finished and until its TTL lapses (refreshed on every read). A read
// at a breakpoint also matches entries up to lookback blocks before it.
type skeletonCacheSim struct {
	entries   map[uint64]*skeletonCacheEntry
	ignoreTTL bool
	lookback  int
}

func newSkeletonCacheSim(ignoreTTL bool, lookback int) *skeletonCacheSim {
	return &skeletonCacheSim{entries: map[uint64]*skeletonCacheEntry{}, ignoreTTL: ignoreTTL, lookback: lookback}
}

// lookup returns the cumulative tokens of the longest cached prefix reachable
// from any breakpoint.
func (s *skeletonCacheSim) lookup(prefix []uint64, cumTokens []int64, bps []skeletonBreakpoint, at time.Time) int64 {
	var best int64
	for _, bp := range bps {
		for k := bp.idx; k >= 0 && k >= bp.idx-s.lookback; k-- {
			e := s.entries[prefix[k]]
			if e == nil || at.Before(e.availableAt) || (!s.ignoreTTL && at.After(e.expireAt)) {
				continue
			}
			e.expireAt = maxTime(e.expireAt, at.Add(e.ttl))
			best = max(best, cumTokens[k])
			break
		}
	}
	return best
}

func (s *skeletonCacheSim) write(prefix []uint64, cumTokens []int64, bps []skeletonBreakpoint, end time.Time, minTokens int64) {
	for _, bp := range bps {
		if cumTokens[bp.idx] < minTokens {
			continue
		}
		key := prefix[bp.idx]
		if e := s.entries[key]; e != nil {
			e.expireAt = maxTime(e.expireAt, end.Add(bp.ttl))
			e.ttl = max(e.ttl, bp.ttl)
			continue
		}
		s.entries[key] = &skeletonCacheEntry{availableAt: end, expireAt: end.Add(bp.ttl), ttl: bp.ttl}
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func parseSkeletonTTL(ttl string) time.Duration {
	if ttl == "1h" {
		return time.Hour
	}
	return 5 * time.Minute
}

// skeletonPrefixHashes chains block hashes from seed so equal values mean an
// identical prefix up to that block under the same rendering parameters (and,
// for OpenAI, the same cache routing key when it is folded into seed).
func skeletonPrefixHashes(row *CacheSkeleton, seed uint64) []uint64 {
	out := make([]uint64, len(row.BlkHash))
	prev := seed
	var buf [16]byte
	for i, h := range row.BlkHash {
		binary.LittleEndian.PutUint64(buf[:8], prev)
		binary.LittleEndian.PutUint64(buf[8:], h)
		prev = xxhash.Sum64(buf[:])
		out[i] = prev
	}
	return out
}

func skeletonBreakpoints(row *CacheSkeleton) []skeletonBreakpoint {
	bps := make([]skeletonBreakpoint, 0, len(row.BpIdx))
	for i, idx := range row.BpIdx {
		if int(idx) >= len(row.BlkHash) {
			continue
		}
		ttl := ""
		if i < len(row.BpTtl) {
			ttl = row.BpTtl[i]
		}
		bps = append(bps, skeletonBreakpoint{idx: int(idx), ttl: parseSkeletonTTL(ttl)})
	}
	return bps
}

// skeletonTailVolatile reports a breakpoint on the last message when that
// message is a mid-conversation system message (e.g. a <total_tokens>
// reminder that the next turn moves or rewrites).
func skeletonTailVolatile(row *CacheSkeleton, bps []skeletonBreakpoint) bool {
	if len(row.MsgStart) == 0 || len(bps) == 0 {
		return false
	}
	last := int(row.MsgStart[len(row.MsgStart)-1])
	if last >= len(row.BlkKind) || !strings.HasPrefix(row.BlkKind[last], "system.") {
		return false
	}
	return bps[len(bps)-1].idx >= last
}

// adjustSkeletonBreakpoints simulates the gateway breakpoint adjustment: a
// breakpoint on a volatile trailing system message gains one on the last
// cacheable block before it (moved instead when all four slots are used).
func adjustSkeletonBreakpoints(row *CacheSkeleton, bps []skeletonBreakpoint) []skeletonBreakpoint {
	if !skeletonTailVolatile(row, bps) {
		return bps
	}
	target := int(row.MsgStart[len(row.MsgStart)-1]) - 1
	for target >= 0 && (strings.HasSuffix(row.BlkKind[target], ".thinking") || strings.HasSuffix(row.BlkKind[target], ".redacted_thinking")) {
		target--
	}
	if target < 0 {
		return bps
	}
	for _, bp := range bps {
		if bp.idx == target {
			return bps
		}
	}
	tail := bps[len(bps)-1]
	out := append([]skeletonBreakpoint{}, bps...)
	if len(out) >= skeletonMaxBreakpoints {
		out[len(out)-1] = skeletonBreakpoint{idx: target, ttl: tail.ttl}
		return out
	}
	out = append(out[:len(out)-1], skeletonBreakpoint{idx: target, ttl: tail.ttl}, tail)
	return out
}

func withSkeletonTTL(bps []skeletonBreakpoint, ttl time.Duration) []skeletonBreakpoint {
	out := make([]skeletonBreakpoint, len(bps))
	for i, bp := range bps {
		out[i] = skeletonBreakpoint{idx: bp.idx, ttl: ttl}
	}
	return out
}

// skeletonExpectation is one turn's simulated hit tokens under the three
// ceilings, plus the loss each step explains (largest one becomes the reason).
type skeletonExpectation struct {
	current, adjusted, adjusted1h int64
	losses                        []skeletonLoss
}

type skeletonLoss struct {
	reason string
	loss   int64
}

// skeletonLaneSim replays the turns of one lane through a provider's cache
// model. Calls are in time order; replay both reads and writes the cache.
type skeletonLaneSim interface {
	replay(row *CacheSkeleton, cumTokens []int64) skeletonExpectation
}

// claudeLaneSim follows Anthropic's explicit breakpoints: client breakpoints
// as-is, ignoring TTL (to size the TTL loss), with the gateway adjustment, and
// with the adjustment at a 1h TTL.
type claudeLaneSim struct {
	minTokens                            int64
	current, noTTL, adjusted, adjusted1h *skeletonCacheSim
}

func newClaudeLaneSim(modelName string) *claudeLaneSim {
	return &claudeLaneSim{
		minTokens:  int64(cacheMinTokensFor(modelName)),
		current:    newSkeletonCacheSim(false, skeletonLookback),
		noTTL:      newSkeletonCacheSim(true, skeletonLookback),
		adjusted:   newSkeletonCacheSim(false, skeletonLookback),
		adjusted1h: newSkeletonCacheSim(false, skeletonLookback),
	}
}

func (s *claudeLaneSim) replay(row *CacheSkeleton, cumTokens []int64) skeletonExpectation {
	prefix := skeletonPrefixHashes(row, row.ParamFp)
	bps := skeletonBreakpoints(row)
	adjusted := adjustSkeletonBreakpoints(row, bps)
	adjusted1h := withSkeletonTTL(adjusted, time.Hour)
	var e skeletonExpectation
	e.current = s.current.lookup(prefix, cumTokens, bps, row.Ts)
	noTTL := s.noTTL.lookup(prefix, cumTokens, bps, row.Ts)
	e.adjusted = max(s.adjusted.lookup(prefix, cumTokens, adjusted, row.Ts), e.current)
	e.adjusted1h = max(s.adjusted1h.lookup(prefix, cumTokens, adjusted1h, row.Ts), e.adjusted)
	s.current.write(prefix, cumTokens, bps, row.EndTs, s.minTokens)
	s.noTTL.write(prefix, cumTokens, bps, row.EndTs, s.minTokens)
	s.adjusted.write(prefix, cumTokens, adjusted, row.EndTs, s.minTokens)
	s.adjusted1h.write(prefix, cumTokens, adjusted1h, row.EndTs, s.minTokens)
	e.losses = []skeletonLoss{
		{SkeletonReasonTTLExpired, noTTL - e.current},
		{SkeletonReasonMisplaced, e.adjusted - e.current},
	}
	return e
}

// openAILaneSim follows OpenAI's automatic caching: every request caches its
// whole prompt and a later one reads the longest cached prefix. Routing is
// modelled by keying entries on prompt_cache_key; the unified sim drops the
// key (one key for the whole lane) and the day sim adds 24h retention.
type openAILaneSim struct {
	keyed, unified, day *skeletonCacheSim
}

func newOpenAILaneSim() *openAILaneSim {
	// Every block is a cache point, so a read may fall back to any prefix.
	lookback := math.MaxInt32
	return &openAILaneSim{
		keyed:   newSkeletonCacheSim(false, lookback),
		unified: newSkeletonCacheSim(false, lookback),
		day:     newSkeletonCacheSim(false, lookback),
	}
}

// openAICachedTokens rounds a matched prefix to what OpenAI reports: nothing
// below 1024 tokens, then whole 128-token steps.
func openAICachedTokens(tokens int64) int64 {
	if tokens < openaiCacheMinTokens {
		return 0
	}
	return tokens / openaiCacheGranularity * openaiCacheGranularity
}

func (s *openAILaneSim) replay(row *CacheSkeleton, cumTokens []int64) skeletonExpectation {
	ttl := openaiInMemoryTTL
	if row.Retention == "24h" {
		ttl = openaiExtendedTTL
	}
	points := make([]skeletonBreakpoint, len(row.BlkHash))
	dayPoints := make([]skeletonBreakpoint, len(row.BlkHash))
	for i := range points {
		points[i] = skeletonBreakpoint{idx: i, ttl: ttl}
		dayPoints[i] = skeletonBreakpoint{idx: i, ttl: openaiExtendedTTL}
	}
	probe := points[len(points)-1:]
	keyed := skeletonPrefixHashes(row, row.ParamFp^(row.CacheKeyHash*0x9E3779B97F4A7C15))
	unified := skeletonPrefixHashes(row, row.ParamFp)

	var e skeletonExpectation
	e.current = openAICachedTokens(s.keyed.lookup(keyed, cumTokens, probe, row.Ts))
	e.adjusted = max(openAICachedTokens(s.unified.lookup(unified, cumTokens, probe, row.Ts)), e.current)
	e.adjusted1h = max(openAICachedTokens(s.day.lookup(unified, cumTokens, probe, row.Ts)), e.adjusted)
	s.keyed.write(keyed, cumTokens, points, row.EndTs, openaiCacheMinTokens)
	s.unified.write(unified, cumTokens, points, row.EndTs, openaiCacheMinTokens)
	s.day.write(unified, cumTokens, dayPoints, row.EndTs, openaiCacheMinTokens)
	e.losses = []skeletonLoss{
		{SkeletonReasonCacheKeyChanged, e.adjusted - e.current},
		{SkeletonReasonTTLExpired, e.adjusted1h - e.adjusted},
	}
	return e
}

func analyzeSkeletonLane(rows []*CacheSkeleton) SkeletonLane {
	lane := SkeletonLane{Turns: make([]SkeletonTurn, 0, len(rows)), Provider: SkeletonProviderClaude}
	if len(rows) > 0 {
		lane.Lane = strconv.FormatUint(rows[0].Lane, 16)
		lane.Model = rows[0].Model
		if rows[0].Provider == SkeletonProviderOpenAI {
			lane.Provider = SkeletonProviderOpenAI
		}
	}
	openAI := lane.Provider == SkeletonProviderOpenAI
	var sim skeletonLaneSim
	minTokens := int64(openaiCacheMinTokens)
	if openAI {
		sim = newOpenAILaneSim()
	} else {
		claude := newClaudeLaneSim(lane.Model)
		minTokens = claude.minTokens
		sim = claude
	}
	var prev *CacheSkeleton

	for _, row := range rows {
		turn := SkeletonTurn{
			RequestId:  row.RequestId,
			Ts:         row.Ts,
			Status:     row.Status,
			Attempt:    row.Attempt,
			ChannelId:  row.ChannelId,
			KeyIdx:     row.KeyIdx,
			DivergeIdx: -1,
			ActualRead: int64(row.CacheRead),
		}
		turn.TotalTokens = int64(row.CacheRead) + int64(row.CacheCreation) + int64(row.Input)
		bps := skeletonBreakpoints(row)
		turn.TailVolatile = !openAI && skeletonTailVolatile(row, bps)
		turn.NoCacheKey = openAI && row.CacheKeyHash == 0
		turn.CacheKeyChanged = openAI && prev != nil && row.CacheKeyHash != prev.CacheKeyHash
		cumBytes := make([]uint64, len(row.BlkLen))
		var total uint64
		for i, l := range row.BlkLen {
			total += uint64(l)
			cumBytes[i] = total
		}
		turn.TotalBytes = total
		for _, bp := range bps {
			ttl := "5m"
			if bp.ttl == time.Hour {
				ttl = "1h"
			}
			turn.Breakpoints = append(turn.Breakpoints, SkeletonBreakpointMark{Offset: cumBytes[bp.idx], TTL: ttl})
		}
		if prev != nil {
			turn.GapSeconds = row.Ts.Sub(prev.EndTs).Seconds()
			turn.DivergeIdx, turn.Segments = skeletonSegments(prev, row)
		} else {
			turn.Segments = []SkeletonSegment{{State: "new", Bytes: total}}
		}

		if row.Status != "ok" || turn.TotalTokens == 0 || len(row.BlkHash) == 0 {
			turn.Reason = SkeletonReasonError
			lane.Turns = append(lane.Turns, turn)
			continue
		}
		// With previous_response_id the earlier turns stay upstream, so the
		// prefix cannot be replayed: count the turn as observed.
		if openAI && row.ParentRequestId != "" {
			turn.Reason = SkeletonReasonPreviousResponse
			turn.ExpectedCurrent, turn.ExpectedAdjusted, turn.ExpectedAdjusted1h = turn.ActualRead, turn.ActualRead, turn.ActualRead
			lane.Summary.add(SkeletonRates{Total: turn.TotalTokens, Actual: turn.ActualRead, Current: turn.ActualRead, Adjusted: turn.ActualRead, Adjusted1h: turn.ActualRead})
			lane.Turns = append(lane.Turns, turn)
			continue
		}

		cumTokens := make([]int64, len(cumBytes))
		for i, b := range cumBytes {
			cumTokens[i] = int64(float64(turn.TotalTokens) * float64(b) / float64(total))
		}
		expect := sim.replay(row, cumTokens)
		turn.ExpectedCurrent = expect.current
		turn.ExpectedAdjusted = expect.adjusted
		turn.ExpectedAdjusted1h = expect.adjusted1h

		turn.Reason = classifySkeletonTurn(row, prev, &turn, openAI || len(bps) > 0, minTokens, expect.losses)
		lane.Summary.add(SkeletonRates{
			Total:      turn.TotalTokens,
			Actual:     turn.ActualRead,
			Current:    max(turn.ExpectedCurrent, turn.ActualRead),
			Adjusted:   max(turn.ExpectedAdjusted, turn.ActualRead),
			Adjusted1h: max(turn.ExpectedAdjusted1h, turn.ActualRead),
		})
		lane.Turns = append(lane.Turns, turn)
		prev = row
	}
	return lane
}

// skeletonSegments compares a turn with the previous turn of its lane and
// returns the first diverging block (-1 when the previous turn is a prefix).
func skeletonSegments(prev, cur *CacheSkeleton) (int, []SkeletonSegment) {
	diverge := -1
	n := min(len(prev.BlkHash), len(cur.BlkHash))
	for i := range n {
		if prev.BlkHash[i] != cur.BlkHash[i] {
			diverge = i
			break
		}
	}
	if diverge < 0 && len(cur.BlkHash) < len(prev.BlkHash) {
		diverge = len(cur.BlkHash)
	}
	if prev.ParamFp != cur.ParamFp {
		diverge = 0
	}
	sameEnd := n
	if diverge >= 0 {
		sameEnd = diverge
	}
	segments := []SkeletonSegment{}
	push := func(state string, bytes uint64) {
		if bytes == 0 {
			return
		}
		segments = append(segments, SkeletonSegment{State: state, Bytes: bytes})
	}
	var same, changed, added uint64
	for i, l := range cur.BlkLen {
		switch {
		case i < sameEnd:
			same += uint64(l)
		case i < len(prev.BlkHash):
			changed += uint64(l)
		default:
			added += uint64(l)
		}
	}
	push("same", same)
	push("changed", changed)
	push("new", added)
	return diverge, segments
}

// classifySkeletonTurn names the main reason a turn read less than it could:
// structural causes first, then the largest simulated loss above 5% of the
// turn. losses holds the provider-specific steps beyond the upstream one.
func classifySkeletonTurn(row, prev *CacheSkeleton, turn *SkeletonTurn, cacheable bool, minTokens int64, losses []skeletonLoss) string {
	if prev == nil {
		return SkeletonReasonFirstTurn
	}
	if turn.TotalTokens < minTokens {
		return SkeletonReasonTooSmall
	}
	if !cacheable {
		return SkeletonReasonNoBreakpoint
	}
	if row.ParamFp != prev.ParamFp {
		return SkeletonReasonParamChanged
	}
	firstMessage := len(row.BlkHash)
	if len(row.MsgStart) > 0 {
		firstMessage = int(row.MsgStart[0])
	}
	if turn.DivergeIdx >= 0 && turn.DivergeIdx < firstMessage {
		return SkeletonReasonPrefixChanged
	}
	if row.Ts.Before(prev.EndTs) && turn.ActualRead < turn.ExpectedAdjusted {
		return SkeletonReasonConcurrent
	}

	upstreamReason := SkeletonReasonUpstreamMiss
	if row.ChannelId != prev.ChannelId || row.KeyIdx != prev.KeyIdx {
		upstreamReason = SkeletonReasonChannelSwitch
	}
	candidates := append([]skeletonLoss{{upstreamReason, turn.ExpectedCurrent - turn.ActualRead}}, losses...)
	reason, best := SkeletonReasonHit, int64(float64(turn.TotalTokens)*skeletonLossThreshold)
	for _, c := range candidates {
		if c.loss > best {
			reason, best = c.reason, c.loss
		}
	}
	return reason
}
