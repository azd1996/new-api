package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMChatDisabled(t *testing.T) {
	_, err := LLMChat(context.Background(), 1, LLMChatRequest{
		Messages: []LLMChatMessage{{Role: "user", Content: "hello"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestLLMChatDistribute(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := modelManagementDB(t, "sqlite", "")
	cfg := config.GlobalConfig.Get("ai_assistant_setting").(*operation_setting.AIAssistantSetting)
	previousCfg := *cfg
	previousGroupRatio := ratio_setting.GroupRatio2JSONString()
	previousBatch, previousLogs := common.BatchUpdateEnabled, common.LogConsumeEnabled
	previousErrors, previousCount := constant.ErrorLogEnabled, constant.CountToken
	previousSelfUse, previousUnset := operation_setting.SelfUseModeEnabled, operation_setting.AllowUnsetModelRatio
	t.Cleanup(func() {
		*cfg = previousCfg
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatio))
		common.BatchUpdateEnabled, common.LogConsumeEnabled = previousBatch, previousLogs
		constant.ErrorLogEnabled, constant.CountToken = previousErrors, previousCount
		operation_setting.SelfUseModeEnabled, operation_setting.AllowUnsetModelRatio = previousSelfUse, previousUnset
	})
	*cfg = operation_setting.AIAssistantSetting{
		Enabled: true, Group: "ai-assistant", DefaultModel: "assistant-priced-test",
		TimeoutSeconds: 5, MaxTokens: 16, Temperature: 0.5,
	}
	common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false
	constant.ErrorLogEnabled, constant.CountToken = false, false
	operation_setting.SelfUseModeEnabled, operation_setting.AllowUnsetModelRatio = false, false
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"ai-assistant":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"assistant-priced-test":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"assistant-priced-test":1}`))
	user := model.User{
		Username: "assistant-test", Status: common.UserStatusEnabled, Role: common.RoleAdminUser,
		Group: "default", Quota: 1000000, Setting: `{"billing_preference":"wallet_only"}`,
	}
	require.NoError(t, db.Create(&user).Error)
	type attempt struct {
		path string
		body map[string]any
	}
	attempts := make(chan attempt, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, common.DecodeJson(r.Body, &body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		attempts <- attempt{path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"assistant-success","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	}))
	t.Cleanup(upstream.Close)
	channel := model.Channel{
		Name: "assistant-mock", Type: constant.ChannelTypeOpenAI, Key: "test-only",
		Status: common.ChannelStatusEnabled, Models: "assistant-priced-test,assistant-unpriced-test",
		Group: "ai-assistant", BaseURL: common.GetPointer(upstream.URL), AutoBan: common.GetPointer(0),
		ModelMapping: common.GetPointer(`{"assistant-priced-test":"upstream-model"}`),
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(db))

	t.Run("routes the default model through the assistant group", func(t *testing.T) {
		result, err := LLMChat(context.Background(), user.Id, LLMChatRequest{
			Messages: []LLMChatMessage{{Role: "user", Content: "hello"}},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "ok", result.Content)
		assert.Equal(t, "stop", result.FinishReason)
		assert.Equal(t, 3, result.PromptTokens)
		assert.Equal(t, 2, result.CompletionTokens)
		assert.Equal(t, 5, result.TotalTokens)
		require.Len(t, attempts, 1)
		sent := <-attempts
		assert.Equal(t, "/v1/chat/completions", sent.path)
		assert.Equal(t, "upstream-model", sent.body["model"])
		assert.Equal(t, false, sent.body["stream"])
		var updated model.User
		require.NoError(t, db.First(&updated, user.Id).Error)
		assert.Equal(t, user.Quota-5, updated.Quota, "internal calls retain wallet billing")
	})
	t.Run("rejects an unpriced model before calling upstream", func(t *testing.T) {
		result, err := LLMChat(context.Background(), user.Id, LLMChatRequest{
			Model: "assistant-unpriced-test", Messages: []LLMChatMessage{{Role: "user", Content: "hello"}},
		})
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "Model assistant-unpriced-test price not configured")
		assert.Empty(t, attempts)
	})
	t.Run("returns distribution errors without invoking Relay", func(t *testing.T) {
		result, err := LLMChat(context.Background(), user.Id, LLMChatRequest{
			Model: "assistant-missing-test", Messages: []LLMChatMessage{{Role: "user", Content: "hello"}},
		})
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "AI assistant request failed")
		assert.NotContains(t, err.Error(), "price not configured")
		assert.NotContains(t, err.Error(), "panic")
		assert.Empty(t, attempts)
	})
}

func TestNormalizeLLMChatRequest(t *testing.T) {
	cfg := operation_setting.AIAssistantSetting{
		Enabled:        true,
		Group:          "ai-assistant",
		DefaultModel:   "gpt-4o",
		TimeoutSeconds: 30,
		MaxTokens:      100,
		Temperature:    0.5,
	}

	t.Run("fills defaults from the assistant setting", func(t *testing.T) {
		req := LLMChatRequest{
			Messages: []LLMChatMessage{{Role: "user", Content: "hello"}},
		}
		require.NoError(t, normalizeLLMChatRequest(&req, cfg))
		assert.Equal(t, "gpt-4o", req.Model)
		assert.Equal(t, 100, req.MaxTokens)
		require.NotNil(t, req.Temperature)
		assert.InEpsilon(t, 0.5, *req.Temperature, 0.0001)
		assert.Equal(t, 30, req.TimeoutSeconds)
	})

	t.Run("keeps explicit values", func(t *testing.T) {
		temperature := 1.5
		req := LLMChatRequest{
			Model:          "custom-model",
			Messages:       []LLMChatMessage{{Role: "user", Content: "hello"}},
			MaxTokens:      16,
			Temperature:    &temperature,
			TimeoutSeconds: 10,
		}
		require.NoError(t, normalizeLLMChatRequest(&req, cfg))
		assert.Equal(t, "custom-model", req.Model)
		assert.Equal(t, 16, req.MaxTokens)
		assert.Equal(t, 1.5, *req.Temperature)
		assert.Equal(t, 10, req.TimeoutSeconds)
	})

	t.Run("rejects a missing model", func(t *testing.T) {
		req := LLMChatRequest{
			Messages: []LLMChatMessage{{Role: "user", Content: "hello"}},
		}
		err := normalizeLLMChatRequest(&req, operation_setting.AIAssistantSetting{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model")
	})

	t.Run("rejects empty messages", func(t *testing.T) {
		err := normalizeLLMChatRequest(&LLMChatRequest{}, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "messages")
	})

	t.Run("rejects an invalid role", func(t *testing.T) {
		req := LLMChatRequest{
			Messages: []LLMChatMessage{{Role: "tool", Content: "hello"}},
		}
		err := normalizeLLMChatRequest(&req, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "role")
	})

	t.Run("rejects an empty message", func(t *testing.T) {
		req := LLMChatRequest{
			Messages: []LLMChatMessage{{Role: "user", Content: "  "}},
		}
		err := normalizeLLMChatRequest(&req, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("rejects too many messages", func(t *testing.T) {
		messages := make([]LLMChatMessage, LLMMaxMessages+1)
		for i := range messages {
			messages[i] = LLMChatMessage{Role: "user", Content: "hello"}
		}
		err := normalizeLLMChatRequest(&LLMChatRequest{Messages: messages}, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot exceed")
	})

	t.Run("rejects oversized content", func(t *testing.T) {
		req := LLMChatRequest{
			Messages: []LLMChatMessage{
				{Role: "user", Content: strings.Repeat("a", LLMMaxContentChars+1)},
			},
		}
		err := normalizeLLMChatRequest(&req, cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "characters")
	})
}

func TestValidateAIAssistantOption(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{"valid enabled", operation_setting.AIAssistantOptionPrefix + "enabled", "true", false},
		{"invalid enabled", operation_setting.AIAssistantOptionPrefix + "enabled", "yes", true},
		{"valid group", operation_setting.AIAssistantOptionPrefix + "group", "ai-assistant", false},
		{"empty group", operation_setting.AIAssistantOptionPrefix + "group", "  ", true},
		{"group with whitespace", operation_setting.AIAssistantOptionPrefix + "group", "ai assistant", true},
		{"valid model", operation_setting.AIAssistantOptionPrefix + "default_model", "gpt-4o", false},
		{"empty model", operation_setting.AIAssistantOptionPrefix + "default_model", "", true},
		{"valid timeout", operation_setting.AIAssistantOptionPrefix + "timeout_seconds", "60", false},
		{"timeout too small", operation_setting.AIAssistantOptionPrefix + "timeout_seconds", "1", true},
		{"timeout too large", operation_setting.AIAssistantOptionPrefix + "timeout_seconds", "301", true},
		{"valid max tokens", operation_setting.AIAssistantOptionPrefix + "max_tokens", "2048", false},
		{"invalid max tokens", operation_setting.AIAssistantOptionPrefix + "max_tokens", "0", true},
		{"valid temperature", operation_setting.AIAssistantOptionPrefix + "temperature", "0.7", false},
		{"temperature too high", operation_setting.AIAssistantOptionPrefix + "temperature", "2.1", true},
		{"unrelated key", "some_other_setting", "anything", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := operation_setting.ValidateAIAssistantOption(tt.key, tt.value)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAIAssistantSettingFromMap(t *testing.T) {
	base := operation_setting.AIAssistantSetting{}
	got := operation_setting.AIAssistantSettingFromMap(base, map[string]string{
		"enabled":         "true",
		"group":           " ai-assistant ",
		"default_model":   " gpt-4o ",
		"timeout_seconds": "1",
		"max_tokens":      "999999",
		"temperature":     "5",
	})

	assert.True(t, got.Enabled)
	assert.Equal(t, "ai-assistant", got.Group)
	assert.Equal(t, "gpt-4o", got.DefaultModel)
	assert.Equal(t, operation_setting.MinAIAssistantTimeoutSeconds, got.TimeoutSeconds)
	assert.Equal(t, operation_setting.MaxAIAssistantMaxTokens, got.MaxTokens)
	assert.Equal(t, float64(operation_setting.MaxAIAssistantTemperature), got.Temperature)

	// Missing keys keep the base values.
	base = operation_setting.AIAssistantSetting{
		Enabled:        true,
		Group:          "existing",
		DefaultModel:   "existing-model",
		TimeoutSeconds: 30,
		MaxTokens:      100,
		Temperature:    0.5,
	}
	got = operation_setting.AIAssistantSettingFromMap(base, map[string]string{
		"default_model": "new-model",
	})
	assert.Equal(t, "existing", got.Group)
	assert.Equal(t, "new-model", got.DefaultModel)
	assert.Equal(t, 30, got.TimeoutSeconds)
}

func TestGetAIAssistantConfig(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/ai-assistant/config", nil)

	GetAIAssistantConfig(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"enabled":false`)
}
