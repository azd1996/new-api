package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"

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
	t.Cleanup(func() {
		ps.Proxies = orig
		proxyHealthMu.Lock()
		proxyHealthState = make(map[string]*proxyHealth)
		proxyHealthMu.Unlock()
		channelOrderMu.Lock()
		channelOrders = make(map[int]*channelProxyOrder)
		channelOrderMu.Unlock()
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
