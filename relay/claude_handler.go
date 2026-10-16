package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

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

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	if request.MaxTokens == nil || *request.MaxTokens == 0 {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
	}

	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(request.Model); ok && effortLevel != "" &&
		(strings.HasPrefix(request.Model, "claude-opus-4-6") ||
			strings.HasPrefix(request.Model, "claude-opus-4-7") ||
			strings.HasPrefix(request.Model, "claude-opus-4-8")) {
		request.Model = baseModel
		request.Thinking = &dto.Thinking{
			Type: "adaptive",
		}
		request.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":"%s"}`, effortLevel))
		if strings.HasPrefix(request.Model, "claude-opus-4-7") ||
			strings.HasPrefix(request.Model, "claude-opus-4-8") {
			// Opus 4.7/4.8 reject non-default temperature/top_p/top_k with 400
			// and defaults display to "omitted"; restore the 4.6 visible summary.
			request.Thinking.Display = "summarized"
			request.Temperature = nil
			request.TopP = nil
			request.TopK = nil
		} else {
			request.Temperature = common.GetPointer[float64](1.0)
		}
		info.UpstreamModelName = request.Model
	} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled &&
		strings.HasSuffix(request.Model, "-thinking") {
		if request.Thinking == nil {
			baseModel := strings.TrimSuffix(request.Model, "-thinking")
			if strings.HasPrefix(baseModel, "claude-opus-4-7") ||
				strings.HasPrefix(baseModel, "claude-opus-4-8") {
				// Opus 4.7/4.8 reject thinking.type="enabled"; use adaptive at high effort.
				request.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
				request.OutputConfig = json.RawMessage(`{"effort":"high"}`)
				request.Temperature = nil
				request.TopP = nil
				request.TopK = nil
			} else {
				// 因为BudgetTokens 必须大于1024
				if request.MaxTokens == nil || *request.MaxTokens < 1280 {
					request.MaxTokens = common.GetPointer[uint](1280)
				}

				// BudgetTokens 为 max_tokens 的 80%
				request.Thinking = &dto.Thinking{
					Type:         "enabled",
					BudgetTokens: common.GetPointer[int](int(float64(*request.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)),
				}
				// TODO: 临时处理
				// https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#important-considerations-when-using-extended-thinking
				request.Temperature = common.GetPointer[float64](1.0)
			}
		}
		if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
			request.Model = strings.TrimSuffix(request.Model, "-thinking")
		}
		info.UpstreamModelName = request.Model
	}

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
		result, convErr := service.ConvertRequest(c, info, types.RelayFormatOpenAI, request)
		if convErr != nil {
			return types.NewError(convErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		openAIRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
		if !ok {
			return types.NewError(fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value), types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		usage, newApiErr := chatCompletionsViaResponses(c, info, adaptor, openAIRequest)
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
		info.UpstreamRequestBodySize = storage.Size()
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
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

		// Resolve the request's using-group (auto cross-group retry resolves to
		// the concrete group) and the outbound model once, for the group+model
		// gated Claude behaviors below.
		claudeGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		if claudeGroup == "auto" {
			if ag := common.GetContextKeyString(c, constant.ContextKeyAutoGroup); ag != "" {
				claudeGroup = ag
			}
		}
		claudeModel := gjson.GetBytes(jsonData, "model").String()

		// Remember whether the client supplied its own metadata.user_id, so the
		// session-id source can be labelled client ("metadata.user_id") vs our
		// synthesized value ("content-fallback").
		clientHadClaudeUserID := strings.TrimSpace(gjson.GetBytes(jsonData, "metadata.user_id").String()) != ""

		// Metadata.user_id synthesis defaults ON for every (group, model); it is
		// skipped only for (group, model) pairs listed in
		// metadata_injection_disable_setting. Synthesizes a Claude Code-style
		// metadata.user_id (device_id = API key suffix + embedded derived
		// session_id) when the client omitted user_id; clients that already set
		// user_id (e.g. Claude Code) are left untouched.
		if !operation_setting.MatchGroupModel(operation_setting.GetMetadataInjectionDisableSetting().Rules, claudeGroup, claudeModel) {
			jsonData, err = fillClaudeMetadataUserID(
				jsonData,
				common.GetContextKeyString(c, constant.ContextKeyTokenKey),
				c.GetHeader("x-claude-code-session-id"),
				c.ClientIP(),
				c.GetHeader("User-Agent"),
			)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
		}

		// For configured (group, model) rules, force the 1-hour prompt-cache TTL:
		// rewrite every existing cache_control breakpoint to ttl=1h and append the
		// extended cache TTL beta header. Only list models that support 1h, else
		// Bedrock returns ValidationException.
		if operation_setting.ShouldForceExtendedCacheTTL(claudeGroup, claudeModel) {
			jsonData, err = forceClaudeCacheControlTTL(jsonData, claudeExtendedCacheTTL)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			c.Request.Header.Set("anthropic-beta", mergeAnthropicBeta(c.Request.Header.Get("anthropic-beta"), claudeExtendedCacheBeta))
		}

		// Apply param override and the one-shot retry-override rewrite. Both are
		// handled inside ApplyParamOverrideWithRelayInfo, so call it whenever
		// either is present (a staged retry rewrite may exist without any param
		// override configured).
		if len(info.ParamOverride) > 0 || len(info.PendingRetryRewrite) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		// Capture the resolved session_id (client-supplied or injected) from the
		// final metadata.user_id blob, labelling its source for the log detail.
		claudeSID := claudeSessionID(jsonData)
		claudeSource := ""
		if claudeSID != "" {
			claudeSource = "metadata.user_id"
			if !clientHadClaudeUserID {
				claudeSource = "content-fallback"
			}
		}
		recordRequestSessionID(c, claudeSID, claudeSource)

		logger.LogDebug(c, "requestBody: %s", jsonData)
		body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		jsonData = nil
		info.UpstreamRequestBodySize = size
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

// fillClaudeMetadataUserID synthesizes a Claude Code-style metadata.user_id for
// clients that did not provide one. The blob embeds device_id (the trailing
// suffix of the request's API key) and a derived session_id, mirroring the exact
// shape Claude Code sends — which the upstream accepts. The session_id is NOT
// written as a top-level metadata.session_id, because the Anthropic metadata
// schema only allows user_id and upstream rejects extra metadata keys. The body
// is returned unchanged when a non-empty user_id is already present (e.g. real
// Claude Code, which carries its own session) or when no key suffix is available.
func fillClaudeMetadataUserID(jsonData []byte, apiKey, headerSessionID, clientIP, userAgent string) ([]byte, error) {
	if existing := gjson.GetBytes(jsonData, "metadata.user_id"); existing.Exists() && strings.TrimSpace(existing.String()) != "" {
		return jsonData, nil
	}
	if len(apiKey) < claudeDerivedDeviceIDLen {
		return jsonData, nil
	}
	suffix := apiKey[len(apiKey)-claudeDerivedDeviceIDLen:]
	sessionID := claudeSessionHash(deriveClaudeSessionSeed(jsonData, headerSessionID, clientIP, userAgent, apiKey))
	encoded, err := common.Marshal(claudeMetadataUserID{
		DeviceID:  suffix,
		SessionID: sessionID,
	})
	if err != nil {
		return jsonData, err
	}
	return sjson.SetBytes(jsonData, "metadata.user_id", string(encoded))
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

const (
	// claudeExtendedCacheTTL is the 1-hour prompt-cache TTL value.
	claudeExtendedCacheTTL = "1h"
	// claudeExtendedCacheBeta is the anthropic-beta token enabling the 1h cache.
	claudeExtendedCacheBeta = "extended-cache-ttl-2025-04-11"
)

// forceClaudeCacheControlTTL rewrites the ttl of every existing cache_control
// breakpoint (in system, tools and message content blocks) to the given value.
// It does not create new breakpoints; blocks without cache_control are left
// untouched. The block's existing "type" (ephemeral) is preserved.
func forceClaudeCacheControlTTL(jsonData []byte, ttl string) ([]byte, error) {
	var err error
	for _, path := range claudeCacheControlPaths(jsonData) {
		jsonData, err = sjson.SetBytes(jsonData, path+".ttl", ttl)
		if err != nil {
			return jsonData, err
		}
	}
	return jsonData, nil
}

// claudeCacheControlPaths returns the sjson paths of every cache_control object
// in the request (system/tools blocks and each message's content blocks).
func claudeCacheControlPaths(jsonData []byte) []string {
	var paths []string
	collect := func(prefix string, blocks gjson.Result) {
		if !blocks.IsArray() {
			return
		}
		blocks.ForEach(func(idx, block gjson.Result) bool {
			if block.Get("cache_control").IsObject() {
				paths = append(paths, fmt.Sprintf("%s.%d.cache_control", prefix, idx.Int()))
			}
			return true
		})
	}
	collect("system", gjson.GetBytes(jsonData, "system"))
	collect("tools", gjson.GetBytes(jsonData, "tools"))
	gjson.GetBytes(jsonData, "messages").ForEach(func(i, msg gjson.Result) bool {
		collect(fmt.Sprintf("messages.%d.content", i.Int()), msg.Get("content"))
		return true
	})
	return paths
}

// mergeAnthropicBeta appends beta to a comma-separated anthropic-beta value,
// keeping existing tokens and avoiding duplicates.
func mergeAnthropicBeta(existing, beta string) string {
	if strings.Contains(existing, beta) {
		return existing
	}
	if existing == "" {
		return beta
	}
	return existing + "," + beta
}
