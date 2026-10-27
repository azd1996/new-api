package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDebugScopeMatches locks the product rule: a request only matches when its
// token name AND client-requested model both equal a configured rule, and a
// rule (or request) with an empty field never matches.
func TestDebugScopeMatches(t *testing.T) {
	original := debugScopeSetting.Rules
	t.Cleanup(func() { debugScopeSetting.Rules = original })

	debugScopeSetting.Rules = []DebugScopeRule{
		{TokenName: "team-a", Model: "gpt-4o"},
		{TokenName: "team-b", Model: "claude-3-5-sonnet"},
		{TokenName: "", Model: "orphan-model"}, // incomplete rule, must never match
		{TokenName: "orphan-token", Model: ""}, // incomplete rule, must never match
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
		{"wrong token right model", "team-c", "gpt-4o", false},
		{"empty token", "", "gpt-4o", false},
		{"empty model", "team-a", "", false},
		{"incomplete rule empty token", "", "orphan-model", false},
		{"incomplete rule empty model", "orphan-token", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DebugScopeMatches(tc.tokenName, tc.model))
		})
	}

	debugScopeSetting.Rules = []DebugScopeRule{}
	require.False(t, DebugScopeMatches("team-a", "gpt-4o"), "no rules configured means no match")
}
