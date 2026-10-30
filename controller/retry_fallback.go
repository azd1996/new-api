package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// retryOverrideTerminate carries a return_error decision back to the relay loop.
// It is applied to the response error only AFTER the attempt's error log is
// written, so logs/metrics keep the original upstream status/message while the
// caller receives the configured override.
type retryOverrideTerminate struct {
	StatusCode int
	Message    string
}

// maybeApplyRetryRuleFallback drives the channel's retry-override: it evaluates
// the configured rules against the just-failed upstream response, and on a match
// of the first (priority-ordered) rule arranges the next step. It returns
// (retryTriggered, terminate, auditDetail):
//
//   - retry_same_channel (default): soft-pin the same channel and grant a
//     one-shot retry via ResetRetryNextTry (does not consume RetryTimes budget);
//     guarded one-shot per request via RetryFallbackDone. retryTriggered=true.
//   - fallback_next_channel: neither pin nor reset, so the loop advances to the
//     next channel; multi-hop (not one-shot), bounded by RetryTimes and channel
//     availability. retryTriggered=true.
//   - return_error: stop retrying entirely. terminate is non-nil and carries the
//     configured status/message; it is NOT applied to apiErr here, so the error
//     log records the original upstream status/message. The caller logs the
//     attempt, then applies terminate to the response error and breaks.
func maybeApplyRetryRuleFallback(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, apiErr *types.NewAPIError, relayFormat types.RelayFormat, retryParam *service.RetryParam) (bool, *retryOverrideTerminate, map[string]interface{}) {
	if info == nil || channel == nil || apiErr == nil || info.RetryFallbackDone {
		logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: hook skipped by guard (info=%t channel=%t apiErr=%t fallbackDone=%t)",
			info != nil, channel != nil, apiErr != nil, info != nil && info.RetryFallbackDone))
		return false, nil, nil
	}
	chSetting := channel.GetSetting()
	ops := chSetting.RetryOverride
	refs := chSetting.RetryOverrideTemplateRefs
	if len(ops) == 0 && len(refs) == 0 {
		// The channel handed in by the relay loop can be a stub carrying only
		// Id/Type/Name/AutoBan (getChannel's ChannelMeta==nil branch), so its
		// Setting is empty. Re-read the full channel to get retry_override.
		if full, err := model.CacheGetChannel(channel.Id); err == nil && full != nil {
			fullSetting := full.GetSetting()
			ops = fullSetting.RetryOverride
			refs = fullSetting.RetryOverrideTemplateRefs
		}
	}
	// Prepend rules contributed by referenced shared templates (selection order),
	// so the effective list is [template rules..., channel own rules...].
	if templateRules := service.ResolveRetryOverrideTemplates(refs); len(templateRules) > 0 {
		ops = append(templateRules, ops...)
	}
	logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: hook reached on channel #%d, configured operations=%d", channel.Id, len(ops)))
	if len(ops) == 0 {
		return false, nil, nil
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
	logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: evaluating %d rule(s) on channel #%d, status_code=%d, error_message=%q",
		len(ops), channel.Id, apiErr.StatusCode, apiErr.Error()))
	decision := retryrule.CollectRewrites(ops, reqCtx, respCtx)
	if !decision.Matched {
		logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: no rule matched on channel #%d, not retrying", channel.Id))
		return false, nil, nil
	}
	if data, err := common.Marshal(decision.Rewrites); err == nil {
		logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: matched rule #%d (action=%s), staging %d rewrite(s) on channel #%d: %s",
			decision.MatchedIndex, decision.Action, len(decision.Rewrites), channel.Id, string(data)))
	} else {
		logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: matched rule #%d (action=%s), staging %d rewrite(s) on channel #%d",
			decision.MatchedIndex, decision.Action, len(decision.Rewrites), channel.Id))
	}
	// apiErr is intentionally NOT mutated here: the audit (below) and the caller's
	// error log must record the original upstream status/message.
	detail := recordRetryOverrideAudit(c, info, channel.Id, apiErr, relayFormat, decision, ruleDescriptions(ops, []int{decision.MatchedIndex}))
	switch decision.Action {
	case retryrule.ActionReturnError:
		// Terminate: no retry, no body rewrite. Hand the configured status/message
		// back to the caller, which applies it to the response after logging.
		logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: terminating channel #%d, caller will return status=%d", channel.Id, decision.ReturnStatus))
		return false, &retryOverrideTerminate{StatusCode: decision.ReturnStatus, Message: decision.ReturnMessage}, detail
	case retryrule.ActionFallbackNextChannel:
		// Fallback is multi-hop: do NOT set the one-shot RetryFallbackDone, and do
		// NOT pin or reset the budget. The normal loop's IncreaseRetry advances the
		// priority tier so getChannel selects the next channel; it re-triggers on
		// each channel until one succeeds or channels/RetryTimes are exhausted.
		// Returning true still bypasses the "not retryable" gate (incl. 200-body).
		info.PendingRetryRewrite = decision.Rewrites
		logger.LogDebug(c, "%s", fmt.Sprintf("retry-override: handing off channel #%d to next-channel fallback", channel.Id))
		return true, nil, detail
	default:
		// Same-channel retry is one-shot: pin the channel and grant a retry that
		// does not consume budget. RetryFallbackDone prevents looping the same
		// channel.
		info.PendingRetryRewrite = decision.Rewrites
		info.RetryFallbackDone = true
		service.SetPinnedChannel(c, channel.Id)
		retryParam.ResetRetryNextTry()
		return true, nil, detail
	}
}

// recordRetryOverrideAudit writes an admin-only retry-override audit log
// (LogTypeRetryOverride) when a rule matched and drove a retry/fallback/terminate,
// and returns the detail so the error log can carry it too.
// descriptions holds the matched rule's description (aligned by position with the
// single-element rule_ids written to the log).
func recordRetryOverrideAudit(c *gin.Context, info *relaycommon.RelayInfo, channelId int, apiErr *types.NewAPIError, relayFormat types.RelayFormat, decision retryrule.Decision, descriptions []string) map[string]interface{} {
	detail := map[string]interface{}{
		"action":         decision.Action,
		"rule_ids":       []int{decision.MatchedIndex},
		"status_code":    apiErr.StatusCode,
		"relay_format":   string(relayFormat),
		"rewrites_count": len(decision.Rewrites),
	}
	if decision.Action == retryrule.ActionReturnError {
		detail["return_status"] = decision.ReturnStatus
		if decision.ReturnMessage != "" {
			detail["return_message"] = decision.ReturnMessage
		}
	}
	if hasNonEmpty(descriptions) {
		detail["rule_descriptions"] = descriptions
	}
	if msg := apiErr.Error(); msg != "" {
		detail["error_message"] = common.LocalLogPreview(msg)
	}
	if summaries := summarizeRewriteOps(decision.Rewrites); len(summaries) > 0 {
		detail["rewrites"] = summaries
	}
	group := info.UsingGroup
	if group == "" {
		group = info.UserGroup
	}
	model.RecordRetryOverrideLog(model.OverrideAuditLogParams{
		UserId:            info.UserId,
		ChannelId:         channelId,
		ModelName:         info.OriginModelName,
		TokenName:         c.GetString("token_name"),
		TokenId:           info.TokenId,
		Group:             group,
		RequestId:         info.RequestId,
		UpstreamRequestId: c.GetString(common.UpstreamRequestIdKey),
		IsStream:          info.IsStream,
	}, "retry override matched", detail)
	return detail
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
