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
