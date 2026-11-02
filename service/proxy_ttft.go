package service

import (
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	prommetrics "github.com/QuantumNous/new-api/pkg/prom_metrics"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// TTFT-aware weighted proxy routing.
//
// The passive connection health in proxy_health.go only reacts to connection
// failures; it cannot tell that a proxy is alive but slow. This layer records
// per (channel, model, alias) time-to-first-token (TTFT) and, for channels that
// opt in, shifts traffic away from a head proxy whose TTFT is consistently slow.
//
// Flow:
//   - While the head proxy's TTFT stays under the threshold, 100% of traffic
//     keeps going to it (strict priority, same as legacy failover).
//   - Once the head records ConsecutiveBad slow samples in a row, a diversion
//     "gate" opens: a share of requests is routed to other eligible aliases,
//     weighted by the inverse of each alias's observed TTFT. Aliases with no
//     samples yet are treated as if they were as slow as the head, so they get
//     explored and start producing their own data.
//   - The diverted share is clamped between MinExploreRatio and MaxDivertRatio,
//     so the head always keeps some traffic and backups always keep being probed.
//   - When the head recovers (ConsecutiveBad fast samples in a row), the gate
//     closes and routing returns to strict head priority.
//
// State is per process (like proxy_health.go); each node tunes from its own
// traffic. Nothing is persisted.

// minProxyTtftSeconds floors the per-alias mean TTFT used as a weight divisor so
// a near-zero measurement cannot produce an unbounded weight.
const minProxyTtftSeconds = 0.001

// proxyTtftStat is one alias's recent TTFT history for a (channel, model).
type proxyTtftStat struct {
	samples []float64 // TTFT seconds, bounded ring
	badRun  int       // consecutive samples at or above the slow threshold
	goodRun int       // consecutive samples below the slow threshold
	picks   int64     // cumulative times this alias was actually selected (this node)
}

func (s *proxyTtftStat) mean() float64 {
	if len(s.samples) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s.samples {
		sum += v
	}
	return sum / float64(len(s.samples))
}

// proxyModelRouting holds every alias's TTFT stats for one (channel, model) plus
// the diversion gate state for that pair.
type proxyModelRouting struct {
	aliases    map[string]*proxyTtftStat
	gateActive bool
}

type proxyRoutingKey struct {
	channelID int
	model     string
}

var (
	proxyTtftMu    sync.Mutex
	proxyTtftState = make(map[proxyRoutingKey]*proxyModelRouting)

	// proxyTtftRandFloat returns a value in [0,1) for the weighted pick. It is a
	// package var so tests can make selection deterministic.
	proxyTtftRandFloat = rand.Float64
)

func proxyModelRoutingLocked(channelID int, model string) *proxyModelRouting {
	key := proxyRoutingKey{channelID: channelID, model: model}
	r := proxyTtftState[key]
	if r == nil {
		r = &proxyModelRouting{aliases: make(map[string]*proxyTtftStat)}
		proxyTtftState[key] = r
	}
	return r
}

// RecordProxyTTFT feeds one request's streaming TTFT into the per
// (channel, model, alias) tracker and the Prometheus histogram. ttftSeconds <= 0
// (non-streaming or no first token observed) and an empty alias are ignored.
func RecordProxyTTFT(channelID int, model, alias string, ttftSeconds float64) {
	alias = strings.TrimSpace(alias)
	if alias == "" || ttftSeconds <= 0 {
		return
	}
	cfg := system_setting.GetProxyTtftRoutingConfig()
	threshold := float64(cfg.ThresholdMs) / 1000

	proxyTtftMu.Lock()
	r := proxyModelRoutingLocked(channelID, model)
	st := r.aliases[alias]
	if st == nil {
		st = &proxyTtftStat{}
		r.aliases[alias] = st
	}
	st.samples = append(st.samples, ttftSeconds)
	if len(st.samples) > cfg.SampleWindow {
		st.samples = st.samples[len(st.samples)-cfg.SampleWindow:]
	}
	if ttftSeconds >= threshold {
		st.badRun++
		st.goodRun = 0
	} else {
		st.goodRun++
		st.badRun = 0
	}
	proxyTtftMu.Unlock()

	prommetrics.RecordProxyTtft(strconv.Itoa(channelID), alias, model, ttftSeconds)
}

// pickProxyByTtft chooses one alias from the eligible list (already in priority
// order, head first) for a (channel, model) using TTFT-weighted selection. It
// returns "" when the diversion gate is closed, signalling the caller to keep
// the strict head. eligible must have at least two entries.
func pickProxyByTtft(channelID int, model string, eligible []string) string {
	cfg := system_setting.GetProxyTtftRoutingConfig()
	head := eligible[0]

	proxyTtftMu.Lock()
	r := proxyModelRoutingLocked(channelID, model)
	headStat := r.aliases[head]
	wasActive := r.gateActive
	switch {
	case headStat != nil && headStat.badRun >= cfg.ConsecutiveBad:
		r.gateActive = true
	case r.gateActive && headStat != nil && headStat.goodRun >= cfg.ConsecutiveBad:
		r.gateActive = false
	}
	gateChanged := r.gateActive != wasActive
	active := r.gateActive
	if !r.gateActive {
		proxyTtftMu.Unlock()
		if gateChanged {
			logProxyTtftGateChange(channelID, model, active)
		}
		return ""
	}
	weights := computeProxyTtftWeightsLocked(r, eligible, cfg)
	proxyTtftMu.Unlock()
	if gateChanged {
		logProxyTtftGateChange(channelID, model, active)
	}

	target := proxyTtftRandFloat()
	var cumulative float64
	for _, alias := range eligible {
		cumulative += weights[alias]
		if target < cumulative {
			return alias
		}
	}
	return eligible[len(eligible)-1]
}

// computeProxyTtftWeightsLocked returns the selection probability of each
// eligible alias. Weight is inverse to mean TTFT; an alias with no samples is
// treated as being as slow as the head so it still gets explored. The head's
// share is clamped so at least MinExploreRatio is diverted and at most
// MaxDivertRatio leaves the head. Caller holds proxyTtftMu.
func computeProxyTtftWeightsLocked(r *proxyModelRouting, eligible []string, cfg system_setting.ProxyTtftRoutingConfig) map[string]float64 {
	head := eligible[0]
	headMean := minProxyTtftSeconds
	if st := r.aliases[head]; st != nil && st.mean() > 0 {
		headMean = st.mean()
	}

	raw := make(map[string]float64, len(eligible))
	var total float64
	for _, alias := range eligible {
		mean := headMean // unknown aliases are treated as head-slow (optimistic for exploration)
		if st := r.aliases[alias]; st != nil && st.mean() > 0 {
			mean = st.mean()
		}
		if mean < minProxyTtftSeconds {
			mean = minProxyTtftSeconds
		}
		w := 1 / mean
		raw[alias] = w
		total += w
	}

	weights := make(map[string]float64, len(eligible))
	if total <= 0 {
		even := 1 / float64(len(eligible))
		for _, alias := range eligible {
			weights[alias] = even
		}
		return weights
	}
	for alias, w := range raw {
		weights[alias] = w / total
	}

	// Clamp the head share into [1-MaxDivertRatio, 1-MinExploreRatio], moving the
	// difference proportionally across the non-head aliases.
	loHead := 1 - cfg.MaxDivertRatio
	hiHead := 1 - cfg.MinExploreRatio
	pHead := weights[head]
	clamped := pHead
	if clamped > hiHead {
		clamped = hiHead
	}
	if clamped < loHead {
		clamped = loHead
	}
	if clamped == pHead {
		return weights
	}
	weights[head] = clamped
	nonHeadTotal := 1 - pHead
	remaining := 1 - clamped
	for _, alias := range eligible {
		if alias == head {
			continue
		}
		if nonHeadTotal <= 0 {
			weights[alias] = remaining / float64(len(eligible)-1)
			continue
		}
		weights[alias] = weights[alias] / nonHeadTotal * remaining
	}
	return weights
}

// ProxyTtftAliasSummary is the UI-facing per-alias TTFT aggregate, averaged
// across every (channel, model) that used the alias.
type ProxyTtftAliasSummary struct {
	AvgMs   float64
	Samples int
}

// proxyTtftAliasSummaries aggregates TTFT samples per alias across all
// (channel, model) pairs, for the admin proxy-pool view.
func proxyTtftAliasSummaries() map[string]ProxyTtftAliasSummary {
	proxyTtftMu.Lock()
	defer proxyTtftMu.Unlock()
	type acc struct {
		sum   float64
		count int
	}
	totals := make(map[string]*acc)
	for _, r := range proxyTtftState {
		for alias, st := range r.aliases {
			a := totals[alias]
			if a == nil {
				a = &acc{}
				totals[alias] = a
			}
			for _, v := range st.samples {
				a.sum += v
				a.count++
			}
		}
	}
	out := make(map[string]ProxyTtftAliasSummary, len(totals))
	for alias, a := range totals {
		if a.count == 0 {
			continue
		}
		out[alias] = ProxyTtftAliasSummary{AvgMs: a.sum / float64(a.count) * 1000, Samples: a.count}
	}
	return out
}

// logProxyTtftGateChange is a thin wrapper so gate transitions are observable in
// server logs without threading a logger through the selection hot path.
func logProxyTtftGateChange(channelID int, model string, active bool) {
	state := "closed"
	if active {
		state = "opened"
	}
	common.SysLog(fmt.Sprintf("proxy TTFT diversion gate %s for channel %d model %q", state, channelID, model))
}

// recordProxyPick increments the cumulative selection counter for the alias a
// request was actually routed to, keyed by (channel, model). It counts every
// real selection (strict or weighted) so the per-channel stats view can show
// actual traffic share per proxy. Empty alias (direct fallback) is ignored.
func recordProxyPick(channelID int, model, alias string) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return
	}
	proxyTtftMu.Lock()
	r := proxyModelRoutingLocked(channelID, model)
	st := r.aliases[alias]
	if st == nil {
		st = &proxyTtftStat{}
		r.aliases[alias] = st
	}
	st.picks++
	proxyTtftMu.Unlock()
}

// ProxyAliasRoutingStat is the UI-facing per-alias routing stat for one
// (channel, model): the next-request selection probability, cumulative picks on
// this node, and TTFT samples.
type ProxyAliasRoutingStat struct {
	Alias       string  `json:"alias"`
	Eligible    bool    `json:"eligible"`
	Probability float64 `json:"probability"`
	Picks       int64   `json:"picks"`
	Samples     int     `json:"samples"`
	AvgTtftMs   float64 `json:"avg_ttft_ms"`
}

// ProxyModelRoutingStat groups one model's per-alias routing stats for a channel.
type ProxyModelRoutingStat struct {
	Model      string                  `json:"model"`
	GateActive bool                    `json:"gate_active"`
	Aliases    []ProxyAliasRoutingStat `json:"aliases"`
}

// ProxyTtftChannelStats returns the current TTFT-routing picture for a channel,
// expanded per model: the diversion gate state, each alias's next-request
// selection probability (instantaneous, this node) and cumulative pick count.
// Probability reflects runtime behavior: strict (head=1) when TTFT routing is
// off, fewer than two eligible aliases, or the gate is closed; otherwise the
// inverse-TTFT weighted split. Models with no observed traffic are omitted.
func ProxyTtftChannelStats(channelID int, configuredAliases []string, ttftEnabled bool) []ProxyModelRoutingStat {
	cfg := system_setting.GetProxyTtftRoutingConfig()
	cleaned := normalizeProxyAliases(configuredAliases)
	order := channelProxyAliasOrder(channelID, cleaned)
	table := proxyAliasURLMap()
	now := time.Now()

	// Eligibility (cooldown) is per-alias and model-independent; compute once.
	eligible := make([]string, 0, len(order))
	eligibleSet := make(map[string]struct{}, len(order))
	proxyHealthMu.Lock()
	for _, alias := range order {
		url, ok := table[alias]
		if !ok || url == "" {
			continue
		}
		if h := proxyHealthState[alias]; h != nil && h.cooldownUntil.After(now) {
			continue
		}
		eligible = append(eligible, alias)
		eligibleSet[alias] = struct{}{}
	}
	proxyHealthMu.Unlock()

	proxyTtftMu.Lock()
	defer proxyTtftMu.Unlock()
	out := make([]ProxyModelRoutingStat, 0)
	for key, entry := range proxyTtftState {
		if key.channelID != channelID {
			continue
		}
		useWeighted := ttftEnabled && len(eligible) >= 2 && entry.gateActive
		var weights map[string]float64
		if useWeighted {
			weights = computeProxyTtftWeightsLocked(entry, eligible, cfg)
		}
		rows := make([]ProxyAliasRoutingStat, 0, len(order))
		for _, alias := range order {
			_, isEligible := eligibleSet[alias]
			prob := 0.0
			switch {
			case useWeighted:
				prob = weights[alias]
			case isEligible && alias == eligible[0]:
				prob = 1 // strict head
			}
			row := ProxyAliasRoutingStat{Alias: alias, Eligible: isEligible, Probability: prob}
			if st := entry.aliases[alias]; st != nil {
				row.Picks = st.picks
				row.Samples = len(st.samples)
				row.AvgTtftMs = st.mean() * 1000
			}
			rows = append(rows, row)
		}
		out = append(out, ProxyModelRoutingStat{Model: key.model, GateActive: entry.gateActive, Aliases: rows})
	}
	slices.SortFunc(out, func(a, b ProxyModelRoutingStat) int { return strings.Compare(a.Model, b.Model) })
	return out
}
