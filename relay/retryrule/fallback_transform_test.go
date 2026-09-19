package retryrule_test

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestThinkingFallbackTransformStripsThinking verifies the end-to-end contract
// of the built-in fallback: the default rule's Transform, run through the real
// param-override executor, removes thinking / redacted_thinking blocks (and
// therefore their signatures) from a Claude request body while preserving the
// remaining content. This is the request rewrite the original-channel retry
// resends after a thinking-family 400.
func TestThinkingFallbackTransformStripsThinking(t *testing.T) {
	body := []byte(`{"model":"claude-x","messages":[` +
		`{"role":"user","content":[{"type":"text","text":"hi"}]},` +
		`{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"secret reasoning","signature":"sig-abc"},` +
		`{"type":"redacted_thinking","data":"xxx"},` +
		`{"type":"text","text":"final answer"}` +
		`]}]}`)

	rule := retryrule.DefaultThinkingFallbackRules()[0]
	out, err := relaycommon.ApplyParamOverride(body, map[string]interface{}{"operations": rule.Transform}, nil)
	require.NoError(t, err)

	result := string(out)
	assert.NotContains(t, result, "thinking", "thinking and redacted_thinking blocks must be removed")
	assert.NotContains(t, result, "signature", "signature must be gone with the thinking block")
	assert.Contains(t, result, "final answer", "assistant text block must be preserved")
	assert.Contains(t, result, "hi", "user message must be preserved")
}
