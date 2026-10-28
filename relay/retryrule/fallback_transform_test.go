package retryrule_test

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryOverrideStripsThinking verifies the end-to-end contract of the retry
// override: the strip-thinking operations (conditions already matched and
// removed), run through the real param-override executor, remove thinking /
// redacted_thinking blocks (and their signatures) from a Claude request body
// while preserving the rest. This is the rewrite resent on the retry attempt.
func TestRetryOverrideStripsThinking(t *testing.T) {
	body := []byte(`{"model":"claude-x","messages":[` +
		`{"role":"user","content":[{"type":"text","text":"hi"}]},` +
		`{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"secret reasoning","signature":"sig-abc"},` +
		`{"type":"redacted_thinking","data":"xxx"},` +
		`{"type":"text","text":"final answer"}` +
		`]}]}`)

	ops := []map[string]any{
		{"mode": "prune_objects", "path": "messages.#.content", "value": map[string]any{"where": map[string]any{"type": "thinking"}}},
		{"mode": "prune_objects", "path": "messages.#.content", "value": map[string]any{"where": map[string]any{"type": "redacted_thinking"}}},
	}

	out, err := relaycommon.ApplyParamOverride(
		body,
		map[string]interface{}{"operations": ops},
		nil,
	)
	require.NoError(t, err)

	result := string(out)
	assert.NotContains(t, result, "thinking")
	assert.NotContains(t, result, "signature")
	assert.Contains(t, result, "final answer")
	assert.Contains(t, result, "hi")
}
