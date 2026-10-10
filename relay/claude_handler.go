package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func ClaudeHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {

	info.InitChannelMeta(c)

	claudeReq, ok := info.Request.(*dto.ClaudeRequest)

	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.ClaudeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(claudeReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ClaudeRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ApplyReasoningModelSuffix(c, info, request); err != nil {
		return newConvertRequestFailedError(c, info, err)
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	if info.ChannelSetting.SystemPrompt != "" {
		if request.System == nil {
			request.SetStringSystem(info.ChannelSetting.SystemPrompt)
		} else if info.ChannelSetting.SystemPromptOverride {
			common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
			if request.IsStringSystem() {
				existing := strings.TrimSpace(request.GetStringSystem())
				if existing == "" {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt)
				} else {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt + "\n" + existing)
				}
			} else {
				systemContents := request.ParseSystem()
				newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
				newSystem.SetText(info.ChannelSetting.SystemPrompt)
				if len(systemContents) == 0 {
					request.System = []dto.ClaudeMediaMessage{newSystem}
				} else {
					request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
				}
			}
		}
	}

	if !model_setting.GetGlobalSettings().PassThroughRequestEnabled &&
		!info.ChannelSetting.PassThroughBodyEnabled &&
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
		usage, newApiErr := textRequestViaResponses(c, info, adaptor, request)
		if newApiErr != nil {
			return newApiErr
		}

		service.PostTextConsumeQuota(c, info, usage, nil)
		return nil
	}

	var requestBody io.Reader
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.NewReplayableBodyReader(storage)
	} else {
		convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			return newConvertRequestFailedError(c, info, err)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for Claude API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// For clients that did not provide metadata.user_id, derive and fill
		// metadata.session_id so downstream gateways can perform session
		// stickiness. Clients that already set user_id (e.g. Claude Code) carry
		// their own session identity and are left untouched. Done before user_id
		// synthesis so the synthesized user_id can reuse the same session_id.
		jsonData, err = fillClaudeMetadataSessionID(
			jsonData,
			c.GetHeader("x-claude-code-session-id"),
			c.ClientIP(),
			c.GetHeader("User-Agent"),
			common.GetContextKeyString(c, constant.ContextKeyTokenKey),
		)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// Fill metadata.user_id with a Claude Code-style blob when the client
		// omitted it, reusing metadata.session_id as the blob's session_id.
		jsonData, err = fillClaudeMetadataUserID(jsonData, common.GetContextKeyString(c, constant.ContextKeyTokenKey))
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		logger.LogDebug(c, "requestBody: %s", jsonData)
		body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		jsonData = nil
		requestBody = body
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}

// claudeDerivedDeviceIDLen is the number of trailing API-key characters used as
// the device_id when synthesizing a metadata.user_id for clients that omit it.
const claudeDerivedDeviceIDLen = 6

// claudeMetadataUserID mirrors the JSON shape Claude Code packs into
// metadata.user_id (a JSON string embedding device_id / account_uuid /
// session_id). Synthesizing the same shape lets downstream gateways parse our
// injected value with the exact logic they already use for real Claude Code
// traffic.
type claudeMetadataUserID struct {
	DeviceID    string `json:"device_id"`
	AccountUUID string `json:"account_uuid"`
	SessionID   string `json:"session_id"`
}

// fillClaudeMetadataUserID sets metadata.user_id to a Claude Code-style JSON
// string whose device_id is the trailing suffix of the request's API key, but
// only when the client did not already provide a user_id. The blob's session_id
// reuses the current metadata.session_id so the two stay identical. The body is
// returned unchanged when a non-empty user_id is already present or no suffix is
// available. Any existing metadata siblings (e.g. session_id) are preserved.
func fillClaudeMetadataUserID(jsonData []byte, apiKey string) ([]byte, error) {
	if existing := gjson.GetBytes(jsonData, "metadata.user_id"); existing.Exists() && strings.TrimSpace(existing.String()) != "" {
		return jsonData, nil
	}
	if len(apiKey) < claudeDerivedDeviceIDLen {
		return jsonData, nil
	}
	suffix := apiKey[len(apiKey)-claudeDerivedDeviceIDLen:]
	encoded, err := common.Marshal(claudeMetadataUserID{
		DeviceID:  suffix,
		SessionID: gjson.GetBytes(jsonData, "metadata.session_id").String(),
	})
	if err != nil {
		return jsonData, err
	}
	return sjson.SetBytes(jsonData, "metadata.user_id", string(encoded))
}

// fillClaudeMetadataSessionID derives a session hash and writes it to
// metadata.session_id, gated on the client NOT having provided metadata.user_id.
// Clients that set user_id (e.g. Claude Code) already carry their own session
// identity inside it, so their metadata is left untouched; only clients missing
// user_id get a synthesized session_id. An explicit session_id already present
// is preserved. The seed combines the model and the stable system prefix with a
// conversation identity (the x-claude-code-session-id header, then ephemeral
// cache_control content, then a client IP + user agent + API key fallback),
// reduced to a 16-char xxhash64 hex matching the session-affinity gateway shape.
func fillClaudeMetadataSessionID(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string) ([]byte, error) {
	if uid := gjson.GetBytes(jsonData, "metadata.user_id"); uid.Exists() && strings.TrimSpace(uid.String()) != "" {
		return jsonData, nil
	}
	if existing := gjson.GetBytes(jsonData, "metadata.session_id"); existing.Exists() && strings.TrimSpace(existing.String()) != "" {
		return jsonData, nil
	}
	hash := claudeSessionHash(deriveClaudeSessionSeed(jsonData, headerSessionID, clientIP, userAgent, apiKeyID))
	if hash == "" {
		return jsonData, nil
	}
	return sjson.SetBytes(jsonData, "metadata.session_id", hash)
}

// claudeSessionHash reduces a session seed to a 16-char xxhash64 hex string.
// An empty (after trim) seed yields an empty hash so callers can skip writing.
func claudeSessionHash(seed string) string {
	normalized := strings.TrimSpace(seed)
	if normalized == "" {
		return ""
	}
	return fmt.Sprintf("%016x", xxhash.Sum64String(normalized))
}

// deriveClaudeSessionSeed builds the session seed as model + stable system
// prefix + conversation identity, so the resulting session_id changes exactly
// when the upstream prompt cache would be invalidated (model switch or a change
// in the cached system prefix) and stays stable otherwise.
func deriveClaudeSessionSeed(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string) string {
	conversation := claudeConversationSeed(jsonData, headerSessionID, clientIP, userAgent, apiKeyID)
	model := gjson.GetBytes(jsonData, "model").String()
	systemPrefix := claudeStableSystemPrefix(jsonData)
	if model == "" && systemPrefix == "" && conversation == "" {
		return ""
	}
	// \x1f (unit separator) keeps the three factors unambiguously delimited.
	return strings.Join([]string{model, systemPrefix, conversation}, "\x1f")
}

// claudeConversationSeed picks the conversation identity by priority: session
// header, then ephemeral cache_control content, then a request-wide fallback.
func claudeConversationSeed(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string) string {
	if s := strings.TrimSpace(headerSessionID); s != "" {
		return s
	}
	if s := claudeCacheControlSeed(jsonData); strings.TrimSpace(s) != "" {
		return s
	}
	return claudeFallbackSessionSeed(clientIP, userAgent, apiKeyID)
}

// claudeStableSystemPrefix returns the cached-stable portion of the system
// prompt: the concatenation of system blocks up to and including the first one
// carrying an ephemeral cache_control breakpoint. The dynamic tail after the
// breakpoint is intentionally excluded so a changing tail does not flip the
// session. When no breakpoint is present it falls back to the first system
// block; a plain-string system is used verbatim.
func claudeStableSystemPrefix(jsonData []byte) string {
	sys := gjson.GetBytes(jsonData, "system")
	if sys.Type == gjson.String {
		return sys.String()
	}
	if !sys.IsArray() {
		return ""
	}
	blocks := sys.Array()
	var sb strings.Builder
	for _, block := range blocks {
		if t := block.Get("text"); t.Exists() {
			sb.WriteString(t.String())
		} else {
			sb.WriteString(block.Raw)
		}
		if block.Get("cache_control.type").String() == "ephemeral" {
			return sb.String()
		}
	}
	if len(blocks) > 0 {
		if t := blocks[0].Get("text"); t.Exists() {
			return t.String()
		}
		return blocks[0].Raw
	}
	return ""
}

// claudeCacheControlSeed concatenates the text of system/message blocks that
// carry an ephemeral cache_control breakpoint — the stable cached prefix of a
// conversation.
func claudeCacheControlSeed(jsonData []byte) string {
	var sb strings.Builder
	collect := func(blocks gjson.Result) {
		blocks.ForEach(func(_, block gjson.Result) bool {
			if block.Get("cache_control.type").String() == "ephemeral" {
				if t := block.Get("text"); t.Exists() {
					sb.WriteString(t.String())
				} else {
					sb.WriteString(block.Raw)
				}
			}
			return true
		})
	}
	if sys := gjson.GetBytes(jsonData, "system"); sys.IsArray() {
		collect(sys)
	}
	gjson.GetBytes(jsonData, "messages").ForEach(func(_, msg gjson.Result) bool {
		if content := msg.Get("content"); content.IsArray() {
			collect(content)
		}
		return true
	})
	return sb.String()
}

// claudeFallbackSessionSeed builds the last-resort seed from request identity
// only (client IP + user agent + API key). Prompt text is intentionally
// excluded so the seed stays stable across the turns of one conversation.
func claudeFallbackSessionSeed(clientIP, userAgent, apiKeyID string) string {
	return clientIP + userAgent + apiKeyID
}
