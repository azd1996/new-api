package system_setting

import "github.com/QuantumNous/new-api/setting/config"

// Proxy forwarding types for ProxyEntry.ProxyType. Empty is treated as Normal.
const (
	// ProxyTypeNormal follows the target scheme: an https target goes through a
	// CONNECT tunnel with end-to-end TLS. This is the default/legacy behavior.
	ProxyTypeNormal = "normal"
	// ProxyTypeTp does NOT follow the target scheme; the proxy reads the target
	// protocol from the `scheme` request header. See docs/proxy-pool.md §11.
	ProxyTypeTp = "tp"
	// ProxyTypeForceHTTP behaves like a normal proxy but blindly downgrades an
	// https target URL to http before forwarding (one extra step vs normal).
	ProxyTypeForceHTTP = "force_http"
)

// ProxyEntry 定义一个命名代理：别名 + 代理地址。
// URL 支持 http/https/socks5/socks5h，可包含认证信息（user:pass@host）。
// Description 仅用于代理池配置页的备注说明，不参与解析、也不在其他地方展示。
type ProxyEntry struct {
	Alias       string `json:"alias"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	// ProxyType selects forwarding behavior: ProxyTypeNormal (default),
	// ProxyTypeTp, or ProxyTypeForceHTTP. Empty is treated as Normal.
	ProxyType string `json:"proxy_type,omitempty"`
	// Auth is an optional credential. For tp proxies it is sent as the
	// X-Tp-Authorization header; other types currently ignore it.
	Auth string `json:"auth,omitempty"`
	// Locked, once true, freezes this entry in the admin UI: every field except
	// Description becomes read-only and no unlock is offered. To change a locked
	// proxy, add a new entry. Enforced in the admin UI.
	Locked bool `json:"locked,omitempty"`
}

// ProxyTtftRoutingConfig holds the global thresholds for TTFT-aware weighted
// proxy selection. A channel opts in with ChannelSettings.ProxyTtftRoutingEnabled;
// these parameters are shared across all channels. All values fall back to the
// defaults below via GetProxyTtftRoutingConfig when unset/invalid.
type ProxyTtftRoutingConfig struct {
	// ThresholdMs is the head proxy's "slow" TTFT threshold in milliseconds. A
	// recorded TTFT at or above it counts as a slow sample.
	ThresholdMs int `json:"threshold_ms,omitempty"`
	// ConsecutiveBad is how many consecutive slow samples on the head proxy
	// activate diversion (and how many consecutive fast samples deactivate it).
	ConsecutiveBad int `json:"consecutive_bad,omitempty"`
	// MinExploreRatio is the minimum fraction of traffic diverted away from the
	// head once diversion is active, so backup proxies keep producing TTFT data.
	MinExploreRatio float64 `json:"min_explore_ratio,omitempty"`
	// MaxDivertRatio caps the total fraction diverted away from the head, so the
	// head always keeps at least (1 - MaxDivertRatio) of the traffic.
	MaxDivertRatio float64 `json:"max_divert_ratio,omitempty"`
	// SampleWindow bounds the per (channel, model, alias) TTFT sample ring.
	SampleWindow int `json:"sample_window,omitempty"`
	// ConnectThresholdMs is the forward-proxy connect-latency "slow" threshold in
	// milliseconds. A head forward proxy whose connect latency is at or above it
	// for ConsecutiveBad samples opens diversion; forward proxies are then weighted
	// by connect latency. Reverse (tp) proxies are excluded from connect-latency
	// comparison (their latency is only the intranet hop to the proxy entry).
	ConnectThresholdMs int `json:"connect_threshold_ms,omitempty"`
}

// Defaults for TTFT-aware proxy routing, applied by GetProxyTtftRoutingConfig.
const (
	defaultProxyTtftThresholdMs     = 3000
	defaultProxyTtftConsecutiveBad  = 3
	defaultProxyTtftMinExploreRatio = 0.2
	defaultProxyTtftMaxDivertRatio  = 0.8
	defaultProxyTtftSampleWindow    = 50
	defaultProxyTtftConnectThreshMs = 1000
)

// ProxySetting 存储全局代理表，供各渠道按别名引用。
type ProxySetting struct {
	Proxies []ProxyEntry `json:"proxies"`
	// TtftRouting holds the global thresholds for TTFT-aware weighted selection.
	TtftRouting ProxyTtftRoutingConfig `json:"ttft_routing,omitempty"`
}

var defaultProxySetting = ProxySetting{
	Proxies: []ProxyEntry{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("proxy_setting", &defaultProxySetting)
}

func GetProxySetting() *ProxySetting {
	return &defaultProxySetting
}

// GetProxyTtftRoutingConfig returns the global TTFT routing parameters with
// defaults applied for any unset or out-of-range field. Ratios are clamped to
// a sane order (0 < MinExplore <= MaxDivert <= 1) so the weighting math stays
// well-formed regardless of how the admin filled the form.
func GetProxyTtftRoutingConfig() ProxyTtftRoutingConfig {
	c := defaultProxySetting.TtftRouting
	if c.ThresholdMs <= 0 {
		c.ThresholdMs = defaultProxyTtftThresholdMs
	}
	if c.ConsecutiveBad <= 0 {
		c.ConsecutiveBad = defaultProxyTtftConsecutiveBad
	}
	if c.SampleWindow <= 0 {
		c.SampleWindow = defaultProxyTtftSampleWindow
	}
	if c.ConnectThresholdMs <= 0 {
		c.ConnectThresholdMs = defaultProxyTtftConnectThreshMs
	}
	if c.MinExploreRatio <= 0 || c.MinExploreRatio >= 1 {
		c.MinExploreRatio = defaultProxyTtftMinExploreRatio
	}
	if c.MaxDivertRatio <= 0 || c.MaxDivertRatio > 1 {
		c.MaxDivertRatio = defaultProxyTtftMaxDivertRatio
	}
	if c.MaxDivertRatio < c.MinExploreRatio {
		c.MaxDivertRatio = c.MinExploreRatio
	}
	return c
}

// onProxyPoolChanged is fired after the global proxy table changes so other
// packages (service) can react, e.g. invalidate cached proxy HTTP clients that
// may now point at a stale address. Registered once at startup; kept as a hook
// to avoid a model/service -> service import cycle.
var onProxyPoolChanged func()

// RegisterProxyPoolChangeHook registers the proxy-pool change callback.
func RegisterProxyPoolChangeHook(fn func()) {
	onProxyPoolChanged = fn
}

// NotifyProxyPoolChanged fires the registered proxy-pool change hook, if any.
func NotifyProxyPoolChanged() {
	if onProxyPoolChanged != nil {
		onProxyPoolChanged()
	}
}
