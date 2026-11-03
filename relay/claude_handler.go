package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

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

	// The prompt-cache skeleton of this attempt's final outbound body is
	// recorded once the attempt ends, success or not.
	var cacheSkeleton *model.CacheSkeleton
	var baselineCoverage *model.CacheSkeleton
	var cacheSkeletonUsage *dto.Usage
	var retainPreviousBreakpoint bool
	_, _, cacheCustomerID, customerDecoded := common.TraceTagFromContext(c)
	if !customerDecoded {
		cacheCustomerID = ""
	}
	defer func() {
		if cacheSkeleton != nil {
			finishCacheSkeleton(c, info, cacheSkeleton, cacheSkeletonUsage, newAPIError)
			if retainPreviousBreakpoint {
				key := claudeCacheBreakpointStateKey(info, cacheSkeleton)
				if key != "" {
					rememberClaudeCacheBreakpointState(cacheCustomerID+"\x1f"+key, cacheSkeleton)
				}
			}
		}
	}()

	var requestBody io.Reader
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if model.CacheSkeletonEnabled() {
			if raw, err := storage.Bytes(); err == nil {
				cacheSkeleton = buildClaudeCacheSkeleton(raw)
			}
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
		// session-id source can be labelled client vs synthesized.
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

		// Resolve cache policies against the final model and prompt after channel
		// and retry rewrites, so retained anchors describe what is actually sent.
		if len(info.ParamOverride) > 0 || len(info.PendingRetryRewrite) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}
		claudeModel = gjson.GetBytes(jsonData, "model").String()
		cachePolicy := resolveClaudeCacheOptimization(
			model.GetCustomerClaudeCacheOptimization(cacheCustomerID), claudeGroup, claudeModel)
		retainPreviousBreakpoint = cachePolicy.RetainPreviousBreakpoint.Enabled

		// Observe a protocol-valid baseline without changing the existing
		// order of transformations applied to the real outbound request.
		if model.CacheSkeletonEnabled() {
			baselineBody, _, baselineErr := prepareClaudeTurnScopedMessages(jsonData)
			if baselineErr == nil {
				baselineCoverage = buildClaudeCacheCoverage(baselineBody)
			}
		}

		// For configured (group, model) rules, move a breakpoint that sits on a
		// volatile trailing reminder onto the last stable block before it. Runs
		// before the forced TTL so a new breakpoint is raised to 1h as well.
		cacheBreakpointFix := ""
		if cachePolicy.FallbackCacheBreakpoint.Enabled {
			var fallbackAdded bool
			jsonData, fallbackAdded, err = addClaudeFallbackCacheBreakpoint(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			if fallbackAdded {
				cacheBreakpointFix = cacheBreakpointAdded
			}
		}
		if cachePolicy.FixVolatileBreakpoint.Enabled {
			var volatileFix string
			jsonData, volatileFix, err = fixClaudeCacheBreakpoints(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			if volatileFix != "" {
				cacheBreakpointFix = volatileFix
			}
		}
		if cachePolicy.StableBaseBreakpoint.Enabled {
			var baseAdded bool
			jsonData, baseAdded, err = addClaudeStableBaseBreakpoint(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			if baseAdded && cacheBreakpointFix == "" {
				cacheBreakpointFix = cacheBreakpointAdded
			}
		}
		if retainPreviousBreakpoint {
			currentSkeleton := buildClaudeCacheSkeleton(jsonData)
			currentSkeleton.SessionId = claudeSessionID(jsonData)
			key := claudeCacheBreakpointStateKey(info, currentSkeleton)
			if key != "" {
				key = cacheCustomerID + "\x1f" + key
			}
			previous := getClaudeCacheBreakpointState(key, time.Now())
			if previous != nil {
				jsonData, _, err = retainClaudeCacheBreakpoints(jsonData, previous)
				if err != nil {
					return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
			}
		}
		// clear_at messages need their beta header and no cache_control on a
		// turn-scoped message; upstream rejects the request otherwise.
		jsonData, hasClearAt, err := prepareClaudeTurnScopedMessages(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if hasClearAt {
			c.Request.Header.Set("anthropic-beta", mergeAnthropicBeta(c.Request.Header.Get("anthropic-beta"), claudeClearAtBeta))
		}

		// For configured (group, model) rules, force the 1-hour prompt-cache TTL:
		// rewrite every existing cache_control breakpoint to ttl=1h and append the
		// extended cache TTL beta header. Only list models that support 1h, else
		// Bedrock returns ValidationException.
		if cachePolicy.ForceExtendedTTL.Enabled {
			jsonData, err = forceClaudeCacheControlTTL(jsonData, claudeExtendedCacheTTL)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			c.Request.Header.Set("anthropic-beta", mergeAnthropicBeta(c.Request.Header.Get("anthropic-beta"), claudeExtendedCacheBeta))
		}

		// Record the final outbound session after param and retry overrides.
		claudeSID := claudeSessionID(jsonData)
		claudeSource := ""
		if claudeSID != "" {
			claudeSource = "metadata.user_id"
			if !clientHadClaudeUserID {
				claudeSource = "content-fallback"
			}
		}
		recordRequestSessionID(c, claudeSID, claudeSource)
		if model.CacheSkeletonEnabled() || retainPreviousBreakpoint {
			cacheSkeleton = buildClaudeCacheSkeleton(jsonData)
			cacheSkeleton.Fixed = cacheBreakpointFix
			cacheSkeleton.SessionId = claudeSID
			if model.CacheSkeletonEnabled() {
				optimizedCoverage := buildClaudeCacheCoverage(jsonData)
				cacheSkeleton.CoverageHash = optimizedCoverage.CoverageHash
				cacheSkeleton.CoverageLen = optimizedCoverage.CoverageLen
				cacheSkeleton.CoverageBpIdx = optimizedCoverage.CoverageBpIdx
				cacheSkeleton.CoverageBpTtl = optimizedCoverage.CoverageBpTtl
				if baselineCoverage != nil {
					cacheSkeleton.BaselineHash = baselineCoverage.CoverageHash
					cacheSkeleton.BaselineLen = baselineCoverage.CoverageLen
					cacheSkeleton.BaselineBpIdx = baselineCoverage.CoverageBpIdx
					cacheSkeleton.BaselineBpTtl = baselineCoverage.CoverageBpTtl
					cacheSkeleton.BaselineParamFp = baselineCoverage.BaselineParamFp
				}
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

	cacheSkeletonUsage, _ = usage.(*dto.Usage)
	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}

// resolveClaudeCacheOptimization turns the customer's complete override into
// independent decisions for this model. Only an absent override inherits globals.
func resolveClaudeCacheOptimization(config *model.CustomerClaudeCacheOptimization, group, modelName string) model.CustomerClaudeCacheOptimization {
	if config == nil {
		fix := operation_setting.ShouldFixCacheBreakpoints(group, modelName)
		return model.CustomerClaudeCacheOptimization{
			ForceExtendedTTL:         model.CustomerClaudeCacheOptimizationRule{Enabled: operation_setting.ShouldForceExtendedCacheTTL(group, modelName)},
			FixVolatileBreakpoint:    model.CustomerClaudeCacheOptimizationRule{Enabled: fix},
			StableBaseBreakpoint:     model.CustomerClaudeCacheOptimizationRule{Enabled: fix},
			RetainPreviousBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: model.CacheSkeletonEnabled()},
		}
	}
	return model.CustomerClaudeCacheOptimization{
		ForceExtendedTTL:         model.CustomerClaudeCacheOptimizationRule{Enabled: config.ForceExtendedTTL.Matches(modelName)},
		FixVolatileBreakpoint:    model.CustomerClaudeCacheOptimizationRule{Enabled: config.FixVolatileBreakpoint.Matches(modelName)},
		StableBaseBreakpoint:     model.CustomerClaudeCacheOptimizationRule{Enabled: config.StableBaseBreakpoint.Matches(modelName)},
		RetainPreviousBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: config.RetainPreviousBreakpoint.Matches(modelName)},
		FallbackCacheBreakpoint:  model.CustomerClaudeCacheOptimizationRule{Enabled: config.FallbackCacheBreakpoint.Matches(modelName)},
	}
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
// header, then the first message (scoped to the API key), then a request-wide
// fallback. Message cache_control breakpoints are not used: clients move them
// onto the newest message every turn, which would split one conversation into
// single-request sessions.
func claudeConversationSeed(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string) string {
	if s := strings.TrimSpace(headerSessionID); s != "" {
		return s
	}
	if s := claudeFirstMessageSeed(jsonData); strings.TrimSpace(s) != "" {
		return apiKeyID + "\x1f" + s
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

// claudeFirstMessageSeed returns the content of the first message, which stays
// fixed for the whole conversation. cache_control is ignored so a breakpoint
// moving on or off the first message does not change the seed.
func claudeFirstMessageSeed(jsonData []byte) string {
	content := gjson.GetBytes(jsonData, "messages.0.content")
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	var sb strings.Builder
	for _, block := range content.Array() {
		if t := block.Get("text"); t.Exists() {
			sb.WriteString(t.String())
			continue
		}
		raw, err := sjson.Delete(block.Raw, "cache_control")
		if err != nil {
			raw = block.Raw
		}
		sb.WriteString(raw)
	}
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
	paths := claudeCacheControlPaths(jsonData)
	if gjson.GetBytes(jsonData, "cache_control").IsObject() {
		paths = append(paths, "cache_control")
	}
	for _, path := range paths {
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
