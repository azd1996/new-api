package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guardInfoWithRule builds a minimal RelayInfo whose retry_override matches any
// response body containing "boom", so the pre-commit guard can be exercised.
func guardInfoWithRule() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
	info.ChannelSetting = dto.ChannelSettings{
		RetryOverride: []dto.RetryRule{
			{
				Phase2ResponseConditions: &dto.RetryRulePhaseCondition{
					Conditions: []map[string]any{
						{"path": "response_body", "mode": "contains", "value": "boom"},
					},
				},
			},
		},
	}
	return info
}

func TestPrecommitGuardMatchInWindowForwardsNothing(t *testing.T) {
	g := newPrecommitGuard(guardInfoWithRule(), http.StatusOK)
	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }

	require.False(t, g.feed(`{"type":"response.created"}`, false, process))
	assert.Empty(t, forwarded)

	require.True(t, g.feed(`{"type":"error","message":"boom rate limit"}`, false, process))
	assert.Empty(t, forwarded)
	assert.NotNil(t, g.matched)
}

func TestPrecommitGuardCommitsOnContentInOrder(t *testing.T) {
	g := newPrecommitGuard(guardInfoWithRule(), http.StatusOK)
	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }

	require.False(t, g.feed(`{"type":"response.created"}`, false, process))
	require.False(t, g.feed(`{"type":"response.output_text.delta","delta":"hi"}`, true, process))
	assert.Equal(t, []string{`{"type":"response.created"}`, `{"type":"response.output_text.delta","delta":"hi"}`}, forwarded)

	// Post-commit chunks pass straight through, even if they would have matched.
	require.False(t, g.feed(`{"type":"error","message":"boom"}`, false, process))
	assert.Len(t, forwarded, 3)
	assert.Nil(t, g.matched)
}

func TestPrecommitGuardNoRulesPassthrough(t *testing.T) {
	g := newPrecommitGuard(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, http.StatusOK)
	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }

	require.False(t, g.feed(`{"type":"error","message":"boom"}`, false, process))
	assert.Equal(t, []string{`{"type":"error","message":"boom"}`}, forwarded)
	assert.Nil(t, g.matched)
}

func TestIsResponsesStreamCommit(t *testing.T) {
	tests := []struct {
		data string
		want bool
	}{
		{`{"type":"response.created"}`, false},
		{`{"type":"response.in_progress"}`, false},
		{`{"type":"response.queued"}`, false},
		{`{"type":"response.output_text.delta","delta":"hi"}`, true},
		{`{"type":"error","message":"x"}`, true},
		{`not json`, true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isResponsesStreamCommit(tt.data), tt.data)
	}
}

// A 200 Responses stream whose error event arrives right after response.created
// must fall back without writing anything to the client.
func TestOaiResponsesStreamHandlerRetryGuardFallsBackBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\"}}\n\n" +
		"data: {\"type\":\"error\",\"message\":\"boom rate limit\"}\n\n" +
		"data: [DONE]\n\n"
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, apiErr := OaiResponsesStreamHandler(c, guardInfoWithRule(), resp)
	require.NotNil(t, apiErr)
	assert.Nil(t, usage)
	assert.NotContains(t, w.Body.String(), "response.created")
}

const boomRuleJSON = `[{"phase2_response_conditions":{"conditions":[{"path":"response_body","mode":"contains","value":"boom"}]}}]`

func guardInfoWithTemplate(t *testing.T) *relaycommon.RelayInfo {
	t.Helper()
	templates := operation_setting.GetOverrideTemplateSetting()
	previous := templates.RetryTemplates
	t.Cleanup(func() { templates.RetryTemplates = previous })
	templates.RetryTemplates = []operation_setting.OverrideTemplateItem{{Name: "shared", Content: json.RawMessage(boomRuleJSON)}}
	return &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
			ChannelSetting:    dto.ChannelSettings{RetryOverrideTemplateRefs: []string{"shared"}},
		},
	}
}

// A retry-override rule referenced from a template must trigger the 200-body
// detection and the stream pre-commit buffer exactly like the same rule typed
// on the channel.
func TestRetryBodyGuardTemplateRulesMatchChannelOwn(t *testing.T) {
	own := guardInfoWithRule()
	viaTemplate := guardInfoWithTemplate(t)

	for name, info := range map[string]*relaycommon.RelayInfo{"channel own": own, "template": viaTemplate} {
		t.Run(name, func(t *testing.T) {
			assert.NotNil(t, retryBodyGuardError(info, http.StatusOK, []byte(`{"error":"boom rate limit"}`)), "matching 200 body is a retryable error")
			assert.Nil(t, retryBodyGuardError(info, http.StatusOK, []byte(`{"ok":true}`)), "clean 200 body passes")

			g := newPrecommitGuard(info, http.StatusOK)
			var forwarded []string
			process := func(d string) { forwarded = append(forwarded, d) }
			require.False(t, g.feed(`{"type":"response.created"}`, false, process))
			require.True(t, g.feed(`{"type":"error","message":"boom"}`, false, process))
			assert.Empty(t, forwarded, "nothing is forwarded before the match")
			assert.NotNil(t, g.matched)

			clean := newPrecommitGuard(info, http.StatusOK)
			forwarded = nil
			require.False(t, clean.feed(`{"type":"response.created"}`, false, process))
			require.False(t, clean.feed(`{"type":"response.output_text.delta","delta":"hi"}`, true, process))
			assert.Len(t, forwarded, 2, "commit flushes the buffered chunks in order")
		})
	}
}

// With template and own rules together, a body matching either one triggers.
func TestRetryBodyGuardTemplateAndOwnRulesCombine(t *testing.T) {
	info := guardInfoWithTemplate(t)
	info.ChannelSetting.RetryOverride = []dto.RetryRule{{
		Phase2ResponseConditions: &dto.RetryRulePhaseCondition{
			Conditions: []map[string]any{{"path": "response_body", "mode": "contains", "value": "quota"}},
		},
	}}

	assert.NotNil(t, retryBodyGuardError(info, http.StatusOK, []byte("boom")))
	assert.NotNil(t, retryBodyGuardError(info, http.StatusOK, []byte("quota exceeded")))
	assert.Nil(t, retryBodyGuardError(info, http.StatusOK, []byte("fine")))
}

func TestRetryBodyGuardUnknownTemplateIsPassthrough(t *testing.T) {
	info := guardInfoWithTemplate(t)
	info.ChannelSetting.RetryOverrideTemplateRefs = []string{"missing"}

	g := newPrecommitGuard(info, http.StatusOK)
	var forwarded []string
	require.False(t, g.feed(`{"type":"error","message":"boom"}`, false, func(d string) { forwarded = append(forwarded, d) }))
	assert.Len(t, forwarded, 1)
	assert.Nil(t, g.matched)
}

// End to end: a template-only channel falls back on a 200 Responses stream whose
// error arrives right after response.created, forwarding nothing to the client.
func TestOaiResponsesStreamHandlerTemplateRetryGuardFallsBackBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\"}}\n\n" +
		"data: {\"type\":\"error\",\"message\":\"boom rate limit\"}\n\n" +
		"data: [DONE]\n\n"
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, apiErr := OaiResponsesStreamHandler(c, guardInfoWithTemplate(t), resp)
	require.NotNil(t, apiErr)
	assert.Nil(t, usage)
	assert.NotContains(t, w.Body.String(), "response.created")
}
