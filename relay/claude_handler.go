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
				// Pass-through mode forwards the body unmodified: none of the
				// cache-optimization rules below ran. Flag it only when a
				// policy was actually configured, so an operator comparing
				// this customer's Fixed column against their configured
				// rules does not read silence as "nothing to do".
				policy := resolveClaudeCacheOptimization(
					model.GetCustomerClaudeCacheOptimization(cacheCustomerID), claudeRequestGroup(c), cacheSkeleton.Model)
				if claudeCacheOptimizationConfigured(policy) {
					cacheSkeleton.Fixed = cacheBreakpointPassThrough
				}
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
		claudeGroup := claudeRequestGroup(c)
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
			if prepared := common.GetContextKeyString(c, constant.ContextKeyClaudeMetadataUserID); !clientHadClaudeUserID && prepared != "" {
				jsonData, err = sjson.SetBytes(jsonData, "metadata.user_id", prepared)
			} else {
				jsonData, err = fillClaudeMetadataUserID(
					jsonData,
					common.GetContextKeyString(c, constant.ContextKeyTokenKey),
					c.GetHeader("x-claude-code-session-id"),
					c.ClientIP(),
					c.GetHeader("User-Agent"),
				)
			}
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
		// Each step below is a best-effort optimization: the request is already
		// protocol-valid without it, so a step's own error is logged and the
		// step is skipped rather than failing the whole request.
		cacheBreakpointFix := ""
		if cachePolicy.FallbackCacheBreakpoint.Enabled {
			if out, fallbackAdded, ferr := addClaudeFallbackCacheBreakpoint(jsonData); ferr != nil {
				logClaudeCacheStepError(c, "fallback_cache_breakpoint", ferr)
			} else {
				jsonData = out
				if fallbackAdded {
					cacheBreakpointFix = cacheBreakpointAdded
				}
			}
		}
		if cachePolicy.FixVolatileBreakpoint.Enabled {
			if out, volatileFix, ferr := fixClaudeCacheBreakpoints(jsonData); ferr != nil {
				logClaudeCacheStepError(c, "fix_volatile_breakpoint", ferr)
			} else {
				jsonData = out
				if volatileFix != "" {
					cacheBreakpointFix = volatileFix
				}
			}
		}
		if cachePolicy.StableBaseBreakpoint.Enabled {
			if out, baseAdded, ferr := addClaudeStableBaseBreakpoint(jsonData); ferr != nil {
				logClaudeCacheStepError(c, "stable_base_breakpoint", ferr)
			} else {
				jsonData = out
				if baseAdded && cacheBreakpointFix == "" {
					cacheBreakpointFix = cacheBreakpointAdded
				}
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
				if out, _, ferr := retainClaudeCacheBreakpoints(jsonData, previous); ferr != nil {
					logClaudeCacheStepError(c, "retain_previous_breakpoint", ferr)
				} else {
					jsonData = out
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

// logClaudeCacheStepError records a prompt-cache optimization step's failure.
// These steps are best-effort: the request is already protocol-valid before
// they run, so a step's error is logged and the step is skipped rather than
// failing the whole request.
func logClaudeCacheStepError(c *gin.Context, step string, err error) {
	logger.LogError(c, common.LogText("claude cache optimization step %s failed: %s", step, err.Error()))
}

// claudeRequestGroup resolves the request's using-group for group+model gated
// Claude behaviors: auto cross-group retry resolves to the concrete group.
func claudeRequestGroup(c *gin.Context) string {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		if ag := common.GetContextKeyString(c, constant.ContextKeyAutoGroup); ag != "" {
			group = ag
		}
	}
	return group
}

// resolveClaudeCacheOptimization turns the customer's complete override into
// independent decisions for this model. Only an absent override inherits
// globals, and each of the five rules has its own independent global setting
// (operation_setting.Should*): an operator enabling one does not implicitly
// enable another.
func resolveClaudeCacheOptimization(config *model.CustomerClaudeCacheOptimization, group, modelName string) model.CustomerClaudeCacheOptimization {
	if config == nil {
		return model.CustomerClaudeCacheOptimization{
			ForceExtendedTTL:         model.CustomerClaudeCacheOptimizationRule{Enabled: operation_setting.ShouldForceExtendedCacheTTL(group, modelName)},
			FixVolatileBreakpoint:    model.CustomerClaudeCacheOptimizationRule{Enabled: operation_setting.ShouldFixCacheBreakpoints(group, modelName)},
			StableBaseBreakpoint:     model.CustomerClaudeCacheOptimizationRule{Enabled: operation_setting.ShouldUseStableBaseBreakpoint(group, modelName)},
			RetainPreviousBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: operation_setting.ShouldRetainPreviousBreakpoint(group, modelName)},
			FallbackCacheBreakpoint:  model.CustomerClaudeCacheOptimizationRule{Enabled: operation_setting.ShouldUseFallbackCacheBreakpoint(group, modelName)},
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

// claudeCacheOptimizationConfigured reports whether any of the five rules is
// enabled, i.e. whether skipping them (pass-through mode) actually skips
// configured behavior rather than a no-op.
func claudeCacheOptimizationConfigured(policy model.CustomerClaudeCacheOptimization) bool {
	return policy.ForceExtendedTTL.Enabled || policy.FixVolatileBreakpoint.Enabled ||
		policy.StableBaseBreakpoint.Enabled || policy.RetainPreviousBreakpoint.Enabled ||
		policy.FallbackCacheBreakpoint.Enabled
}

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
	out, _, err := common.PrepareClaudeMetadataUserID(jsonData, apiKey, headerSessionID, clientIP, userAgent)
	return out, err
}

// claudeSessionHash reduces a session seed to a 16-char xxhash64 hex string.
// An empty (after trim) seed yields an empty hash so callers can skip writing.
func claudeSessionHash(seed string) string {
	return common.ClaudeSessionHash(seed)
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
