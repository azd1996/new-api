package service

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// Passive proxy health tracking.
//
// Instead of an active prober, proxy health is derived from the outcome of real
// upstream requests that actually use a proxy (see relay/channel/api_request.go).
// Only connection-level outcomes feed this: a request that fails to obtain a
// connection through the proxy is a failure; a request that gets a connection
// (regardless of the upstream HTTP status) is a success. Connection-establish
// latency (dial + TLS) is sampled for P50/P90/P99.
const (
	// proxyFailureThreshold is how many consecutive connection failures mark a
	// proxy unhealthy. A single failure must not demote (too sensitive).
	proxyFailureThreshold = 3
	// proxyCooldown is how long a proxy stays unhealthy before it becomes
	// eligible again (half-open) and can be retried by a later request.
	proxyCooldown = 60 * time.Second
	// proxyLatencyWindow bounds the per-alias connect-latency sample ring.
	proxyLatencyWindow = 200

	ProxyStatusHealthy   = "healthy"
	ProxyStatusUnknown   = "unknown"
	ProxyStatusUnhealthy = "unhealthy"
)

// ProxyAliasStatus is the UI-facing health snapshot of one named proxy.
type ProxyAliasStatus struct {
	Alias           string  `json:"alias"`
	Status          string  `json:"status"`
	P50Ms           float64 `json:"p50_ms"`
	P90Ms           float64 `json:"p90_ms"`
	P99Ms           float64 `json:"p99_ms"`
	Samples         int     `json:"samples"`
	LastCheckedUnix int64   `json:"last_checked_unix"`
}

type proxyHealth struct {
	consecutiveFailures int
	cooldownUntil       time.Time
	everSucceeded       bool
	lastChecked         time.Time
	latencies           []float64 // connect-establish latency in ms (bounded ring)
}

// channelProxyOrder is a channel's runtime priority order of proxy aliases.
// signature detects when the channel's configured alias list changed so the
// runtime order is rebuilt from the new config.
type channelProxyOrder struct {
	signature string
	order     []string
}

var (
	proxyHealthMu    sync.Mutex
	proxyHealthState = make(map[string]*proxyHealth)
	channelOrderMu   sync.Mutex
	channelOrders    = make(map[int]*channelProxyOrder)
)

func init() {
	// Invalidate cached proxy HTTP clients when the global proxy table changes,
	// so editing an alias's address does not keep serving the old endpoint.
	system_setting.RegisterProxyPoolChangeHook(ResetProxyClientCache)
}

// ResolveChannelProxy picks the effective proxy URL for a channel: the first
// eligible (not in failure cooldown) alias in the channel's runtime priority
// order whose alias exists in the global proxy table. It falls back to the
// channel's direct proxy string when the alias list is empty or none of the
// listed proxies is currently eligible. The returned alias is "" when the
// direct fallback was used (so the caller skips health recording).
func ResolveChannelProxy(channelID int, aliases []string, fallback string) (string, string) {
	cleaned := normalizeProxyAliases(aliases)
	if len(cleaned) == 0 {
		return fallback, ""
	}
	order := channelProxyAliasOrder(channelID, cleaned)
	table := proxyAliasURLMap()
	now := time.Now()

	proxyHealthMu.Lock()
	defer proxyHealthMu.Unlock()
	for _, alias := range order {
		url, ok := table[alias]
		if !ok || url == "" {
			continue
		}
		if h := proxyHealthState[alias]; h != nil && h.cooldownUntil.After(now) {
			continue // currently unhealthy (in cooldown)
		}
		return url, alias
	}
	return fallback, ""
}

// RecordProxyConnectResult feeds one real request's connection-level outcome
// into the passive health tracker. connErr must be true only for connection
// failures (could not dial/connect/handshake through the proxy), never for an
// upstream HTTP error. On sustained failure the alias is demoted in priority.
func RecordProxyConnectResult(channelID int, alias string, connectLatency time.Duration, connErr bool) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return
	}
	now := time.Now()
	demote := false

	proxyHealthMu.Lock()
	h := proxyHealthState[alias]
	if h == nil {
		h = &proxyHealth{}
		proxyHealthState[alias] = h
	}
	h.lastChecked = now
	if connErr {
		h.consecutiveFailures++
		if h.consecutiveFailures >= proxyFailureThreshold && !h.cooldownUntil.After(now) {
			h.consecutiveFailures = 0
			h.cooldownUntil = now.Add(proxyCooldown)
			demote = true
		}
	} else {
		h.consecutiveFailures = 0
		h.everSucceeded = true
		h.cooldownUntil = time.Time{} // half-open success -> recovered
		if connectLatency > 0 {
			ms := float64(connectLatency) / float64(time.Millisecond)
			h.latencies = append(h.latencies, ms)
			if len(h.latencies) > proxyLatencyWindow {
				h.latencies = h.latencies[len(h.latencies)-proxyLatencyWindow:]
			}
		}
	}
	proxyHealthMu.Unlock()

	if demote {
		demoteProxyAlias(alias)
		common.SysLog(fmt.Sprintf(
			"proxy priority adjusted due to connection failures: alias %q demoted to tail after %d consecutive failures (channel %d), cooldown %s",
			alias, proxyFailureThreshold, channelID, proxyCooldown))
	}
}

// ProxyHealthSnapshot returns the status of every alias in the global proxy
// table for the admin UI.
func ProxyHealthSnapshot() []ProxyAliasStatus {
	proxies := system_setting.GetProxySetting().Proxies
	now := time.Now()

	proxyHealthMu.Lock()
	defer proxyHealthMu.Unlock()
	out := make([]ProxyAliasStatus, 0, len(proxies))
	for _, p := range proxies {
		alias := strings.TrimSpace(p.Alias)
		if alias == "" {
			continue
		}
		s := ProxyAliasStatus{Alias: alias, Status: ProxyStatusUnknown}
		if h := proxyHealthState[alias]; h != nil {
			switch {
			case h.cooldownUntil.After(now):
				s.Status = ProxyStatusUnhealthy
			case h.everSucceeded:
				s.Status = ProxyStatusHealthy
			}
			s.Samples = len(h.latencies)
			s.P50Ms, s.P90Ms, s.P99Ms = proxyLatencyPercentiles(h.latencies)
			if !h.lastChecked.IsZero() {
				s.LastCheckedUnix = h.lastChecked.Unix()
			}
		}
		out = append(out, s)
	}
	return out
}

func channelProxyAliasOrder(channelID int, configured []string) []string {
	sig := strings.Join(configured, "\n")
	channelOrderMu.Lock()
	defer channelOrderMu.Unlock()
	co := channelOrders[channelID]
	if co == nil || co.signature != sig {
		co = &channelProxyOrder{signature: sig, order: append([]string(nil), configured...)}
		channelOrders[channelID] = co
	}
	return append([]string(nil), co.order...)
}

// demoteProxyAlias moves alias to the tail of every channel's runtime order so
// a recovered proxy does not immediately reclaim the head (anti-flapping).
func demoteProxyAlias(alias string) {
	channelOrderMu.Lock()
	defer channelOrderMu.Unlock()
	for _, co := range channelOrders {
		co.order = moveProxyAliasToTail(co.order, alias)
	}
}

func moveProxyAliasToTail(order []string, alias string) []string {
	idx := -1
	for i, a := range order {
		if a == alias {
			idx = i
			break
		}
	}
	if idx < 0 || idx == len(order)-1 {
		return order
	}
	out := append(order[:idx:idx], order[idx+1:]...)
	return append(out, alias)
}

func normalizeProxyAliases(aliases []string) []string {
	if len(aliases) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(aliases))
	out := make([]string, 0, len(aliases))
	for _, a := range aliases {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

// ProxyBehaviorForAlias returns the configured forwarding type and auth for a
// named proxy. proxyType is one of system_setting.ProxyType* and defaults to
// ProxyTypeNormal for an empty alias, an unknown alias, or an unset/invalid
// type. auth is the entry's credential (sent as X-Tp-Authorization for tp
// proxies).
func ProxyBehaviorForAlias(alias string) (proxyType string, auth string) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return system_setting.ProxyTypeNormal, ""
	}
	for _, p := range system_setting.GetProxySetting().Proxies {
		if strings.TrimSpace(p.Alias) != alias {
			continue
		}
		switch p.ProxyType {
		case system_setting.ProxyTypeTp, system_setting.ProxyTypeForceHTTP:
			return p.ProxyType, strings.TrimSpace(p.Auth)
		default:
			return system_setting.ProxyTypeNormal, strings.TrimSpace(p.Auth)
		}
	}
	return system_setting.ProxyTypeNormal, ""
}

func proxyAliasURLMap() map[string]string {
	proxies := system_setting.GetProxySetting().Proxies
	m := make(map[string]string, len(proxies))
	for _, p := range proxies {
		alias := strings.TrimSpace(p.Alias)
		if alias == "" {
			continue
		}
		m[alias] = strings.TrimSpace(p.URL)
	}
	return m
}

func proxyLatencyPercentiles(samples []float64) (p50, p90, p99 float64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	return proxyPercentile(sorted, 0.50), proxyPercentile(sorted, 0.90), proxyPercentile(sorted, 0.99)
}

func proxyPercentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
