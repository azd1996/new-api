package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// maybeApplyRetryRuleFallback drives the channel's retry-override: it evaluates
// each configured operation's conditions against the just-failed upstream
// response ({status_code, error_message}), and on a match arranges a single
// error-triggered retry and returns true so the caller continues the loop:
//
//   - stages the matching operations (with conditions/action stripped) as the
//     one-shot request rewrite applied by the relay handler on the next attempt;
//   - the resolved action decides where the rewritten request is retried:
//   - retry_same_channel (default for non-200): soft-pin the same channel and
//     grant a one-shot retry via ResetRetryNextTry (does not consume RetryTimes
//     budget); guarded one-shot per request via RetryFallbackDone.
//   - fallback_next_channel: neither pin nor reset, so the loop advances to the
//     next channel; multi-hop (not one-shot), bounded by RetryTimes and channel
//     availability. A 200-body trigger is always coerced to this action.
func maybeApplyRetryRuleFallback(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, apiErr *types.NewAPIError, relayFormat types.RelayFormat, retryParam *service.RetryParam) bool {
	if info == nil || channel == nil || apiErr == nil || info.RetryFallbackDone {
		logger.LogDebug(c, fmt.Sprintf("retry-override: hook skipped by guard (info=%t channel=%t apiErr=%t fallbackDone=%t)",
			info != nil, channel != nil, apiErr != nil, info != nil && info.RetryFallbackDone))
		return false
	}
	ops := channel.GetSetting().RetryOverride
	if len(ops) == 0 {
		// The channel handed in by the relay loop can be a stub carrying only
		// Id/Type/Name/AutoBan (getChannel's ChannelMeta==nil branch), so its
		// Setting is empty. Re-read the full channel to get retry_override.
		if full, err := model.CacheGetChannel(channel.Id); err == nil && full != nil {
			ops = full.GetSetting().RetryOverride
		}
	}
	logger.LogDebug(c, fmt.Sprintf("retry-override: hook reached on channel #%d, configured operations=%d", channel.Id, len(ops)))
	if len(ops) == 0 {
		return false
	}
	reqCtx := relaycommon.BuildParamOverrideContext(info)
	if reqCtx == nil {
		reqCtx = map[string]any{}
	}
	reqCtx["relay_format"] = string(relayFormat)
	respCtx := retryrule.ResponseContext(apiErr.StatusCode, apiErr.Error(), string(relayFormat))
	if m, ok := reqCtx["model"]; ok {
		respCtx["model"] = m
	}
	logger.LogDebug(c, fmt.Sprintf("retry-override: evaluating %d rule(s) on channel #%d, status_code=%d, error_message=%q",
		len(ops), channel.Id, apiErr.StatusCode, apiErr.Error()))
	rewrites, matched, action, ruleIDs := retryrule.CollectRewrites(ops, reqCtx, respCtx)
	if !matched {
		logger.LogDebug(c, fmt.Sprintf("retry-override: no operation matched on channel #%d, not retrying", channel.Id))
		return false
	}
	if data, err := common.Marshal(rewrites); err == nil {
		logger.LogDebug(c, fmt.Sprintf("retry-override: matched (action=%s), staging %d rewrite(s) on channel #%d: %s",
			action, len(rewrites), channel.Id, string(data)))
	} else {
		logger.LogDebug(c, fmt.Sprintf("retry-override: matched (action=%s), staging %d rewrite(s) on channel #%d",
			action, len(rewrites), channel.Id))
	}
	info.PendingRetryRewrite = rewrites
	recordRetryOverrideAudit(c, info, channel.Id, apiErr, relayFormat, action, ruleIDs, ruleDescriptions(ops, ruleIDs), rewrites)
	if action == retryrule.ActionFallbackNextChannel {
		// Fallback is multi-hop: do NOT set the one-shot RetryFallbackDone, and do
		// NOT pin or reset the budget. The normal loop's IncreaseRetry advances the
		// priority tier so getChannel selects the next channel; it re-triggers on
		// each channel until one succeeds or channels/RetryTimes are exhausted.
		// Returning true still bypasses the "not retryable" gate (incl. 200-body).
		logger.LogDebug(c, fmt.Sprintf("retry-override: handing off channel #%d to next-channel fallback", channel.Id))
		return true
	}
	// Same-channel retry is one-shot: pin the channel and grant a retry that does
	// not consume budget. RetryFallbackDone prevents looping the same channel.
	info.RetryFallbackDone = true
	service.SetPinnedChannel(c, channel.Id)
	retryParam.ResetRetryNextTry()
	return true
}

// overrideAuditParams builds the request-identity for an override-audit log from
// the current relay context. Shared by the retry-override and param-override
// audit log call sites.
func overrideAuditParams(c *gin.Context, info *relaycommon.RelayInfo, channelId int) model.OverrideAuditLogParams {
	group := info.UsingGroup
	if group == "" {
		group = info.UserGroup
	}
	return model.OverrideAuditLogParams{
		UserId:            info.UserId,
		ChannelId:         channelId,
		ModelName:         info.OriginModelName,
		TokenName:         c.GetString("token_name"),
		TokenId:           info.TokenId,
		Group:             group,
		RequestId:         info.RequestId,
		UpstreamRequestId: c.GetString(common.UpstreamRequestIdKey),
		IsStream:          info.IsStream,
	}
}

// recordRetryOverrideAudit writes an admin-only retry-override audit log
// (LogTypeRetryOverride) when a rule fully matched and triggered a retry/fallback.
// ruleIDs are the indices (into the channel's retry_override array) of every
// matched rule; descriptions are the corresponding rule descriptions (aligned by
// position, empty when a rule has none).
func recordRetryOverrideAudit(c *gin.Context, info *relaycommon.RelayInfo, channelId int, apiErr *types.NewAPIError, relayFormat types.RelayFormat, action string, ruleIDs []int, descriptions []string, rewrites []map[string]any) {
	detail := map[string]interface{}{
		"action":         action,
		"rule_ids":       ruleIDs,
		"status_code":    apiErr.StatusCode,
		"relay_format":   string(relayFormat),
		"rewrites_count": len(rewrites),
	}
	if hasNonEmpty(descriptions) {
		detail["rule_descriptions"] = descriptions
	}
	if msg := apiErr.Error(); msg != "" {
		detail["error_message"] = common.LocalLogPreview(msg)
	}
	if summaries := summarizeRewriteOps(rewrites); len(summaries) > 0 {
		detail["rewrites"] = summaries
	}
	model.RecordRetryOverrideLog(overrideAuditParams(c, info, channelId), "retry override matched", detail)
}

// ruleDescriptions maps matched rule indices to their configured descriptions,
// aligned by position with ruleIDs (empty string when a rule has none).
func ruleDescriptions(ops []dto.RetryRule, ruleIDs []int) []string {
	out := make([]string, 0, len(ruleIDs))
	for _, idx := range ruleIDs {
		if idx >= 0 && idx < len(ops) {
			out = append(out, ops[idx].Description)
		} else {
			out = append(out, "")
		}
	}
	return out
}

func hasNonEmpty(values []string) bool {
	for _, v := range values {
		if v != "" {
			return true
		}
	}
	return false
}

// recordParamOverrideAudit writes an admin-only param-override audit log
// (LogTypeParamOverride) for the current attempt when the channel's static
// param-override operations matched and ran. It consumes and clears
// info.ParamOverrideMatched so each attempt is recorded at most once.
func recordParamOverrideAudit(c *gin.Context, info *relaycommon.RelayInfo, channelId int) {
	if info == nil || len(info.ParamOverrideMatched) == 0 {
		return
	}
	applied := info.ParamOverrideMatched
	ruleIDs := info.ParamOverrideMatchedIndices
	descriptions := info.ParamOverrideMatchedDescriptions
	info.ParamOverrideMatched = nil
	info.ParamOverrideMatchedIndices = nil
	info.ParamOverrideMatchedDescriptions = nil
	detail := map[string]interface{}{
		"relay_format": string(info.RelayFormat),
		"retry_index":  info.RetryIndex,
		"rule_ids":     ruleIDs,
		"count":        len(applied),
		"applied":      applied,
	}
	if len(descriptions) > 0 {
		detail["descriptions"] = descriptions
	}
	model.RecordParamOverrideLog(overrideAuditParams(c, info, channelId), "param override applied", detail)
}

// summarizeRewriteOps renders staged retry-override rewrite operations as compact
// "mode path" summaries for the audit log.
func summarizeRewriteOps(rewrites []map[string]any) []string {
	out := make([]string, 0, len(rewrites))
	for _, rw := range rewrites {
		mode, _ := rw["mode"].(string)
		if mode == "" {
			continue
		}
		if path, _ := rw["path"].(string); path != "" {
			out = append(out, mode+" "+path)
			continue
		}
		out = append(out, mode)
	}
	return out
}
