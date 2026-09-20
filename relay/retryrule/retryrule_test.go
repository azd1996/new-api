package retryrule

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cond(path, mode string, value any) map[string]any {
	return map[string]any{"path": path, "mode": mode, "value": value}
}

func phase(logic string, conds ...map[string]any) *dto.RetryRulePhaseCondition {
	return &dto.RetryRulePhaseCondition{Conditions: conds, Logic: logic}
}

func pruneReasoning() map[string]any {
	return map[string]any{
		"mode":  "prune_objects",
		"path":  "input",
		"value": map[string]any{"where": map[string]any{"type": "reasoning"}},
	}
}

// encryptedContentRule mirrors the doc example: on a Responses 400 whose error
// mentions "encrypted content", strip reasoning items and retry the same channel,
// but only for the gpt-5.6-sol model (phase1).
func encryptedContentRule() dto.RetryRule {
	return dto.RetryRule{
		Phase1RequestCondition: phase("AND", cond("model", "full", "gpt-5.6-sol")),
		Phase2ResponseConditions: phase("AND",
			cond("status_code", "full", float64(400)),
			cond("error_message", "contains", "encrypted content"),
		),
		Phase3RequestRewrite: []map[string]any{pruneReasoning()},
		Phase4RetryAction:    ActionRetrySameChannel,
	}
}

func TestCollectRewritesPhaseGating(t *testing.T) {
	rules := []dto.RetryRule{encryptedContentRule()}

	cases := []struct {
		name      string
		model     string
		status    int
		message   string
		wantMatch bool
	}{
		{"phase1+phase2 match", "gpt-5.6-sol", 400, "The encrypted content for item rs_x could not be verified", true},
		{"phase1 filtered out (other model)", "gpt-4o", 400, "encrypted content", false},
		{"phase2 status mismatch", "gpt-5.6-sol", 500, "encrypted content", false},
		{"phase2 message mismatch", "gpt-5.6-sol", 400, "rate limited", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqCtx := map[string]any{"model": tc.model, "relay_format": "openai_responses"}
			respCtx := ResponseContext(tc.status, tc.message, "openai_responses")
			respCtx["model"] = tc.model
			rewrites, matched, action := CollectRewrites(rules, reqCtx, respCtx)
			assert.Equal(t, tc.wantMatch, matched)
			assert.Equal(t, ActionRetrySameChannel, action)
			if tc.wantMatch {
				require.Len(t, rewrites, 1)
				assert.Equal(t, "prune_objects", rewrites[0]["mode"])
			} else {
				assert.Empty(t, rewrites)
			}
		})
	}
}

func TestCollectRewritesEmptyRules(t *testing.T) {
	respCtx := ResponseContext(400, "x", "claude")
	_, matched, action := CollectRewrites(nil, map[string]any{}, respCtx)
	assert.False(t, matched)
	assert.Equal(t, ActionRetrySameChannel, action)
}

func TestCollectRewritesLogicDefaultOr(t *testing.T) {
	// No explicit logic -> param-override default is OR: matches if ANY condition holds.
	rule := dto.RetryRule{
		Phase2ResponseConditions: &dto.RetryRulePhaseCondition{
			Conditions: []map[string]any{
				cond("error_message", "contains", "signature"),
				cond("error_message", "contains", "thinking"),
			},
		},
		Phase3RequestRewrite: []map[string]any{{"mode": "delete", "path": "x"}},
	}
	rules := []dto.RetryRule{rule}

	_, matched, _ := CollectRewrites(rules, map[string]any{}, ResponseContext(400, "bad signature", "claude"))
	assert.True(t, matched, "default OR matches when one condition matches")

	_, matched, _ = CollectRewrites(rules, map[string]any{}, ResponseContext(400, "content policy", "claude"))
	assert.False(t, matched, "default OR does not match when none matches")

	// Explicit AND requires both.
	rule.Phase2ResponseConditions.Logic = "AND"
	_, matched, _ = CollectRewrites([]dto.RetryRule{rule}, map[string]any{}, ResponseContext(400, "bad signature", "claude"))
	assert.False(t, matched, "AND requires all conditions")
}

func TestCollectRewritesMultipleRewrites(t *testing.T) {
	rule := dto.RetryRule{
		Phase2ResponseConditions: phase("AND", cond("status_code", "full", float64(400))),
		Phase3RequestRewrite: []map[string]any{
			{"mode": "prune_objects", "path": "messages", "value": map[string]any{"where": map[string]any{"type": "thinking"}}},
			{"mode": "prune_objects", "path": "messages", "value": map[string]any{"where": map[string]any{"type": "redacted_thinking"}}},
		},
	}
	rewrites, matched, _ := CollectRewrites([]dto.RetryRule{rule}, map[string]any{}, ResponseContext(400, "boom", "claude"))
	assert.True(t, matched)
	require.Len(t, rewrites, 2)
}

func TestCollectRewritesFallbackWinsAndAction(t *testing.T) {
	respCtx := ResponseContext(400, "boom", "claude")
	rules := []dto.RetryRule{
		{Phase3RequestRewrite: []map[string]any{{"mode": "delete", "path": "a"}}, Phase4RetryAction: ActionRetrySameChannel},
		{Phase3RequestRewrite: []map[string]any{{"mode": "delete", "path": "b"}}, Phase4RetryAction: ActionFallbackNextChannel},
	}
	_, matched, action := CollectRewrites(rules, map[string]any{}, respCtx)
	assert.True(t, matched)
	assert.Equal(t, ActionFallbackNextChannel, action, "fallback wins when any matched rule requests it")
}

func TestCollectRewrites200ForcesFallback(t *testing.T) {
	// Authored (wrongly) with retry_same_channel; a 200-body trigger must coerce
	// the action to fallback. An empty phase3 still matches (pure detect + action).
	rule := dto.RetryRule{
		Phase2ResponseConditions: phase("AND", cond("response_body", "contains", "rate limit")),
		Phase4RetryAction:        ActionRetrySameChannel,
	}
	respCtx := SuccessContext("openai", 200, "account rate limit exceeded")
	rewrites, matched, action := CollectRewrites([]dto.RetryRule{rule}, map[string]any{}, respCtx)
	assert.True(t, matched)
	assert.Empty(t, rewrites, "no rewrite staged for a detect-only rule")
	assert.Equal(t, ActionFallbackNextChannel, action, "200-body trigger forces fallback")
}

func TestShouldTriggerOnBody(t *testing.T) {
	rules := []dto.RetryRule{{
		Phase2ResponseConditions: phase("AND",
			cond("status_code", "full", float64(200)),
			cond("response_body", "contains", "rate limit"),
		),
	}}
	hit := SuccessContext("openai", 200, `{"error":{"message":"account rate limit exceeded"}}`)
	assert.True(t, ShouldTriggerOnBody(rules, map[string]any{}, hit))

	miss := SuccessContext("openai", 200, `{"choices":[{"delta":{"content":"hello"}}]}`)
	assert.False(t, ShouldTriggerOnBody(rules, map[string]any{}, miss))
}

func TestCollectRewritesInvalidConditionIsGraceful(t *testing.T) {
	// A condition missing "mode" fails to parse; the rule must be skipped, never panic.
	rules := []dto.RetryRule{{
		Phase2ResponseConditions: &dto.RetryRulePhaseCondition{
			Conditions: []map[string]any{{"path": "status_code", "value": float64(400)}},
		},
		Phase3RequestRewrite: []map[string]any{{"mode": "delete", "path": "x"}},
	}}
	rewrites, matched, action := CollectRewrites(rules, map[string]any{}, ResponseContext(400, "boom", "claude"))
	assert.False(t, matched, "unparseable condition -> rule skipped")
	assert.Empty(t, rewrites)
	assert.Equal(t, ActionRetrySameChannel, action)
}
