package service

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setOverrideTemplates(t *testing.T, param, retry map[string]string) {
	t.Helper()
	templates := operation_setting.GetOverrideTemplateSetting()
	prevParam, prevRetry := templates.ParamTemplates, templates.RetryTemplates
	t.Cleanup(func() { templates.ParamTemplates, templates.RetryTemplates = prevParam, prevRetry })
	templates.ParamTemplates = nil
	for name, content := range param {
		templates.ParamTemplates = append(templates.ParamTemplates, operation_setting.OverrideTemplateItem{Name: name, Content: json.RawMessage(content)})
	}
	templates.RetryTemplates = nil
	for name, content := range retry {
		templates.RetryTemplates = append(templates.RetryTemplates, operation_setting.OverrideTemplateItem{Name: name, Content: json.RawMessage(content)})
	}
}

func channelWith(t *testing.T, paramOverride string, setting dto.ChannelSettings) *model.Channel {
	t.Helper()
	settingJSON, err := common.Marshal(setting)
	require.NoError(t, err)
	settingStr := string(settingJSON)
	channel := &model.Channel{Id: 7, Setting: &settingStr}
	if paramOverride != "" {
		channel.ParamOverride = &paramOverride
	}
	return channel
}

func applyOverride(t *testing.T, override map[string]any) string {
	t.Helper()
	out, err := relaycommon.ApplyParamOverride([]byte(`{"model":"m","temperature":1}`), override, nil)
	require.NoError(t, err)
	return string(out)
}

// A param override written in a template must change the request exactly like
// the same JSON typed on the channel, in every accepted form.
func TestParamOverrideTemplateMatchesChannelOwn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	forms := map[string]string{
		"operations object": `{"operations":[{"mode":"set","path":"temperature","value":0.2},{"mode":"delete","path":"model"}]}`,
		"bare operations":   `[{"mode":"set","path":"temperature","value":0.2},{"mode":"delete","path":"model"}]`,
		"legacy flat":       `{"temperature":0.2}`,
	}
	for name, content := range forms {
		t.Run(name, func(t *testing.T) {
			setOverrideTemplates(t, map[string]string{"tpl": content}, nil)

			ownContent := content
			if content[0] == '[' {
				ownContent = `{"operations":` + content + `}`
			}
			own := ResolveChannelParamOverride(c, channelWith(t, ownContent, dto.ChannelSettings{}))
			viaTemplate := ResolveChannelParamOverride(c, channelWith(t, "", dto.ChannelSettings{ParamOverrideTemplateRefs: []string{"tpl"}}))

			assert.Equal(t, applyOverride(t, own), applyOverride(t, viaTemplate))
			assert.NotEqual(t, `{"model":"m","temperature":1}`, applyOverride(t, viaTemplate), "template must actually change the body")
		})
	}
}

// Template operations run first in selection order and the channel's own
// operations run last, so the channel has the final say.
func TestParamOverrideTemplateOrdering(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	setOverrideTemplates(t, map[string]string{
		"a": `{"operations":[{"mode":"set","path":"temperature","value":0.1}]}`,
		"b": `{"operations":[{"mode":"set","path":"temperature","value":0.2}]}`,
	}, nil)

	tests := []struct {
		name string
		own  string
		refs []string
		want string
	}{
		{"later template wins over earlier", "", []string{"a", "b"}, `{"model":"m","temperature":0.2}`},
		{"selection order matters", "", []string{"b", "a"}, `{"model":"m","temperature":0.1}`},
		{"channel own runs last", `{"operations":[{"mode":"set","path":"temperature","value":0.9}]}`, []string{"a", "b"}, `{"model":"m","temperature":0.9}`},
		{"unknown ref is skipped", "", []string{"missing", "a"}, `{"model":"m","temperature":0.1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := ResolveChannelParamOverride(c, channelWith(t, tt.own, dto.ChannelSettings{ParamOverrideTemplateRefs: tt.refs}))
			assert.JSONEq(t, tt.want, applyOverride(t, merged))
		})
	}
}

func mustRules(t *testing.T, raw string) []dto.RetryRule {
	t.Helper()
	var rules []dto.RetryRule
	require.NoError(t, common.Unmarshal([]byte(raw), &rules))
	return rules
}

// Every retry-override consumer reads EffectiveRetryOverrideRules, so a rule in
// a template must resolve to the identical rule a channel would carry itself.
func TestEffectiveRetryOverrideRules(t *testing.T) {
	ruleA := `{"description":"a","phase2_response_conditions":{"conditions":[{"path":"status_code","mode":"full","value":429}]},"phase4_retry_action":{"action":"fallback_next_channel"}}`
	ruleB := `{"description":"b","phase4_retry_action":"return_error"}`
	ruleOwn := `{"description":"own","phase3_request_rewrite":[{"mode":"set","path":"stream","value":false}]}`
	setOverrideTemplates(t, nil, map[string]string{
		"ta": `[` + ruleA + `]`,
		"tb": `[` + ruleB + `]`,
	})

	own := mustRules(t, `[`+ruleA+`]`)
	assert.Equal(t, own, EffectiveRetryOverrideRules(dto.ChannelSettings{RetryOverride: own}), "channel-own rules pass through")
	assert.Equal(t, own, EffectiveRetryOverrideRules(dto.ChannelSettings{RetryOverrideTemplateRefs: []string{"ta"}}), "template-only equals the same rule on the channel")
	assert.Empty(t, EffectiveRetryOverrideRules(dto.ChannelSettings{}))
	assert.Empty(t, EffectiveRetryOverrideRules(dto.ChannelSettings{RetryOverrideTemplateRefs: []string{"missing", " "}}))

	got := EffectiveRetryOverrideRules(dto.ChannelSettings{
		RetryOverrideTemplateRefs: []string{"tb", "ta"},
		RetryOverride:             mustRules(t, `[`+ruleOwn+`]`),
	})
	require.Len(t, got, 3)
	assert.Equal(t, []string{"b", "a", "own"}, []string{got[0].Description, got[1].Description, got[2].Description}, "templates in selection order, then channel rules")
}
