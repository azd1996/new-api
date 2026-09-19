package retryrule

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func apiError(statusCode int, message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseStatusCode, statusCode)
}

func TestMatchThinkingFamily(t *testing.T) {
	rules := DefaultThinkingFallbackRules()

	cases := []struct {
		name        string
		statusCode  int
		message     string
		relayFormat string
		wantMatch   bool
	}{
		{
			name:       "signature verification 400",
			statusCode: 400,
			message:    "messages.1.content.0: The `signature` field is invalid for thinking block",
			wantMatch:  true,
		},
		{
			name:       "bedrock final block thinking 400",
			statusCode: 400,
			message:    "InvokeModelWithResponseStream, ValidationException: messages.3: The final block in an assistant message cannot be `thinking`.",
			wantMatch:  true,
		},
		{
			name:       "generic 400 max_tokens too large",
			statusCode: 400,
			message:    "invalid request: max_tokens exceeds model limit",
			wantMatch:  false,
		},
		{
			name:       "business 400 content policy",
			statusCode: 400,
			message:    "content policy violation",
			wantMatch:  false,
		},
		{
			name:       "signature-like text but not 400",
			statusCode: 500,
			message:    "internal error while verifying signature",
			wantMatch:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, ok := Match(rules, apiError(tc.statusCode, tc.message), tc.relayFormat)
			assert.Equal(t, tc.wantMatch, ok)
			if tc.wantMatch {
				assert.Equal(t, ruleNameThinkingFallback, rule.Name)
				assert.Equal(t, dto.RetryRuleTargetOriginalChannel, rule.Retry.Target)
			}
		})
	}
}

func TestMatchNilAndEmpty(t *testing.T) {
	_, ok := Match(DefaultThinkingFallbackRules(), nil, "")
	assert.False(t, ok, "nil error must not match")

	_, ok = Match(nil, apiError(400, "signature invalid"), "")
	assert.False(t, ok, "empty rule set must not match")
}

func TestMatchRelayFormatScope(t *testing.T) {
	rules := []dto.RetryRule{{
		Name:      "claude-only",
		Match:     dto.RetryRuleMatch{StatusCodes: []int{400}, ErrorRegex: "signature", RelayFormat: "claude"},
		Transform: []map[string]any{{"mode": "delete", "path": "x"}},
	}}

	_, ok := Match(rules, apiError(400, "bad signature"), "claude")
	assert.True(t, ok, "matching relay format should match")

	_, ok = Match(rules, apiError(400, "bad signature"), "openai")
	assert.False(t, ok, "non-matching relay format should not match")
}

func TestEffectiveRules(t *testing.T) {
	custom := []dto.RetryRule{{
		Name:      "custom",
		Match:     dto.RetryRuleMatch{StatusCodes: []int{429}},
		Transform: []map[string]any{{"mode": "delete", "path": "x"}},
	}}

	t.Run("custom overrides built-in", func(t *testing.T) {
		got := EffectiveRules(dto.ChannelSettings{ThinkingFallbackEnabled: true, RetryRules: custom})
		require.Len(t, got, 1)
		assert.Equal(t, "custom", got[0].Name)
	})

	t.Run("enabled uses built-in defaults", func(t *testing.T) {
		got := EffectiveRules(dto.ChannelSettings{ThinkingFallbackEnabled: true})
		require.Len(t, got, 1)
		assert.Equal(t, ruleNameThinkingFallback, got[0].Name)
	})

	t.Run("off returns nil", func(t *testing.T) {
		assert.Nil(t, EffectiveRules(dto.ChannelSettings{}))
	})
}

func TestDefaultRulesAreValid(t *testing.T) {
	// The built-in rules must pass the same validation admins' custom rules do.
	require.NoError(t, dto.ValidateRetryRules(DefaultThinkingFallbackRules()))
}
