package relay

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	// gatewayPromptCacheKeyPrefix marks prompt_cache_key values the gateway
	// derived, so they are told apart from client keys in logs.
	gatewayPromptCacheKeyPrefix = "gw-"
	// gatewayPromptCacheKeySource is the session-id source label for them.
	gatewayPromptCacheKeySource = "gateway.prompt_cache_key"
)

// shouldInjectPromptCacheKey reports whether an OpenAI request without a
// prompt_cache_key gets a derived one, by the request's using-group (auto
// cross-group retry resolves to the concrete group) and client model.
func shouldInjectPromptCacheKey(c *gin.Context, modelName string) bool {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		if autoGroup := common.GetContextKeyString(c, constant.ContextKeyAutoGroup); autoGroup != "" {
			group = autoGroup
		}
	}
	return operation_setting.ShouldInjectPromptCacheKey(group, modelName)
}

// deriveOpenAIPromptCacheKey builds a stable routing key for one conversation
// from the model, the API key and a conversation anchor that stays fixed for
// every turn. OpenAI routes requests by prefix and key, so one key per
// conversation keeps its turns on the machine holding the cached prefix.
// Returns "" without an anchor or API key.
func deriveOpenAIPromptCacheKey(modelName, apiKey, anchor string) string {
	if strings.TrimSpace(anchor) == "" || apiKey == "" {
		return ""
	}
	return gatewayPromptCacheKeyPrefix + claudeSessionHash(modelName+"\x1f"+apiKey+"\x1f"+anchor)
}

// chatPromptCacheAnchor is the first two messages (normally the system prompt
// and the first user turn), which never change within a conversation.
func chatPromptCacheAnchor(req *dto.GeneralOpenAIRequest) string {
	var sb strings.Builder
	for _, msg := range req.Messages[:min(2, len(req.Messages))] {
		raw, err := common.Marshal(msg)
		if err != nil {
			return ""
		}
		sb.Write(raw)
	}
	return sb.String()
}

// responsesPromptCacheAnchor prefers the client's own session id
// (client_metadata.session_id, sent by Codex), else the instructions and the
// first input item.
func responsesPromptCacheAnchor(req *dto.OpenAIResponsesRequest) string {
	if len(req.ClientMetadata) > 0 {
		if sid := strings.TrimSpace(gjson.GetBytes(req.ClientMetadata, "session_id").String()); sid != "" {
			return sid
		}
	}
	input := gjson.ParseBytes(req.Input)
	first := input.Raw
	if input.IsArray() {
		first = input.Get("0").Raw
	}
	if first == "" {
		return ""
	}
	return string(req.Instructions) + "\x1f" + first
}
