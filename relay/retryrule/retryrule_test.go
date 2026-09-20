package retryrule

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func apiError(statusCode int, message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseStatusCode, statusCode)
}

// thinkingStripOps is a representative retry-override config: strip thinking
// blocks, gated on a 400 whose error message mentions thinking.
func thinkingStripOps() []map[string]any {
	cond := []any{
		map[string]any{"path": "status_code", "mode": "full", "value": float64(400)},
		map[string]any{"path": "error_message", "mode": "contains", "value": "thinking"},
	}
	return []map[string]any{
		{
			"mode":       "prune_objects",
			"path":       "messages",
			"value":      map[string]any{"where": map[string]any{"type": "thinking"}},
			"conditions": cond,
			"logic":      "AND",
		},
	}
}

func TestCollectRewritesMatching(t *testing.T) {
	ops := thinkingStripOps()

	cases := []struct {
		name       string
		statusCode int
		message    string
		wantMatch  bool
	}{
		{"bedrock final block thinking 400", 400, "ValidationException: messages.3: The final block in an assistant message cannot be `thinking`.", true},
		{"signature 400 mentioning thinking", 400, "messages.1.content.0.thinking.signature is invalid", true},
		{"generic 400 without thinking", 400, "invalid request: max_tokens too large", false},
		{"thinking text but not 400", 500, "internal error handling thinking block", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ResponseContext(apiError(tc.statusCode, tc.message), "claude")
			rewrites, matched, action := CollectRewrites(ops, ctx)
			assert.Equal(t, tc.wantMatch, matched)
			assert.Equal(t, ActionRetrySameChannel, action, "default action is same channel")
			if tc.wantMatch {
				require.Len(t, rewrites, 1)
				assert.Equal(t, "prune_objects", rewrites[0]["mode"])
				_, hasConditions := rewrites[0]["conditions"]
				assert.False(t, hasConditions, "conditions must be stripped from applied rewrites")
			} else {
				assert.Empty(t, rewrites)
			}
		})
	}
}

func TestCollectRewritesEmptyAndUnconditional(t *testing.T) {
	// No configured ops -> never matches.
	_, matched, _ := CollectRewrites(nil, ResponseContext(apiError(400, "x"), "claude"))
	assert.False(t, matched)

	// An operation without conditions always matches.
	ops := []map[string]any{{"mode": "delete", "path": "x"}}
	rewrites, matched, action := CollectRewrites(ops, ResponseContext(apiError(500, "y"), "openai"))
	assert.True(t, matched)
	assert.Equal(t, ActionRetrySameChannel, action)
	require.Len(t, rewrites, 1)
}

func TestCollectRewritesOrLogic(t *testing.T) {
	ops := []map[string]any{{
		"mode": "delete",
		"path": "x",
		"conditions": []any{
			map[string]any{"path": "error_message", "mode": "contains", "value": "signature"},
			map[string]any{"path": "error_message", "mode": "contains", "value": "thinking"},
		},
		"logic": "OR",
	}}

	_, matched, _ := CollectRewrites(ops, ResponseContext(apiError(400, "bad signature"), "claude"))
	assert.True(t, matched, "OR should match when one condition matches")

	_, matched, _ = CollectRewrites(ops, ResponseContext(apiError(400, "content policy"), "claude"))
	assert.False(t, matched, "OR should not match when no condition matches")
}

func TestCollectRewritesAction(t *testing.T) {
	ctx := ResponseContext(apiError(400, "boom"), "claude")

	t.Run("default is same channel", func(t *testing.T) {
		ops := []map[string]any{{"mode": "delete", "path": "x"}}
		_, matched, action := CollectRewrites(ops, ctx)
		assert.True(t, matched)
		assert.Equal(t, ActionRetrySameChannel, action)
	})

	t.Run("explicit fallback next channel", func(t *testing.T) {
		ops := []map[string]any{{"mode": "delete", "path": "x", "action": ActionFallbackNextChannel}}
		rewrites, matched, action := CollectRewrites(ops, ctx)
		assert.True(t, matched)
		assert.Equal(t, ActionFallbackNextChannel, action)
		require.Len(t, rewrites, 1)
		_, hasAction := rewrites[0]["action"]
		assert.False(t, hasAction, "action must be stripped from applied rewrites")
	})

	t.Run("fallback wins when mixed", func(t *testing.T) {
		ops := []map[string]any{
			{"mode": "delete", "path": "a", "action": ActionRetrySameChannel},
			{"mode": "delete", "path": "b", "action": ActionFallbackNextChannel},
		}
		_, matched, action := CollectRewrites(ops, ctx)
		assert.True(t, matched)
		assert.Equal(t, ActionFallbackNextChannel, action)
	})

	t.Run("unknown action falls back to same channel", func(t *testing.T) {
		ops := []map[string]any{{"mode": "delete", "path": "x", "action": "bogus"}}
		_, matched, action := CollectRewrites(ops, ctx)
		assert.True(t, matched)
		assert.Equal(t, ActionRetrySameChannel, action)
	})
}

func TestShouldTriggerOnBody(t *testing.T) {
	ops := []map[string]any{{
		"conditions": []any{
			map[string]any{"path": "status_code", "mode": "full", "value": float64(200)},
			map[string]any{"path": "response_body", "mode": "contains", "value": "rate limit"},
		},
		"logic":  "AND",
		"action": "fallback_next_channel",
	}}

	hit := SuccessContext("openai", 200, `{"error":{"message":"account rate limit exceeded"}}`)
	assert.True(t, ShouldTriggerOnBody(ops, hit), "200 body containing rate limit should trigger")

	miss := SuccessContext("openai", 200, `{"choices":[{"delta":{"content":"hello"}}]}`)
	assert.False(t, ShouldTriggerOnBody(ops, miss), "clean 200 body should not trigger")
}

func TestCollectRewrites200ForcesFallback(t *testing.T) {
	// A rule authored (wrongly) with retry_same_channel must be coerced to
	// fallback when the trigger is a 200 response body.
	ops := []map[string]any{{
		"conditions": []any{
			map[string]any{"path": "response_body", "mode": "contains", "value": "rate limit"},
		},
		"action": ActionRetrySameChannel,
	}}
	ctx := SuccessContext("openai", 200, "account rate limit exceeded")

	rewrites, matched, action := CollectRewrites(ops, ctx)
	assert.True(t, matched, "pure conditions+action rule (no mode) still matches")
	assert.Empty(t, rewrites, "no rewrite staged for a conditions+action only rule")
	assert.Equal(t, ActionFallbackNextChannel, action, "200-body trigger forces fallback")
}
