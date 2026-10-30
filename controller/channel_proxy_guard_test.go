package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/require"
)

// TestValidateChannelRejectsTpProxyOnAws asserts the save-time guard: an AWS
// Bedrock channel may not bind a tp (reverse) proxy alias (SigV4 + the tp
// scheme-header rewrite are incompatible), while normal proxies and non-AWS
// channels are unaffected. Rejection happens in validateChannel, before any DB
// write, so a bad binding never persists.
func TestValidateChannelRejectsTpProxyOnAws(t *testing.T) {
	ps := system_setting.GetProxySetting()
	orig := ps.Proxies
	ps.Proxies = []system_setting.ProxyEntry{
		{Alias: "tp-1", URL: "http://tp:8080", ProxyType: system_setting.ProxyTypeTp},
		{Alias: "fwd-1", URL: "http://fwd:3128", ProxyType: system_setting.ProxyTypeNormal},
	}
	t.Cleanup(func() { ps.Proxies = orig })

	settingPtr := func(s string) *string { return &s }

	awsTp := &model.Channel{Type: constant.ChannelTypeAws, Key: "ak:sk", Setting: settingPtr(`{"proxy_aliases":["tp-1"]}`)}
	require.Error(t, validateChannel(awsTp, true), "AWS + tp proxy must be rejected before persistence")

	awsFwd := &model.Channel{Type: constant.ChannelTypeAws, Key: "ak:sk", Setting: settingPtr(`{"proxy_aliases":["fwd-1"]}`)}
	require.NoError(t, validateChannel(awsFwd, true), "AWS + normal forward proxy is allowed")

	nonAwsTp := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "sk-x", Setting: settingPtr(`{"proxy_aliases":["tp-1"]}`)}
	require.NoError(t, validateChannel(nonAwsTp, true), "tp proxy on a non-AWS channel is allowed")
}
