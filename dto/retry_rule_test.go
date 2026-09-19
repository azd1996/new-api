package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// thinkingFallbackRule is a representative rule matching the "thinking family"
// 400 errors and stripping thinking blocks + signature via param-override ops.
func thinkingFallbackRule() RetryRule {
	return RetryRule{
		Name: "claude-thinking-fallback",
		Match: RetryRuleMatch{
			StatusCodes: []int{400},
			ErrorRegex:  "(?i)(signature|final block in an assistant message cannot be .?thinking.?)",
			RelayFormat: "claude",
		},
		Transform: []map[string]any{
			{"mode": "prune_objects", "path": "messages.#.content", "value": map[string]any{"where": map[string]any{"type": "thinking"}}},
			{"mode": "prune_objects", "path": "messages.#.content", "value": map[string]any{"where": map[string]any{"type": "redacted_thinking"}}},
		},
		Retry: RetryRuleRetry{Target: RetryRuleTargetOriginalChannel, MaxAttempts: 1},
	}
}

func TestChannelSettingsRetryRulesRoundTrip(t *testing.T) {
	original := ChannelSettings{
		ThinkingToContent: true,
		Proxy:             "http://127.0.0.1:7890",
		RetryRules:        []RetryRule{thinkingFallbackRule()},
	}

	data, err := common.Marshal(original)
	require.NoError(t, err)
	assert.Contains(t, string(data), "retry_rules", "retry_rules must be serialized when present")

	var decoded ChannelSettings
	require.NoError(t, common.Unmarshal(data, &decoded))
	assert.Equal(t, original, decoded, "ChannelSettings must round-trip through JSON unchanged")
}

func TestChannelSettingsRetryRulesOmittedWhenEmpty(t *testing.T) {
	data, err := common.Marshal(ChannelSettings{ThinkingToContent: true})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "retry_rules", "empty RetryRules must be omitted via omitempty")
}

func TestChannelSettingsBackwardCompatibleWithoutRetryRules(t *testing.T) {
	// An existing channel `setting` value produced before this feature existed.
	legacy := `{"thinking_to_content":true,"proxy":"http://127.0.0.1:7890"}`

	var decoded ChannelSettings
	require.NoError(t, common.Unmarshal([]byte(legacy), &decoded))
	assert.Nil(t, decoded.RetryRules, "legacy settings without retry_rules must decode to nil")
	assert.True(t, decoded.ThinkingToContent)
	assert.Equal(t, "http://127.0.0.1:7890", decoded.Proxy)
}

func TestRetryRuleValidate(t *testing.T) {
	validTransform := []map[string]any{{"mode": "delete", "path": "messages.#.content.#.signature"}}

	cases := []struct {
		name    string
		rule    RetryRule
		wantErr bool
	}{
		{
			name: "valid full rule",
			rule: thinkingFallbackRule(),
		},
		{
			name: "valid with only status code and transform",
			rule: RetryRule{Match: RetryRuleMatch{StatusCodes: []int{400}}, Transform: validTransform},
		},
		{
			name:    "no match criteria",
			rule:    RetryRule{Transform: validTransform},
			wantErr: true,
		},
		{
			name:    "status code out of range",
			rule:    RetryRule{Match: RetryRuleMatch{StatusCodes: []int{99}}, Transform: validTransform},
			wantErr: true,
		},
		{
			name:    "invalid error regex",
			rule:    RetryRule{Match: RetryRuleMatch{ErrorRegex: "("}, Transform: validTransform},
			wantErr: true,
		},
		{
			name:    "empty transform",
			rule:    RetryRule{Match: RetryRuleMatch{StatusCodes: []int{400}}},
			wantErr: true,
		},
		{
			name:    "transform op missing mode",
			rule:    RetryRule{Match: RetryRuleMatch{StatusCodes: []int{400}}, Transform: []map[string]any{{"path": "a"}}},
			wantErr: true,
		},
		{
			name:    "unsupported retry target",
			rule:    RetryRule{Match: RetryRuleMatch{StatusCodes: []int{400}}, Transform: validTransform, Retry: RetryRuleRetry{Target: "somewhere"}},
			wantErr: true,
		},
		{
			name:    "negative max attempts",
			rule:    RetryRule{Match: RetryRuleMatch{StatusCodes: []int{400}}, Transform: validTransform, Retry: RetryRuleRetry{MaxAttempts: -1}},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rule.Validate()
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateRetryRulesReportsFirstInvalid(t *testing.T) {
	rules := []RetryRule{
		thinkingFallbackRule(),
		{Name: "bad", Match: RetryRuleMatch{ErrorRegex: "("}, Transform: []map[string]any{{"mode": "delete", "path": "a"}}},
	}
	err := ValidateRetryRules(rules)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
}
