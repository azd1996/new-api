package relay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForceClaudeCacheControlTTL(t *testing.T) {
	t.Run("rewrites ttl on system, tools and message content breakpoints", func(t *testing.T) {
		body := `{
			"system":[{"type":"text","text":"S","cache_control":{"type":"ephemeral"}}],
			"tools":[{"name":"t","cache_control":{"type":"ephemeral","ttl":"5m"}}],
			"messages":[
				{"role":"user","content":[{"type":"text","text":"m","cache_control":{"type":"ephemeral"}}]},
				{"role":"assistant","content":"plain string, no breakpoint"}
			]
		}`
		out, err := forceClaudeCacheControlTTL([]byte(body), "1h")
		require.NoError(t, err)

		assert.Equal(t, "1h", gjson.GetBytes(out, "system.0.cache_control.ttl").String())
		assert.Equal(t, "ephemeral", gjson.GetBytes(out, "system.0.cache_control.type").String(), "type preserved")
		assert.Equal(t, "1h", gjson.GetBytes(out, "tools.0.cache_control.ttl").String(), "existing 5m overwritten")
		assert.Equal(t, "1h", gjson.GetBytes(out, "messages.0.content.0.cache_control.ttl").String())
	})

	t.Run("leaves bodies without cache_control untouched", func(t *testing.T) {
		body := `{"system":[{"type":"text","text":"S"}],"messages":[{"role":"user","content":"hi"}]}`
		out, err := forceClaudeCacheControlTTL([]byte(body), "1h")
		require.NoError(t, err)
		assert.JSONEq(t, body, string(out))
	})

	t.Run("string system does not panic", func(t *testing.T) {
		body := `{"system":"plain","messages":[]}`
		out, err := forceClaudeCacheControlTTL([]byte(body), "1h")
		require.NoError(t, err)
		assert.JSONEq(t, body, string(out))
	})
}

func TestMergeAnthropicBeta(t *testing.T) {
	const beta = "extended-cache-ttl-2025-04-11"

	assert.Equal(t, beta, mergeAnthropicBeta("", beta))
	assert.Equal(t, "a,b,"+beta, mergeAnthropicBeta("a,b", beta))
	// idempotent: already present
	assert.Equal(t, "a,"+beta, mergeAnthropicBeta("a,"+beta, beta))
}

func TestFixClaudeCacheBreakpoints(t *testing.T) {
	const prefix = `"system":[{"type":"text","text":"S","cache_control":{"type":"ephemeral"}}],"messages":[
		{"role":"user","content":"fix it"},
		{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"s"},{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},`
	reminder := `{"role":"system","content":[{"type":"text","text":"<total_tokens>9 left</total_tokens>","cache_control":{"type":"ephemeral","ttl":"1h"}}]}`
	toolResult := `{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}`
	cases := []struct {
		name     string
		body     string
		wantFix  string
		wantAt   string // path of the new breakpoint, "" when unchanged
		tailKept bool
		tailPath string
		wantTTL  string
	}{
		{
			name:     "volatile reminder gains a breakpoint on the tool_result",
			body:     `{` + prefix + toolResult + `,` + reminder + `]}`,
			wantFix:  cacheBreakpointAdded,
			wantAt:   "messages.2.content.0.cache_control",
			tailKept: true,
			tailPath: "messages.3.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:     "all four slots used moves the tail breakpoint",
			body:     `{"tools":[{"name":"a","cache_control":{"type":"ephemeral"}},{"name":"b","cache_control":{"type":"ephemeral"}}],` + prefix + toolResult + `,` + reminder + `]}`,
			wantFix:  cacheBreakpointMoved,
			wantAt:   "messages.2.content.0.cache_control",
			tailPath: "messages.3.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:     "skips thinking when the previous message has no stable block",
			body:     `{` + prefix + reminder + `]}`,
			wantFix:  cacheBreakpointAdded,
			wantAt:   "messages.1.content.1.cache_control",
			tailKept: true,
			tailPath: "messages.2.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:    "target already cached is left alone",
			body:    `{` + prefix + `{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok","cache_control":{"type":"ephemeral"}}]},` + reminder + `]}`,
			wantFix: "",
		},
		{
			name:     "turn-scoped tail breakpoint is always moved",
			body:     `{` + prefix + toolResult + `,{"role":"system","clear_at":"next_user_message","content":[{"type":"text","text":"note","cache_control":{"type":"ephemeral"}}]}]}`,
			wantFix:  cacheBreakpointMoved,
			wantAt:   "messages.2.content.0.cache_control",
			tailPath: "messages.3.content.0.cache_control",
		},
		{
			name:     "string content becomes a text block carrying the breakpoint",
			body:     `{"messages":[{"role":"user","content":"fix it"},` + reminder + `]}`,
			wantFix:  cacheBreakpointAdded,
			wantAt:   "messages.0.content.0.cache_control",
			tailKept: true,
			tailPath: "messages.1.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:    "breakpoint on a plain trailing user turn is not touched",
			body:    `{"messages":[{"role":"assistant","content":"a"},{"role":"user","content":[{"type":"text","text":"q","cache_control":{"type":"ephemeral"}}]}]}`,
			wantFix: "",
		},
		{
			name:    "stable system tail with full slots is not moved",
			body:    `{"tools":[{"name":"a","cache_control":{"type":"ephemeral"}},{"name":"b","cache_control":{"type":"ephemeral"}}],` + prefix + toolResult + `,{"role":"system","content":[{"type":"text","text":"# Environment","cache_control":{"type":"ephemeral"}}]}]}`,
			wantFix: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, fix, err := fixClaudeCacheBreakpoints([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.wantFix, fix)
			if tc.wantAt == "" {
				assert.JSONEq(t, tc.body, string(out))
				return
			}
			require.True(t, gjson.GetBytes(out, tc.wantAt).IsObject(), string(out))
			assert.Equal(t, tc.wantTTL, gjson.GetBytes(out, tc.wantAt+".ttl").String())
			assert.Equal(t, tc.tailKept, gjson.GetBytes(out, tc.tailPath).Exists())
			assert.LessOrEqual(t, len(claudeCacheControlPaths(out)), claudeMaxCacheBreakpoints)
		})
	}
}

func TestPrepareClaudeTurnScopedMessages(t *testing.T) {
	body := `{"messages":[
		{"role":"user","content":"q"},
		{"role":"system","clear_at":"next_user_message","content":[{"type":"text","text":"old","cache_control":{"type":"ephemeral"}}]},
		{"role":"user","content":"q2"},
		{"role":"system","clear_at":"never","content":[{"type":"text","text":"keep","cache_control":{"type":"ephemeral"}}]}
	]}`
	out, hasClearAt, err := prepareClaudeTurnScopedMessages([]byte(body))
	require.NoError(t, err)
	assert.True(t, hasClearAt)
	assert.False(t, gjson.GetBytes(out, "messages.1.content.0.cache_control").Exists(), "turn-scoped messages may not carry cache_control")
	assert.True(t, gjson.GetBytes(out, "messages.3.content.0.cache_control").Exists(), "clear_at never keeps its breakpoint")
	assert.Equal(t, "next_user_message", gjson.GetBytes(out, "messages.1.clear_at").String())

	plain, hasClearAt, err := prepareClaudeTurnScopedMessages([]byte(`{"messages":[{"role":"user","content":"q"}]}`))
	require.NoError(t, err)
	assert.False(t, hasClearAt)
	assert.JSONEq(t, `{"messages":[{"role":"user","content":"q"}]}`, string(plain))
}
