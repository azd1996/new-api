package proxypool

import (
	"net/http"
	"strings"
)

// ApplySchemeHeaderProxyRewrite adapts an outbound request for a tp proxy.
func ApplySchemeHeaderProxyRewrite(req *http.Request, tpAuth string) {
	if req == nil || req.URL == nil {
		return
	}
	originalScheme := req.URL.Scheme
	req.Header.Set("scheme", originalScheme)
	req.Header.Set("host", req.URL.Host)
	if tpAuth != "" {
		req.Header.Set("X-Tp-Authorization", tpAuth)
	}
	if strings.EqualFold(originalScheme, "https") {
		req.URL.Scheme = "http"
	}
}

// ForceHTTPProxyScheme downgrades an HTTPS target before forwarding.
func ForceHTTPProxyScheme(req *http.Request) {
	if req != nil && req.URL != nil && strings.EqualFold(req.URL.Scheme, "https") {
		req.URL.Scheme = "http"
	}
}
