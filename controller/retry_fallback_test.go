package controller

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupRetryHookDB gives the retry-override audit log writer an in-memory
// database and keeps it off Redis.
func setupRetryHookDB(t *testing.T) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
	})
}

// TestRetryOverrideReturnErrorTerminates protects the return_error contract from
// both ports: a matched return_error rule terminates the relay loop WITHOUT
// mutating apiErr (so the error log keeps the original upstream status/message),
// while handing the relay loop a terminate directive carrying the configured
// override that is applied to the response only after logging.
func TestRetryOverrideReturnErrorTerminates(t *testing.T) {
	setupRetryHookDB(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	// Object form of phase4 with a return_error action, custom code and message.
	setting := `{"retry_override":[{"description":"hard stop on policy block","phase2_response_conditions":{"conditions":[{"path":"status_code","mode":"full","value":400}]},"phase4_retry_action":{"action":"return_error","status_code":451,"status_msg":"blocked by policy"}}]}`
	channel := &model.Channel{Id: 21, Setting: &setting}
	info := &relaycommon.RelayInfo{UserId: 1}
	apiErr := types.NewErrorWithStatusCode(errors.New("upstream rejected"), types.ErrorCodeBadResponse, 400)

	triggered, terminate, detail := maybeApplyRetryRuleFallback(c, info, channel, apiErr, types.RelayFormatOpenAI, &service.RetryParam{})
	assert.False(t, triggered, "terminate must not request a retry")
	require.NotNil(t, terminate, "return_error signals terminate")
	assert.Equal(t, 451, terminate.StatusCode)
	assert.Equal(t, "blocked by policy", terminate.Message)
	require.NotNil(t, detail)
	assert.False(t, info.RetryFallbackDone, "terminate neither pins nor consumes the one-shot retry")

	// apiErr is NOT mutated by the hook: the error log must record the original.
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Equal(t, "upstream rejected", apiErr.Error())

	// The audit detail carries the override for the admin-only retry_override log.
	assert.Equal(t, retryrule.ActionReturnError, detail["action"])
	assert.Equal(t, 451, detail["return_status"])
	assert.Equal(t, "blocked by policy", detail["return_message"])

	// Only after logging does the relay loop apply the override to what the caller
	// sees (mirrors controller.Relay's terminate branch).
	apiErr.StatusCode = terminate.StatusCode
	apiErr.SetMessage(terminate.Message)
	assert.Equal(t, 451, apiErr.StatusCode)
	assert.Equal(t, "blocked by policy", apiErr.Error())
}

type retryOutcome struct {
	Triggered   bool
	Terminate   *retryOverrideTerminate
	Action      any
	Rewrites    []map[string]any
	FallbackSet bool
	Pinned      int
}

func runRetryHook(t *testing.T, setting dto.ChannelSettings, statusCode int, message string, done bool) retryOutcome {
	t.Helper()
	setupRetryHookDB(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	settingJSON, err := common.Marshal(setting)
	require.NoError(t, err)
	settingStr := string(settingJSON)
	channel := &model.Channel{Id: 31, Setting: &settingStr}
	info := &relaycommon.RelayInfo{UserId: 1, OriginModelName: "gpt-4o", RetryFallbackDone: done}
	apiErr := types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponse, statusCode)

	triggered, terminate, detail := maybeApplyRetryRuleFallback(c, info, channel, apiErr, types.RelayFormatOpenAI, &service.RetryParam{})
	out := retryOutcome{Triggered: triggered, Terminate: terminate, Rewrites: info.PendingRetryRewrite, FallbackSet: info.RetryFallbackDone && !done}
	if detail != nil {
		out.Action = detail["action"]
	}
	out.Pinned, _ = service.GetPinnedChannel(c)
	return out
}

func retryRulesFromJSON(t *testing.T, raw string) []dto.RetryRule {
	t.Helper()
	var rules []dto.RetryRule
	require.NoError(t, common.Unmarshal([]byte(raw), &rules))
	return rules
}

// Each retry-override scenario is run twice, once with the rules typed on the
// channel and once with the same rules in a shared template, and must give the
// same decision, staged rewrites, channel pin and one-shot flag.
func TestRetryOverrideTemplateMatchesChannelOwn(t *testing.T) {
	rewrite := `"phase3_request_rewrite":[{"mode":"set","path":"stream","value":false}]`
	cond429 := `"phase2_response_conditions":{"conditions":[{"path":"status_code","mode":"full","value":429}]}`
	tests := []struct {
		name    string
		rules   string
		status  int
		message string
		done    bool
		check   func(t *testing.T, got retryOutcome)
	}{
		{
			name:   "retry_same_channel by default pins channel and stages rewrite",
			rules:  `[{` + cond429 + `,` + rewrite + `}]`,
			status: 429,
			check: func(t *testing.T, got retryOutcome) {
				assert.True(t, got.Triggered)
				assert.Equal(t, retryrule.ActionRetrySameChannel, got.Action)
				assert.Equal(t, 31, got.Pinned)
				assert.True(t, got.FallbackSet)
				assert.Len(t, got.Rewrites, 1)
			},
		},
		{
			name:   "retry_same_channel string action",
			rules:  `[{` + cond429 + `,"phase4_retry_action":"retry_same_channel"}]`,
			status: 429,
			check: func(t *testing.T, got retryOutcome) {
				assert.True(t, got.Triggered)
				assert.Equal(t, 31, got.Pinned)
				assert.Empty(t, got.Rewrites)
			},
		},
		{
			name:   "fallback_next_channel neither pins nor consumes the one-shot",
			rules:  `[{` + cond429 + `,` + rewrite + `,"phase4_retry_action":{"action":"fallback_next_channel"}}]`,
			status: 429,
			check: func(t *testing.T, got retryOutcome) {
				assert.True(t, got.Triggered)
				assert.Equal(t, retryrule.ActionFallbackNextChannel, got.Action)
				assert.Zero(t, got.Pinned)
				assert.False(t, got.FallbackSet)
				assert.Len(t, got.Rewrites, 1)
			},
		},
		{
			name:   "return_error terminates with configured status and message",
			rules:  `[{` + cond429 + `,"phase4_retry_action":{"action":"return_error","status_code":451,"status_msg":"blocked"}}]`,
			status: 429,
			check: func(t *testing.T, got retryOutcome) {
				assert.False(t, got.Triggered)
				require.NotNil(t, got.Terminate)
				assert.Equal(t, 451, got.Terminate.StatusCode)
				assert.Equal(t, "blocked", got.Terminate.Message)
				assert.Zero(t, got.Pinned)
				assert.Empty(t, got.Rewrites)
			},
		},
		{
			name:   "return_error with invalid status falls back to 429",
			rules:  `[{"phase4_retry_action":{"action":"return_error","status_code":7}}]`,
			status: 500,
			check: func(t *testing.T, got retryOutcome) {
				require.NotNil(t, got.Terminate)
				assert.Equal(t, 429, got.Terminate.StatusCode)
			},
		},
		{
			name:   "no rule matches the response",
			rules:  `[{` + cond429 + `,` + rewrite + `}]`,
			status: 500,
			check: func(t *testing.T, got retryOutcome) {
				assert.False(t, got.Triggered)
				assert.Nil(t, got.Terminate)
				assert.Zero(t, got.Pinned)
			},
		},
		{
			name:   "phase1 request condition filters by model",
			rules:  `[{"phase1_request_condition":{"conditions":[{"path":"model","mode":"full","value":"other-model"}]},` + rewrite + `}]`,
			status: 500,
			check: func(t *testing.T, got retryOutcome) {
				assert.False(t, got.Triggered)
			},
		},
		{
			name:   "phase1 request condition matches the model",
			rules:  `[{"phase1_request_condition":{"conditions":[{"path":"model","mode":"full","value":"gpt-4o"}]},"phase4_retry_action":"fallback_next_channel"}]`,
			status: 500,
			check: func(t *testing.T, got retryOutcome) {
				assert.True(t, got.Triggered)
				assert.Equal(t, retryrule.ActionFallbackNextChannel, got.Action)
			},
		},
		{
			name:    "HTTP 200 body error matches response_body",
			rules:   `[{"phase2_response_conditions":{"conditions":[{"path":"response_body","mode":"contains","value":"rate limit"}]},"phase4_retry_action":"fallback_next_channel"}]`,
			status:  200,
			message: "upstream rate limit exceeded",
			check: func(t *testing.T, got retryOutcome) {
				assert.True(t, got.Triggered)
				assert.Equal(t, retryrule.ActionFallbackNextChannel, got.Action)
			},
		},
		{
			name:   "first matching rule decides",
			rules:  `[{` + cond429 + `,"phase4_retry_action":"fallback_next_channel"},{` + cond429 + `,"phase4_retry_action":"return_error"}]`,
			status: 429,
			check: func(t *testing.T, got retryOutcome) {
				assert.True(t, got.Triggered)
				assert.Equal(t, retryrule.ActionFallbackNextChannel, got.Action)
				assert.Nil(t, got.Terminate)
			},
		},
		{
			name:   "one-shot same-channel retry already used",
			rules:  `[{` + cond429 + `}]`,
			status: 429,
			done:   true,
			check: func(t *testing.T, got retryOutcome) {
				assert.False(t, got.Triggered)
				assert.Zero(t, got.Pinned)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templates := operation_setting.GetOverrideTemplateSetting()
			previous := templates.RetryTemplates
			t.Cleanup(func() { templates.RetryTemplates = previous })
			templates.RetryTemplates = []operation_setting.OverrideTemplateItem{{Name: "shared", Content: json.RawMessage(tt.rules)}}

			own := runRetryHook(t, dto.ChannelSettings{RetryOverride: retryRulesFromJSON(t, tt.rules)}, tt.status, tt.message, tt.done)
			viaTemplate := runRetryHook(t, dto.ChannelSettings{RetryOverrideTemplateRefs: []string{"shared"}}, tt.status, tt.message, tt.done)

			tt.check(t, own)
			assert.Equal(t, own, viaTemplate, "template must behave exactly like the same rules on the channel")
		})
	}
}

// Template rules are consulted before the channel's own rules, so a template
// rule that matches wins, and the channel's own rule applies when none does.
func TestRetryOverrideTemplateAndOwnRuleOrder(t *testing.T) {
	templates := operation_setting.GetOverrideTemplateSetting()
	previous := templates.RetryTemplates
	t.Cleanup(func() { templates.RetryTemplates = previous })
	templates.RetryTemplates = []operation_setting.OverrideTemplateItem{{
		Name:    "shared",
		Content: json.RawMessage(`[{"phase2_response_conditions":{"conditions":[{"path":"status_code","mode":"full","value":429}]},"phase4_retry_action":"fallback_next_channel"}]`),
	}}
	setting := dto.ChannelSettings{
		RetryOverrideTemplateRefs: []string{"shared"},
		RetryOverride:             retryRulesFromJSON(t, `[{"phase4_retry_action":{"action":"return_error","status_code":503}}]`),
	}

	matchesTemplate := runRetryHook(t, setting, 429, "", false)
	assert.True(t, matchesTemplate.Triggered)
	assert.Equal(t, retryrule.ActionFallbackNextChannel, matchesTemplate.Action)

	fallsThroughToOwn := runRetryHook(t, setting, 500, "", false)
	assert.False(t, fallsThroughToOwn.Triggered)
	require.NotNil(t, fallsThroughToOwn.Terminate)
	assert.Equal(t, 503, fallsThroughToOwn.Terminate.StatusCode)
}
