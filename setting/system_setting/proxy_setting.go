package system_setting

import "github.com/QuantumNous/new-api/setting/config"

// ProxyEntry 定义一个命名代理：别名 + 代理地址。
// URL 支持 http/https/socks5/socks5h，可包含认证信息（user:pass@host）。
// Description 仅用于代理池配置页的备注说明，不参与解析、也不在其他地方展示。
type ProxyEntry struct {
	Alias       string `json:"alias"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// ProxySetting 存储全局代理表，供各渠道按别名引用。
type ProxySetting struct {
	Proxies []ProxyEntry `json:"proxies"`
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
