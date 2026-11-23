package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPreviewClaudeRelayWithChannel covers the full-relay dry run: the Claude
// request is converted, cache optimizations are applied, and the outbound
// request is captured (with sensitive headers redacted) instead of sent.
func TestPreviewClaudeRelayWithChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"claude-sonnet-4","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	c.Request = request

	common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-sonnet-4")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(c, constant.ContextKeyClaudeCacheOptimizationPreviewOverride, &model.CustomerClaudeCacheOptimization{
		ForceExtendedTTL:         model.CustomerClaudeCacheOptimizationRule{Enabled: true},
		RetainPreviousBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true},
		FallbackCacheBreakpoint:  model.CustomerClaudeCacheOptimizationRule{Enabled: true},
	})

	baseURL := ""
	channel := &model.Channel{
		Id:      7,
		Name:    "Anthropic",
		Type:    constant.ChannelTypeAnthropic,
		Key:     "sk-secret",
		Group:   "default",
		Status:  1,
		BaseURL: &baseURL,
	}
	req, err := helper.GetAndValidateClaudeRequest(c)
	require.NoError(t, err)
	info := relaycommon.GenRelayInfoClaude(c, req)

	preview, apiErr := previewClaudeRelayWithChannel(c, info, channel, "default")
	require.Nil(t, apiErr)
	require.NotNil(t, preview)

	assert.Equal(t, 7, preview.ChannelID)
	assert.Equal(t, "Anthropic", preview.ChannelName)
	assert.Equal(t, "default", preview.Group)
	assert.Equal(t, "claude-sonnet-4", preview.OriginModel)
	assert.Equal(t, "https://api.anthropic.com/v1/messages", preview.URL)
	assert.Equal(t, http.MethodPost, preview.Method)
	assert.Contains(t, preview.Body, "cache_control")
	assert.Contains(t, preview.Body, `"ttl":"1h"`)
	assert.Equal(t, "***", preview.Headers["X-Api-Key"])
	assert.NotContains(t, strings.Join(mapValues(preview.Headers), " "), "sk-secret")
	assert.Equal(t, []string{"force_extended_ttl", "retain_previous_breakpoint", "fallback_cache_breakpoint"}, preview.EnabledRules)
	assert.Equal(t, []string{"retain_previous_breakpoint"}, preview.SkippedRules)
}

func mapValues(m map[string]string) []string {
	values := make([]string, 0, len(m))
	for _, value := range m {
		values = append(values, value)
	}
	return values
}
