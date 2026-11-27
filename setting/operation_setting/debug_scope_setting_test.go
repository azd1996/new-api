package operation_setting_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebugScopeMatches(t *testing.T) {
	setting := operation_setting.GetDebugScopeSetting()
	original := setting.Rules
	t.Cleanup(func() { setting.Rules = original })

	setting.Rules = []operation_setting.DebugScopeRule{
		{Group: "team-a", Model: "gpt-4o"},
		{Group: "team-b", Model: "claude-3-5-sonnet"},
		{Group: "team-c", Model: "gpt-4o*"},
		{Group: "team-*", Model: "gemini*"},
		{Group: "", Model: "orphan-model"},
		{Group: "orphan-group", Model: ""},
		{Group: "orphan-*", Model: ""},
	}
	cases := []struct {
		name  string
		group string
		model string
		want  bool
	}{
		{"exact match", "team-a", "gpt-4o", true},
		{"second rule match", "team-b", "claude-3-5-sonnet", true},
		{"right group wrong model", "team-a", "gpt-4o-mini", false},
		{"wrong group right model", "team-z", "gpt-4o", false},
		{"model prefix matches longer name", "team-c", "gpt-4o-mini", true},
		{"model prefix matches the bare name", "team-c", "gpt-4o", true},
		{"model prefix rejects other family", "team-c", "claude-3-5-sonnet", false},
		{"group prefix matches", "team-z", "gemini-2.5-pro", true},
		{"group prefix without model prefix", "team-z", "gpt-4o", false},
		{"case sensitive", "TEAM-a", "gpt-4o", false},
		{"empty group", "", "gpt-4o", false},
		{"empty model", "team-a", "", false},
		{"incomplete rule empty group", "", "orphan-model", false},
		{"incomplete rule empty model", "orphan-group", "", false},
		{"incomplete prefix rule", "orphan-x", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, operation_setting.DebugScopeMatches(tc.group, tc.model))
		})
	}

	setting.Rules = []operation_setting.DebugScopeRule{{Group: "team-a", Model: "*"}}
	assert.True(t, operation_setting.DebugScopeMatches("team-a", "anything"))
	assert.False(t, operation_setting.DebugScopeMatches("team-b", "anything"), "model wildcard must not widen the group scope")
	setting.Rules = []operation_setting.DebugScopeRule{}
	assert.False(t, operation_setting.DebugScopeMatches("team-a", "gpt-4o"))

	require.NoError(t, common.UnmarshalJsonStr(`[{"token_name":"team-a","model":"*"}]`, &setting.Rules))
	assert.False(t, operation_setting.DebugScopeMatches("team-a", "gpt-4o"), "legacy token names must not become group rules")
	require.NoError(t, common.UnmarshalJsonStr(`[{"group":"team-a","model":"claude-"}]`, &setting.Rules))
	assert.False(t, operation_setting.DebugScopeMatches("team-a", "claude-sonnet"), "a prefix requires a trailing star")
}

func TestDebugActiveRoutingGroup(t *testing.T) {
	setting := operation_setting.GetDebugScopeSetting()
	originalRules, originalDebug := setting.Rules, common.DebugEnabled
	t.Cleanup(func() {
		setting.Rules = originalRules
		common.DebugEnabled = originalDebug
	})
	common.DebugEnabled = false
	setting.Rules = []operation_setting.DebugScopeRule{{Group: "team-a", Model: "claude-*"}}

	cases := []struct {
		name       string
		group      string
		autoGroup  string
		tokenName  string
		tokenGroup string
		model      string
		want       bool
	}{
		{"first key in group", "team-a", "", "key-one", "team-a", "claude-sonnet", true},
		{"second key in group", "team-a", "", "key-two", "team-a", "claude-sonnet", true},
		{"inherited user group", "team-a", "", "key-three", "", "claude-sonnet", true},
		{"effective group overrides key metadata", "team-b", "", "team-a", "team-a", "claude-sonnet", false},
		{"auto selects matching group", "auto", "team-a", "", "auto", "claude-sonnet", true},
		{"auto selects other group", "auto", "team-b", "", "auto", "claude-sonnet", false},
		{"missing group ignores stale auto group", "", "team-a", "", "", "claude-sonnet", false},
		{"missing client model", "team-a", "", "", "", "", false},
		{"mapped upstream model not used", "team-a", "", "", "", "client-alias", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set(string(constant.ContextKeyUsingGroup), tc.group)
			c.Set(string(constant.ContextKeyAutoGroup), tc.autoGroup)
			c.Set(string(constant.ContextKeyOriginalModel), tc.model)
			c.Set("token_name", tc.tokenName)
			c.Set("token_group", tc.tokenGroup)
			c.Set("model", "claude-sonnet")
			assert.Equal(t, tc.want, logger.DebugActive(c))
		})
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(string(constant.ContextKeyUsingGroup), "auto")
	c.Set(string(constant.ContextKeyOriginalModel), "claude-sonnet")
	setting.Rules = []operation_setting.DebugScopeRule{{Group: "*", Model: "*"}}
	assert.False(t, logger.DebugActive(c), "unresolved auto must not match even a wildcard")
	c.Set(string(constant.ContextKeyAutoGroup), "auto")
	assert.False(t, logger.DebugActive(c), "auto is not a concrete group")
	setting.Rules = []operation_setting.DebugScopeRule{{Group: "team-a", Model: "claude-*"}}
	c.Set(string(constant.ContextKeyAutoGroup), "team-a")
	assert.True(t, logger.DebugActive(c))
	c.Set(string(constant.ContextKeyAutoGroup), "team-b")
	assert.False(t, logger.DebugActive(c), "retry into another group must re-evaluate debug scope")
	c.Set(string(constant.ContextKeyAutoGroup), "team-a")
	assert.True(t, logger.DebugActive(c), "retry back into matching group enables debug again")

	assert.False(t, logger.DebugActive(nil))
	assert.False(t, logger.DebugActive(context.Background()))
	common.DebugEnabled = true
	assert.True(t, logger.DebugActive(nil), "global debug remains unconditional")
	assert.True(t, logger.DebugActive(context.Background()))
	assert.True(t, logger.DebugActive(c))
}
