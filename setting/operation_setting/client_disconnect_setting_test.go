package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAbortUpstreamOnClientDisconnect locks the product rule: the upstream is
// only aborted on client disconnect when the request's api-key (token name) AND
// client-requested model both equal a configured rule. The default (no rule, or
// an incomplete rule/request) keeps the upstream alive.
func TestAbortUpstreamOnClientDisconnect(t *testing.T) {
	original := clientDisconnectSetting.Rules
	t.Cleanup(func() { clientDisconnectSetting.Rules = original })

	require.False(t, AbortUpstreamOnClientDisconnect("team-a", "gpt-4o"),
		"default (no rules) must keep the upstream alive")

	clientDisconnectSetting.Rules = []ClientDisconnectRule{
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
			assert.Equal(t, tc.want, AbortUpstreamOnClientDisconnect(tc.tokenName, tc.model))
		})
	}
}
