package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const (
	// LLMMaxMessages caps how many messages one internal LLM call may carry.
	LLMMaxMessages = 100
	// LLMMaxContentChars caps the total prompt content sent by internal callers.
	LLMMaxContentChars = 200000
)

// LLMChatMessage is one chat message for an internal LLM call.
type LLMChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMChatRequest describes a non-streaming completion request routed through
// the AI assistant group.
type LLMChatRequest struct {
	Model          string           `json:"model"`
	Messages       []LLMChatMessage `json:"messages"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	Temperature    *float64         `json:"temperature,omitempty"`
	TimeoutSeconds int              `json:"timeout_seconds,omitempty"`
}

// LLMChatResponse is the normalized result of an internal LLM call.
type LLMChatResponse struct {
	Content          string `json:"content"`
	Reasoning        string `json:"reasoning,omitempty"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Model            string `json:"model"`
	FinishReason     string `json:"finish_reason"`
}

// AIAssistantConfigResponse exposes the non-sensitive assistant settings to
// the console so the assistant page can pin its group and default model.
type AIAssistantConfigResponse struct {
	Enabled      bool    `json:"enabled"`
	Group        string  `json:"group"`
	DefaultModel string  `json:"default_model"`
	MaxTokens    int     `json:"max_tokens"`
	Temperature  float64 `json:"temperature"`
}

func GetAIAssistantConfig(c *gin.Context) {
	setting := operation_setting.GetAIAssistantSetting()

	// In a multi-node deployment each node keeps an in-memory copy of the
	// options and syncs them from the database every SYNC_FREQUENCY seconds.
	// A setting saved on one node may therefore still be stale on another.
	// This endpoint is low-traffic, so read the persisted options directly to
	// make the assistant page reflect saves immediately on every node.
	if model.DB != nil {
		if options, err := model.AllOption(); err == nil {
			values := make(map[string]string, len(options))
			for _, option := range options {
				if key, ok := strings.CutPrefix(option.Key, operation_setting.AIAssistantOptionPrefix); ok {
					values[key] = option.Value
				}
			}
			if len(values) > 0 {
				setting = operation_setting.AIAssistantSettingFromMap(setting, values)
			}
		}
	}

	common.ApiSuccess(c, AIAssistantConfigResponse{
		Enabled:      setting.Enabled,
		Group:        setting.Group,
		DefaultModel: setting.DefaultModel,
		MaxTokens:    setting.MaxTokens,
		Temperature:  setting.Temperature,
	})
}

func normalizeLLMChatRequest(req *LLMChatRequest, setting operation_setting.AIAssistantSetting) error {
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		req.Model = setting.DefaultModel
	}
	if req.Model == "" {
		return errors.New("AI assistant model is not configured")
	}

	if len(req.Messages) == 0 {
		return errors.New("AI assistant messages cannot be empty")
	}
	if len(req.Messages) > LLMMaxMessages {
		return fmt.Errorf("AI assistant messages cannot exceed %d", LLMMaxMessages)
	}

	totalChars := 0
	for i := range req.Messages {
		role := strings.TrimSpace(req.Messages[i].Role)
		if role != "user" && role != "assistant" && role != "system" {
			return fmt.Errorf("AI assistant message %d has invalid role %q", i+1, req.Messages[i].Role)
		}
		req.Messages[i].Role = role
		if strings.TrimSpace(req.Messages[i].Content) == "" {
			return fmt.Errorf("AI assistant message %d is empty", i+1)
		}
		totalChars += len(req.Messages[i].Content)
	}
	if totalChars > LLMMaxContentChars {
		return fmt.Errorf("AI assistant messages exceed %d characters", LLMMaxContentChars)
	}

	if req.MaxTokens <= 0 {
		req.MaxTokens = setting.MaxTokens
	}
	if req.MaxTokens > operation_setting.MaxAIAssistantMaxTokens {
		req.MaxTokens = operation_setting.MaxAIAssistantMaxTokens
	}

	if req.Temperature == nil {
		temperature := setting.Temperature
		req.Temperature = &temperature
	}

	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = setting.TimeoutSeconds
	}
	if req.TimeoutSeconds < operation_setting.MinAIAssistantTimeoutSeconds {
		req.TimeoutSeconds = operation_setting.MinAIAssistantTimeoutSeconds
	} else if req.TimeoutSeconds > operation_setting.MaxAIAssistantTimeoutSeconds {
		req.TimeoutSeconds = operation_setting.MaxAIAssistantTimeoutSeconds
	}
	return nil
}

type llmRelayChoice struct {
	Message struct {
		Role             string `json:"role"`
		Content          string `json:"content"`
		ReasoningContent string `json:"reasoning_content"`
		Reasoning        string `json:"reasoning"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type llmRelayResponse struct {
	Model   string           `json:"model"`
	Choices []llmRelayChoice `json:"choices"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type llmRelayErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func parseLLMRelayError(body []byte, status int) error {
	var parsed llmRelayErrorBody
	if err := common.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		if parsed.Error.Type != "" {
			return fmt.Errorf("AI assistant request failed (%s): %s", parsed.Error.Type, parsed.Error.Message)
		}
		return fmt.Errorf("AI assistant request failed: %s", parsed.Error.Message)
	}
	preview := string(body)
	if len(preview) > 300 {
		preview = preview[:300]
	}
	return fmt.Errorf("AI assistant request failed with status %d: %s", status, preview)
}

// LLMChat runs one non-streaming completion through the configured AI
// assistant group. It reuses the regular relay pipeline (channel selection,
// model mapping, retries and logging) through a temporary in-memory token, so
// assistant channels are managed exactly like any other channel.
func LLMChat(ctx context.Context, userID int, req LLMChatRequest) (result *LLMChatResponse, err error) {
	setting := operation_setting.GetAIAssistantSetting()
	if !setting.Enabled {
		return nil, errors.New("AI assistant is not enabled")
	}
	if setting.Group == "" {
		return nil, errors.New("AI assistant group is not configured")
	}
	if err := normalizeLLMChatRequest(&req, setting); err != nil {
		return nil, err
	}

	messages := make([]map[string]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		messages = append(messages, map[string]string{
			"role":    message.Role,
			"content": message.Content,
		})
	}
	payload := map[string]any{
		"model":       req.Model,
		"messages":    messages,
		"stream":      false,
		"max_tokens":  req.MaxTokens,
		"temperature": *req.Temperature,
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to build AI assistant request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
	defer cancel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(ctx)

	userCache, err := model.GetUserCache(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load user %d: %w", userID, err)
	}
	tempToken := &model.Token{
		UserId: userID,
		Name:   "ai-assistant",
		Group:  setting.Group,
	}
	// Run a real Gin handler chain so Distribute's c.Next() invokes Relay
	// with the selected channel and original model already initialized.
	engine := gin.New()
	engine.POST("/pg/chat/completions", func(inner *gin.Context) {
		defer common.CleanupBodyStorage(inner)
		userCache.WriteContext(inner)
		common.SetContextKey(inner, constant.ContextKeyUsingGroup, setting.Group)
		if setupErr := middleware.SetupContextForToken(inner, tempToken); setupErr != nil {
			err = fmt.Errorf("failed to set up AI assistant context: %w", setupErr)
			inner.Abort()
			return
		}
		inner.Next()
	}, middleware.Distribute(), func(inner *gin.Context) {
		Relay(inner, types.RelayFormatOpenAI)
	})

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("AI assistant relay panic: %v", recovered)
			}
		}()
		engine.ServeHTTP(recorder, request)
	}()
	if err != nil {
		return nil, err
	}

	if recorder.Code < http.StatusOK || recorder.Code >= http.StatusMultipleChoices {
		return nil, parseLLMRelayError(recorder.Body.Bytes(), recorder.Code)
	}

	var relayResponse llmRelayResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &relayResponse); err != nil {
		return nil, fmt.Errorf("failed to parse AI assistant response: %w", err)
	}
	if len(relayResponse.Choices) == 0 {
		return nil, errors.New("AI assistant returned no choices")
	}

	response := &LLMChatResponse{
		Content:      relayResponse.Choices[0].Message.Content,
		Reasoning:    relayResponse.Choices[0].Message.ReasoningContent,
		Model:        relayResponse.Model,
		FinishReason: relayResponse.Choices[0].FinishReason,
	}
	if response.Reasoning == "" {
		response.Reasoning = relayResponse.Choices[0].Message.Reasoning
	}
	if relayResponse.Usage != nil {
		response.PromptTokens = relayResponse.Usage.PromptTokens
		response.CompletionTokens = relayResponse.Usage.CompletionTokens
		response.TotalTokens = relayResponse.Usage.TotalTokens
	}
	return response, nil
}
