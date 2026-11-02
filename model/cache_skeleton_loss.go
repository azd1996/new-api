package model

import "time"

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
	// These values are cumulative prefix ceilings. Taking successive positive
	// increments preserves partial hits and removes overlapping explanations.
	candidates := []skeletonLoss{
		{upstream, e.current},
		{SkeletonReasonMisplaced, e.adjusted},
		{SkeletonReasonTTLExpired, e.adjusted1h},
		{SkeletonReasonLookback, e.lookback},
		{SkeletonReasonConcurrent, e.pending},
		{SkeletonReasonNoBreakpoint, e.fallback},
	}
	covered := turn.ActualRead
	items := make([]SkeletonLossAmount, 0, len(candidates)+2)
	for _, candidate := range candidates {
		ceiling := min(turn.TotalTokens, candidate.loss)
		if ceiling <= covered {
			continue
		}
		items = append(items, SkeletonLossAmount{Reason: candidate.reason, Tokens: ceiling - covered, Turns: 1})
		covered = ceiling
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
	reason := ""
	if row.ParamFp != prev.ParamFp {
		reason = SkeletonReasonParamChanged
		stableBytes = 0
	} else if turn.DivergeIdx >= 0 && turn.DivergeIdx < end {
		reason = SkeletonReasonPrefixChanged
	}
	if reason != "" {
		oldTokens := int64(float64(turn.TotalTokens) * float64(oldBytes) / float64(turn.TotalBytes))
		stableTokens := int64(float64(turn.TotalTokens) * float64(stableBytes) / float64(turn.TotalBytes))
		amount := max(0, oldTokens-max(stableTokens, covered))
		if amount > 0 {
			items = append(items, SkeletonLossAmount{Reason: reason, Tokens: amount, Turns: 1})
			covered += amount
		}
	}
	if covered < turn.TotalTokens {
		items = append(items, SkeletonLossAmount{Reason: SkeletonReasonNewContent, Tokens: turn.TotalTokens - covered, Turns: 1})
	}
	return items
}
