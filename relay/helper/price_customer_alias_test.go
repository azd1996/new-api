package helper

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolvePricingModelName 验证「方案甲」价格回退：
// 发生模型映射且请求名未配价时，用映射目标名计价；否则沿用请求名。
func TestResolvePricingModelName(t *testing.T) {
	original := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateModelRatioByJSONString(original)
	})
	// 仅 modelA 配置了倍率；别名 modelA_c1 与 modelX 均未配置。
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"modelA":2.0}`))

	cases := []struct {
		name     string
		info     *relaycommon.RelayInfo
		expected string
	}{
		{
			name:     "not mapped keeps origin",
			info:     &relaycommon.RelayInfo{OriginModelName: "modelX", ChannelMeta: &relaycommon.ChannelMeta{IsModelMapped: false}},
			expected: "modelX",
		},
		{
			name:     "mapped and origin unpriced falls back to upstream",
			info:     &relaycommon.RelayInfo{OriginModelName: "modelA_c1", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "modelA", IsModelMapped: true}},
			expected: "modelA",
		},
		{
			name:     "mapped but upstream equals origin keeps origin",
			info:     &relaycommon.RelayInfo{OriginModelName: "modelA", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "modelA", IsModelMapped: true}},
			expected: "modelA",
		},
		{
			name:     "mapped but origin already priced keeps origin",
			info:     &relaycommon.RelayInfo{OriginModelName: "modelA", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "modelB", IsModelMapped: true}},
			expected: "modelA",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, resolvePricingModelName(tc.info))
		})
	}
}
