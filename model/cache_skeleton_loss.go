package model

import (
	"slices"
	"time"
)

// SkeletonLossAmount partitions uncached input, not money or guaranteed
// recoverable tokens. A turn may contribute disjoint amounts to several rows.
type SkeletonLossAmount struct {
	Reason string `json:"reason"`
	Tokens int64  `json:"tokens"`
	Turns  int64  `json:"turns"`
}

// SkeletonLossSummary covers successful, replayable Claude attempts only.
// Items sum to UncachedTokens; turns overlap across reasons and are not additive.
type SkeletonLossSummary struct {
	AnalyzedTurns  int64                `json:"analyzed_turns"`
	TotalTokens    int64                `json:"total_tokens"`
	ActualRead     int64                `json:"actual_read"`
	UncachedTokens int64                `json:"uncached_tokens"`
	Items          []SkeletonLossAmount `json:"items"`
}

func (s *SkeletonLossSummary) add(other SkeletonLossSummary) {
	if s.Items == nil {
		s.Items = []SkeletonLossAmount{}
	}
	s.AnalyzedTurns += other.AnalyzedTurns
	s.TotalTokens += other.TotalTokens
	s.ActualRead += other.ActualRead
	s.UncachedTokens += other.UncachedTokens
	for _, item := range other.Items {
		found := false
		for i := range s.Items {
			if s.Items[i].Reason == item.Reason {
				s.Items[i].Tokens += item.Tokens
				s.Items[i].Turns += item.Turns
				found = true
				break
			}
		}
		if !found {
			s.Items = append(s.Items, item)
		}
	}
}

// probe changes one lookup constraint without mutating the simulated cache.
// pending is a possible overlap: skeletons record completion, not first byte.
// unlimited measures already-written anchors outside the raw-block window;
// it does not assume a new write at every block.
func (s *skeletonCacheSim) probe(prefix []uint64, tokens []int64, bps []skeletonBreakpoint, at time.Time, pending, unlimited bool) int64 {
	var best int64
	for _, bp := range bps {
		for k := bp.idx; k >= 0; k-- {
			if !unlimited && k <= bp.idx-s.lookback {
				break
			}
			entry := s.entries[prefix[k]]
			if entry == nil || at.After(entry.expireAt) || (!pending && at.Before(entry.availableAt)) {
				continue
			}
			best = max(best, tokens[k])
			break
		}
	}
	return best
}

// skeletonTurnLosses uses a deterministic waterfall so one token is never
// counted twice. Observed reads floor every successive ceiling. Structural
// changes account only for the old-history portion, leaving new input separate.
func skeletonTurnLosses(row, prev *CacheSkeleton, turn *SkeletonTurn, minTokens int64, e skeletonExpectation) []SkeletonLossAmount {
	uncached := max(0, turn.TotalTokens-turn.ActualRead)
	if uncached == 0 {
		return nil
	}
	if prev == nil {
		return []SkeletonLossAmount{{Reason: SkeletonReasonFirstTurn, Tokens: uncached, Turns: 1}}
	}
	if turn.TotalTokens < minTokens {
		return []SkeletonLossAmount{{Reason: SkeletonReasonTooSmall, Tokens: uncached, Turns: 1}}
	}
	upstream := SkeletonReasonUpstreamMiss
	if row.ChannelId != prev.ChannelId || row.KeyIdx != prev.KeyIdx {
		upstream = SkeletonReasonChannelSwitch
	}
	// A changed parameter invalidates the old history; a content change
	// invalidates from its first changed block through the old history's end.
	// This is affected input, not a promise that the gateway can recover it.
	end := min(len(prev.BlkHash), len(row.BlkHash))
	var oldBytes, stableBytes uint64
	for i := range end {
		oldBytes += uint64(row.BlkLen[i])
		if turn.DivergeIdx < 0 || i < turn.DivergeIdx {
			stableBytes += uint64(row.BlkLen[i])
		}
	}
	oldTokens := int64(float64(turn.TotalTokens) * float64(oldBytes) / float64(turn.TotalBytes))
	stableTokens := int64(float64(turn.TotalTokens) * float64(stableBytes) / float64(turn.TotalBytes))

	reason := ""
	if row.ParamFp != prev.ParamFp {
		reason = SkeletonReasonParamChanged
		// A parameter change invalidates the whole old-history prefix, even
		// the byte-identical part: it changes what gets rendered ahead of it.
		stableTokens = 0
	} else if turn.DivergeIdx >= 0 && turn.DivergeIdx < end {
		reason = SkeletonReasonPrefixChanged
	}

	// A coverage gap is stable history beyond BOTH observed endpoints. A
	// normally advancing endpoint covers the previous history, even if its
	// old anchor has since expired or fallen outside the lookback window.
	// The gap is structural evidence of insufficient writes, not a promise
	// that a hypothetical write would still be alive.
	bps, previousBps := skeletonBreakpoints(row), skeletonBreakpoints(prev)
	last := -1
	for _, points := range [][]skeletonBreakpoint{bps, previousBps} {
		for _, bp := range points {
			last = max(last, bp.idx)
		}
	}
	var coveredBytes uint64
	for i := 0; i <= last && i < len(row.BlkLen); i++ {
		coveredBytes += uint64(row.BlkLen[i])
	}
	gapStart := int64(float64(turn.TotalTokens) * float64(coveredBytes) / float64(turn.TotalBytes))
	hasGap := len(bps) > 0 && len(previousBps) > 0 && stableTokens > gapStart

	// First explain the already-observed policy's miss. Counterfactual
	// gains must never absorb current-actual or overlap an observed read.
	candidates := []skeletonLoss{
		{upstream, e.current},
		{SkeletonReasonMisplaced, e.adjusted},
	}
	if hasGap {
		bridgeReason := SkeletonReasonStableUnexplained
		switch {
		case e.adjusted1h >= gapStart:
			bridgeReason = SkeletonReasonTTLExpired
		case e.lookback >= gapStart:
			bridgeReason = SkeletonReasonLookback
		case e.pending >= gapStart:
			bridgeReason = SkeletonReasonConcurrent
		}
		candidates = append(candidates,
			skeletonLoss{bridgeReason, gapStart},
			skeletonLoss{SkeletonReasonStalled, stableTokens},
		)
	} else if len(bps) > 0 && len(previousBps) > 0 {
		// Rolling also bridges large jumps between valid, moving endpoints.
		// That is a lookback improvement, not evidence of stalled coverage.
		candidates = append(candidates, skeletonLoss{SkeletonReasonLookback, e.advanced})
	} else {
		// Reuse needs a write in the earlier request as well as a read
		// breakpoint now. Adding only the latter cannot repair a missing write.
		candidates = append(candidates, skeletonLoss{SkeletonReasonNoBreakpoint, max(e.fallback, e.advanced)})
	}
	candidates = append(candidates,
		skeletonLoss{SkeletonReasonTTLExpired, max(e.adjusted1h, e.advanced1h)},
		skeletonLoss{SkeletonReasonLookback, e.lookback},
		skeletonLoss{SkeletonReasonConcurrent, e.pending},
	)
	covered := turn.ActualRead
	items := make([]SkeletonLossAmount, 0, len(candidates)+3)
	for _, candidate := range candidates {
		ceiling := min(turn.TotalTokens, candidate.loss)
		if ceiling <= covered {
			continue
		}
		items = append(items, SkeletonLossAmount{Reason: candidate.reason, Tokens: ceiling - covered, Turns: 1})
		covered = ceiling
	}

	// Equal bytes alone prove neither a cache write nor an upstream miss.
	if stableTokens > covered {
		items = append(items, SkeletonLossAmount{Reason: SkeletonReasonStableUnexplained, Tokens: stableTokens - covered, Turns: 1})
		covered = stableTokens
	}

	if reason != "" {
		amount := max(0, oldTokens-covered)
		if amount > 0 {
			items = append(items, SkeletonLossAmount{Reason: reason, Tokens: amount, Turns: 1})
			covered = oldTokens
		}
	}
	if covered < turn.TotalTokens {
		items = append(items, SkeletonLossAmount{Reason: SkeletonReasonNewContent, Tokens: turn.TotalTokens - covered, Turns: 1})
	}
	// A reason can explain disjoint segments of this turn. Keep its first
	// position and total tokens, but count the affected turn only once.
	merged := items[:0]
	for _, item := range items {
		idx := slices.IndexFunc(merged, func(other SkeletonLossAmount) bool { return other.Reason == item.Reason })
		if idx >= 0 {
			merged[idx].Tokens += item.Tokens
			continue
		}
		item.Turns = 1
		merged = append(merged, item)
	}
	return merged
}
