package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDebugScopeMatches locks the product rule: a request only matches when its
// token name AND client-requested model both match a configured rule (exactly,
// or by prefix when the rule field ends in "*"), and a rule (or request) with an
// empty field never matches.
func TestDebugScopeMatches(t *testing.T) {
	original := debugScopeSetting.Rules
	t.Cleanup(func() { debugScopeSetting.Rules = original })

	debugScopeSetting.Rules = []DebugScopeRule{
		{TokenName: "team-a", Model: "gpt-4o"},
		{TokenName: "team-b", Model: "claude-3-5-sonnet"},
		{TokenName: "team-c", Model: "gpt-4o*"}, // model prefix
		{TokenName: "team-*", Model: "gemini*"}, // both fields prefix
		{TokenName: "", Model: "orphan-model"},  // incomplete rule, must never match
		{TokenName: "orphan-token", Model: ""},  // incomplete rule, must never match
		{TokenName: "orphan-*", Model: ""},      // incomplete rule, must never match
	}

	cases := []struct {
		name      string
		tokenName string
		model     string
		want      bool
	}{
		{"exact match", "team-a", "gpt-4o", true},
		{"second rule match", "team-b", "claude-3-5-sonnet", true},
		{"right token wrong model", "team-a", "gpt-4o-mini", false},
		{"wrong token right model", "team-z", "gpt-4o", false},
		{"model prefix matches longer name", "team-c", "gpt-4o-mini", true},
		{"model prefix matches the bare name", "team-c", "gpt-4o", true},
		{"model prefix rejects other family", "team-c", "claude-3-5-sonnet", false},
		{"token prefix matches", "team-z", "gemini-2.5-pro", true},
		{"token prefix without model prefix", "team-z", "gpt-4o", false},
		{"empty token", "", "gpt-4o", false},
		{"empty model", "team-a", "", false},
		{"incomplete rule empty token", "", "orphan-model", false},
		{"incomplete rule empty model", "orphan-token", "", false},
		{"incomplete rule empty model with token prefix", "orphan-x", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DebugScopeMatches(tc.tokenName, tc.model))
		})
	}

	debugScopeSetting.Rules = []DebugScopeRule{
		{TokenName: "team-a", Model: "*"},
	}
	assert.True(t, DebugScopeMatches("team-a", "anything"), "bare * model matches every model for that token")
	require.False(t, DebugScopeMatches("team-b", "anything"), "bare * model does not widen the token scope")

	debugScopeSetting.Rules = []DebugScopeRule{}
	require.False(t, DebugScopeMatches("team-a", "gpt-4o"), "no rules configured means no match")
}
