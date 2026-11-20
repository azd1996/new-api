package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
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

func TestGetAIAssistantConfig(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/ai-assistant/config", nil)

	GetAIAssistantConfig(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Contains(t, recorder.Body.String(), `"enabled":false`)
}
