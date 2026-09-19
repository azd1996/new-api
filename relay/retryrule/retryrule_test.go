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
			"path":       "messages.#.content",
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
			rewrites, matched := CollectRewrites(ops, ctx)
			assert.Equal(t, tc.wantMatch, matched)
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
	_, matched := CollectRewrites(nil, ResponseContext(apiError(400, "x"), "claude"))
	assert.False(t, matched)

	// An operation without conditions always matches.
	ops := []map[string]any{{"mode": "delete", "path": "x"}}
	rewrites, matched := CollectRewrites(ops, ResponseContext(apiError(500, "y"), "openai"))
	assert.True(t, matched)
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

	_, matched := CollectRewrites(ops, ResponseContext(apiError(400, "bad signature"), "claude"))
	assert.True(t, matched, "OR should match when one condition matches")

	_, matched = CollectRewrites(ops, ResponseContext(apiError(400, "content policy"), "claude"))
	assert.False(t, matched, "OR should not match when no condition matches")
}
