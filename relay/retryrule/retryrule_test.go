package retryrule

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cond(path, mode string, value any) map[string]any {
	return map[string]any{"path": path, "mode": mode, "value": value}
}

func phase(logic string, conds ...map[string]any) *dto.RetryRulePhaseCondition {
	return &dto.RetryRulePhaseCondition{Conditions: conds, Logic: logic}
}

func action(a string) *dto.RetryAction {
	return &dto.RetryAction{Action: a}
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
		Phase4RetryAction:    action(ActionRetrySameChannel),
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
			d := CollectRewrites(rules, reqCtx, respCtx)
			assert.Equal(t, tc.wantMatch, d.Matched)
			assert.Equal(t, ActionRetrySameChannel, d.Action)
			if tc.wantMatch {
				require.Len(t, d.Rewrites, 1)
				assert.Equal(t, "prune_objects", d.Rewrites[0]["mode"])
			} else {
				assert.Empty(t, d.Rewrites)
			}
		})
	}
}

func TestCollectRewritesEmptyRules(t *testing.T) {
	respCtx := ResponseContext(400, "x", "claude")
	d := CollectRewrites(nil, map[string]any{}, respCtx)
	assert.False(t, d.Matched)
	assert.Equal(t, ActionRetrySameChannel, d.Action)
	assert.Equal(t, -1, d.MatchedIndex)
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

	assert.True(t, CollectRewrites(rules, map[string]any{}, ResponseContext(400, "bad signature", "claude")).Matched,
		"default OR matches when one condition matches")
	assert.False(t, CollectRewrites(rules, map[string]any{}, ResponseContext(400, "content policy", "claude")).Matched,
		"default OR does not match when none matches")

	// Explicit AND requires both.
	rule.Phase2ResponseConditions.Logic = "AND"
	assert.False(t, CollectRewrites([]dto.RetryRule{rule}, map[string]any{}, ResponseContext(400, "bad signature", "claude")).Matched,
		"AND requires all conditions")
}

func TestCollectRewritesStagesAllRewritesOfMatchedRule(t *testing.T) {
	rule := dto.RetryRule{
		Phase2ResponseConditions: phase("AND", cond("status_code", "full", float64(400))),
		Phase3RequestRewrite: []map[string]any{
			{"mode": "prune_objects", "path": "messages", "value": map[string]any{"where": map[string]any{"type": "thinking"}}},
			{"mode": "prune_objects", "path": "messages", "value": map[string]any{"where": map[string]any{"type": "redacted_thinking"}}},
		},
	}
	d := CollectRewrites([]dto.RetryRule{rule}, map[string]any{}, ResponseContext(400, "boom", "claude"))
	assert.True(t, d.Matched)
	require.Len(t, d.Rewrites, 2)
}

func TestCollectRewritesFirstMatchWins(t *testing.T) {
	// Rules are priority-ordered: the first matching rule fully decides the
	// action, even if a later matching rule asks for a different one.
	respCtx := ResponseContext(400, "boom", "claude")
	rules := []dto.RetryRule{
		{Phase3RequestRewrite: []map[string]any{{"mode": "delete", "path": "a"}}, Phase4RetryAction: action(ActionRetrySameChannel)},
		{Phase3RequestRewrite: []map[string]any{{"mode": "delete", "path": "b"}}, Phase4RetryAction: action(ActionFallbackNextChannel)},
	}
	d := CollectRewrites(rules, map[string]any{}, respCtx)
	assert.True(t, d.Matched)
	assert.Equal(t, ActionRetrySameChannel, d.Action, "first matched rule's action wins")
	assert.Equal(t, 0, d.MatchedIndex)
	require.Len(t, d.Rewrites, 1)
	assert.Equal(t, "a", d.Rewrites[0]["path"], "only the first matched rule's rewrites are staged")
}

func TestCollectRewritesReturnError(t *testing.T) {
	respCtx := ResponseContext(400, "boom", "claude")

	withStatus := dto.RetryRule{
		Phase2ResponseConditions: phase("AND", cond("status_code", "full", float64(400))),
		Phase4RetryAction:        &dto.RetryAction{Action: ActionReturnError, StatusCode: 403, StatusMsg: "blocked by policy"},
	}
	d := CollectRewrites([]dto.RetryRule{withStatus}, map[string]any{}, respCtx)
	assert.True(t, d.Matched)
	assert.Equal(t, ActionReturnError, d.Action)
	assert.Equal(t, 403, d.ReturnStatus)
	assert.Equal(t, "blocked by policy", d.ReturnMessage)

	// Missing/invalid status code falls back to the default (429).
	missingStatus := dto.RetryRule{
		Phase2ResponseConditions: phase("AND", cond("status_code", "full", float64(400))),
		Phase4RetryAction:        &dto.RetryAction{Action: ActionReturnError},
	}
	d = CollectRewrites([]dto.RetryRule{missingStatus}, map[string]any{}, respCtx)
	assert.Equal(t, ActionReturnError, d.Action)
	assert.Equal(t, defaultReturnErrorStatus, d.ReturnStatus)
	assert.Empty(t, d.ReturnMessage)
}

func TestCollectRewrites200HonorsRuleAction(t *testing.T) {
	// A 200-body trigger must honor the rule's own phase4 action: when the rule
	// asks for retry_same_channel, the action stays retry_same_channel (no forced
	// coercion to fallback). An empty phase3 still matches (detect + action).
	rule := dto.RetryRule{
		Phase2ResponseConditions: phase("AND", cond("response_body", "contains", "rate limit")),
		Phase4RetryAction:        action(ActionRetrySameChannel),
	}
	respCtx := SuccessContext("openai", 200, "account rate limit exceeded")
	d := CollectRewrites([]dto.RetryRule{rule}, map[string]any{}, respCtx)
	assert.True(t, d.Matched)
	assert.Empty(t, d.Rewrites, "no rewrite staged for a detect-only rule")
	assert.Equal(t, ActionRetrySameChannel, d.Action, "200-body trigger honors the rule's action")
}

func TestCollectRewritesReturnsFirstMatchedRuleIndex(t *testing.T) {
	respCtx := ResponseContext(400, "boom", "claude")
	rules := []dto.RetryRule{
		{Phase2ResponseConditions: phase("AND", cond("error_message", "contains", "nope"))},
		{Phase2ResponseConditions: phase("AND", cond("error_message", "contains", "boom")), Phase3RequestRewrite: []map[string]any{{"mode": "delete", "path": "a"}}},
		{Phase2ResponseConditions: phase("AND", cond("status_code", "full", float64(400)))},
	}
	d := CollectRewrites(rules, map[string]any{}, respCtx)
	assert.True(t, d.Matched)
	assert.Equal(t, 1, d.MatchedIndex, "only the first matching rule is selected, later matches ignored")
	require.Len(t, d.Rewrites, 1)
	assert.Equal(t, "a", d.Rewrites[0]["path"])
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
	d := CollectRewrites(rules, map[string]any{}, ResponseContext(400, "boom", "claude"))
	assert.False(t, d.Matched, "unparseable condition -> rule skipped")
	assert.Empty(t, d.Rewrites)
	assert.Equal(t, ActionRetrySameChannel, d.Action)
}
