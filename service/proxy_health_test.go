package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetProxyHealthStateForTest clears the package-level passive-health state so
// each test starts from a known baseline.
func resetProxyHealthStateForTest(t *testing.T, proxies []system_setting.ProxyEntry) {
	t.Helper()
	ps := system_setting.GetProxySetting()
	orig := ps.Proxies
	ps.Proxies = proxies
	proxyHealthMu.Lock()
	proxyHealthState = make(map[string]*proxyHealth)
	proxyHealthMu.Unlock()
	channelOrderMu.Lock()
	channelOrders = make(map[int]*channelProxyOrder)
	channelOrderMu.Unlock()
	proxyTtftMu.Lock()
	proxyTtftState = make(map[proxyRoutingKey]*proxyModelRouting)
	proxyTtftMu.Unlock()
	origRand := proxyTtftRandFloat
	t.Cleanup(func() {
		ps.Proxies = orig
		proxyHealthMu.Lock()
		proxyHealthState = make(map[string]*proxyHealth)
		proxyHealthMu.Unlock()
		channelOrderMu.Lock()
		channelOrders = make(map[int]*channelProxyOrder)
		channelOrderMu.Unlock()
		proxyTtftMu.Lock()
		proxyTtftState = make(map[proxyRoutingKey]*proxyModelRouting)
		proxyTtftMu.Unlock()
		proxyTtftRandFloat = origRand
	})
}

// TestProxyFailoverConvergesAndStays verifies a single process: after the
// failure threshold on proxyA, resolution switches to proxyB and STAYS on
// proxyB (no flap back to proxyA while proxyB stays healthy).
func TestProxyFailoverConvergesAndStays(t *testing.T) {
	resetProxyHealthStateForTest(t, []system_setting.ProxyEntry{
		{Alias: "proxyA", URL: "http://a:1"},
		{Alias: "proxyB", URL: "http://b:1"},
	})
	const ch = 1
	aliases := []string{"proxyA", "proxyB"}

	_, alias := ResolveChannelProxy(ch, aliases, "")
	require.Equal(t, "proxyA", alias, "first pick should be the head alias")

	// proxyA is down: threshold-1 failures must NOT yet demote.
	for i := 0; i < proxyFailureThreshold-1; i++ {
		RecordProxyConnectResult(ch, "proxyA", 0, true)
		_, alias = ResolveChannelProxy(ch, aliases, "")
		require.Equal(t, "proxyA", alias, "still proxyA before threshold reached")
	}

	// The threshold-th failure demotes proxyA.
	RecordProxyConnectResult(ch, "proxyA", 0, true)
	_, alias = ResolveChannelProxy(ch, aliases, "")
	require.Equal(t, "proxyB", alias, "should switch to proxyB after threshold")

	// proxyB keeps succeeding; many subsequent resolves must stay on proxyB
	// (no oscillation back to proxyA within a single process).
	for i := 0; i < 20; i++ {
		RecordProxyConnectResult(ch, "proxyB", 5, false)
		_, alias = ResolveChannelProxy(ch, aliases, "")
		require.Equal(t, "proxyB", alias, "must stay on proxyB, no flap")
	}
}

// TestProxyFallbackWhenAllCooling verifies that when every listed alias is in
// cooldown, resolution falls back to the channel's direct proxy string.
func TestProxyFallbackWhenAllCooling(t *testing.T) {
	resetProxyHealthStateForTest(t, []system_setting.ProxyEntry{
		{Alias: "proxyA", URL: "http://a:1"},
	})
	const ch = 2
	aliases := []string{"proxyA"}
	for i := 0; i < proxyFailureThreshold; i++ {
		RecordProxyConnectResult(ch, "proxyA", 0, true)
	}
	url, alias := ResolveChannelProxy(ch, aliases, "socks5://fallback:1")
	require.Equal(t, "", alias, "no alias eligible")
	require.Equal(t, "socks5://fallback:1", url, "falls back to direct proxy")
}

// TestProxyBehaviorForAlias verifies forwarding-type resolution by alias:
// explicit tp/force_http map through, everything else (normal, empty, unknown,
// unset type) resolves to normal; auth is returned trimmed.
func TestProxyBehaviorForAlias(t *testing.T) {
	resetProxyHealthStateForTest(t, []system_setting.ProxyEntry{
		{Alias: "tp-1", URL: "http://tp:8080", ProxyType: system_setting.ProxyTypeTp, Auth: "  secret  "},
		{Alias: "fh-1", URL: "http://fh:8080", ProxyType: system_setting.ProxyTypeForceHTTP},
		{Alias: "plain", URL: "http://p:1", ProxyType: system_setting.ProxyTypeNormal},
		{Alias: "legacy", URL: "http://l:1"}, // unset type => normal
	})

	pt, auth := ProxyBehaviorForAlias("tp-1")
	require.Equal(t, system_setting.ProxyTypeTp, pt)
	require.Equal(t, "secret", auth, "auth is trimmed")

	pt, auth = ProxyBehaviorForAlias("fh-1")
	require.Equal(t, system_setting.ProxyTypeForceHTTP, pt)
	require.Equal(t, "", auth)

	pt, _ = ProxyBehaviorForAlias("plain")
	require.Equal(t, system_setting.ProxyTypeNormal, pt)

	pt, _ = ProxyBehaviorForAlias("legacy")
	require.Equal(t, system_setting.ProxyTypeNormal, pt, "unset type defaults to normal")

	pt, _ = ProxyBehaviorForAlias("missing")
	require.Equal(t, system_setting.ProxyTypeNormal, pt, "unknown alias defaults to normal")

	pt, _ = ProxyBehaviorForAlias("")
	require.Equal(t, system_setting.ProxyTypeNormal, pt, "empty alias defaults to normal")

	// IsTpProxyAlias gates AWS Bedrock bindings: only an explicit tp entry counts.
	require.True(t, IsTpProxyAlias("tp-1"))
	require.False(t, IsTpProxyAlias("fh-1"))
	require.False(t, IsTpProxyAlias("missing"))
}

// routingWithMeans builds a proxyModelRouting whose aliases carry a single TTFT
// sample equal to the given mean (seconds); a nil entry means "no samples yet".
func routingWithMeans(means map[string]float64) *proxyModelRouting {
	r := &proxyModelRouting{aliases: make(map[string]*proxyTtftStat)}
	for alias, m := range means {
		if m <= 0 {
			continue
		}
		r.aliases[alias] = &proxyTtftStat{samples: []float64{m}}
	}
	return r
}

// TestProxyTtftWeights checks the inverse-TTFT weighting and the head-share
// clamp into [1-MaxDivert, 1-MinExplore].
func TestProxyTtftWeights(t *testing.T) {
	cfg := system_setting.GetProxyTtftRoutingConfig() // defaults: min 0.2, max 0.8

	cases := []struct {
		name     string
		eligible []string
		means    map[string]float64
		expect   map[string]float64
	}{
		{
			name:     "faster backup wins but head keeps min share",
			eligible: []string{"head", "b"},
			means:    map[string]float64{"head": 4, "b": 1},
			expect:   map[string]float64{"head": 0.2, "b": 0.8},
		},
		{
			name:     "head faster keeps most traffic but still diverts min",
			eligible: []string{"head", "b"},
			means:    map[string]float64{"head": 1, "b": 4},
			expect:   map[string]float64{"head": 0.8, "b": 0.2},
		},
		{
			name:     "unknown backup explored as if head-slow",
			eligible: []string{"head", "b"},
			means:    map[string]float64{"head": 4},
			expect:   map[string]float64{"head": 0.5, "b": 0.5},
		},
		{
			name:     "max divert caps head loss across two backups",
			eligible: []string{"head", "b", "c"},
			means:    map[string]float64{"head": 10, "b": 0.5, "c": 0.5},
			expect:   map[string]float64{"head": 0.2, "b": 0.4, "c": 0.4},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			weights := computeProxyTtftWeightsLocked(routingWithMeans(tc.means), tc.eligible, cfg)
			var sum float64
			for _, alias := range tc.eligible {
				assert.InDelta(t, tc.expect[alias], weights[alias], 0.01, "weight for %s", alias)
				sum += weights[alias]
			}
			assert.InDelta(t, 1.0, sum, 0.0001, "weights must sum to 1")
		})
	}
}

// TestProxyTtftGateTriggerDivertRecover drives the full lifecycle: strict head
// while fast, diversion after consecutive slow samples, and a return to strict
// head after the head recovers.
func TestProxyTtftGateTriggerDivertRecover(t *testing.T) {
	resetProxyHealthStateForTest(t, []system_setting.ProxyEntry{
		{Alias: "A", URL: "http://a:1"},
		{Alias: "B", URL: "http://b:1"},
	})
	const ch = 10
	const model = "gpt-x"
	aliases := []string{"A", "B"}
	cfg := system_setting.GetProxyTtftRoutingConfig()
	slow := float64(cfg.ThresholdMs)/1000 + 1
	fast := float64(cfg.ThresholdMs)/1000 - 1

	// Head is fast: no diversion, always strict head even though routing is on.
	proxyTtftRandFloat = func() float64 { return 0.99 }
	RecordProxyTTFT(ch, model, "A", fast)
	_, alias := ResolveChannelProxyWeighted(ch, aliases, "", model, true)
	require.Equal(t, "A", alias, "fast head stays strict")

	// Head turns slow for ConsecutiveBad samples -> gate opens.
	for i := 0; i < cfg.ConsecutiveBad; i++ {
		RecordProxyTTFT(ch, model, "A", slow)
	}
	// With head and unknown B both treated as slow, weights are ~0.5/0.5; a high
	// random draw lands on the backup, proving traffic is diverted.
	_, alias = ResolveChannelProxyWeighted(ch, aliases, "", model, true)
	require.Equal(t, "B", alias, "gate open diverts to backup on high random draw")
	// A low draw still lands on the head, so the head keeps a share.
	proxyTtftRandFloat = func() float64 { return 0.0 }
	_, alias = ResolveChannelProxyWeighted(ch, aliases, "", model, true)
	require.Equal(t, "A", alias, "head keeps a share while gate is open")

	// Head recovers for ConsecutiveBad fast samples -> gate closes, strict head.
	for i := 0; i < cfg.ConsecutiveBad; i++ {
		RecordProxyTTFT(ch, model, "A", fast)
	}
	proxyTtftRandFloat = func() float64 { return 0.99 }
	_, alias = ResolveChannelProxyWeighted(ch, aliases, "", model, true)
	require.Equal(t, "A", alias, "recovered head returns to strict priority")
}

// TestProxyTtftChannelStats verifies the per-channel stats expose gate state,
// per-alias selection probability, and cumulative pick counts per model.
func TestProxyTtftChannelStats(t *testing.T) {
	resetProxyHealthStateForTest(t, []system_setting.ProxyEntry{
		{Alias: "A", URL: "http://a:1"},
		{Alias: "B", URL: "http://b:1"},
	})
	const ch = 20
	const model = "gpt-x"
	aliases := []string{"A", "B"}
	cfg := system_setting.GetProxyTtftRoutingConfig()
	slow := float64(cfg.ThresholdMs)/1000 + 1

	// Open the diversion gate (head A consistently slow).
	for i := 0; i < cfg.ConsecutiveBad; i++ {
		RecordProxyTTFT(ch, model, "A", slow)
	}
	// Three resolves land on A (low draw), two on B (high draw) -> known picks.
	proxyTtftRandFloat = func() float64 { return 0.0 }
	for i := 0; i < 3; i++ {
		ResolveChannelProxyWeighted(ch, aliases, "", model, true)
	}
	proxyTtftRandFloat = func() float64 { return 0.99 }
	for i := 0; i < 2; i++ {
		ResolveChannelProxyWeighted(ch, aliases, "", model, true)
	}

	stats := ProxyTtftChannelStats(ch, aliases, true)
	require.Len(t, stats, 1, "one model observed")
	require.Equal(t, model, stats[0].Model)
	require.True(t, stats[0].GateActive, "gate open after consecutive slow samples")

	byAlias := map[string]ProxyAliasRoutingStat{}
	var probSum float64
	for _, a := range stats[0].Aliases {
		byAlias[a.Alias] = a
		probSum += a.Probability
	}
	require.InDelta(t, 1.0, probSum, 0.0001, "probabilities sum to 1 while diverting")
	assert.Greater(t, byAlias["A"].Probability, 0.0)
	assert.Greater(t, byAlias["B"].Probability, 0.0)
	assert.Equal(t, int64(3), byAlias["A"].Picks, "A chosen on low draws")
	assert.Equal(t, int64(2), byAlias["B"].Picks, "B chosen on high draws")
}

// never diverts when the channel has it off or only one alias is eligible.
func TestProxyTtftDisabledAndSingleEligibleStayStrict(t *testing.T) {
	resetProxyHealthStateForTest(t, []system_setting.ProxyEntry{
		{Alias: "A", URL: "http://a:1"},
		{Alias: "B", URL: "http://b:1"},
	})
	const ch = 11
	const model = "gpt-x"
	cfg := system_setting.GetProxyTtftRoutingConfig()
	slow := float64(cfg.ThresholdMs)/1000 + 1
	proxyTtftRandFloat = func() float64 { return 0.99 }

	// Make the head consistently slow so a diversion would fire if enabled.
	for i := 0; i < cfg.ConsecutiveBad+1; i++ {
		RecordProxyTTFT(ch, model, "A", slow)
	}

	// Toggle off: strict head regardless of TTFT.
	_, alias := ResolveChannelProxyWeighted(ch, []string{"A", "B"}, "", model, false)
	require.Equal(t, "A", alias, "disabled channel never diverts")

	// Only one eligible alias: nothing to divert to.
	_, alias = ResolveChannelProxyWeighted(ch, []string{"A"}, "", model, true)
	require.Equal(t, "A", alias, "single eligible alias stays strict")
}
