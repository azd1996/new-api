package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
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
//   - retry_same_channel (default): soft-pin the same channel and grant a
//     one-shot retry via ResetRetryNextTry (does not consume RetryTimes budget);
//   - fallback_next_channel: neither pin nor reset, so the normal loop advances
//     the retry index and getChannel selects the next channel (subject to the
//     RetryTimes budget and channel availability).
//
// It fires at most once per request (RelayInfo.RetryFallbackDone) to avoid loops.
func maybeApplyRetryRuleFallback(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, apiErr *types.NewAPIError, relayFormat types.RelayFormat, retryParam *service.RetryParam) bool {
	if info == nil || channel == nil || apiErr == nil || info.RetryFallbackDone {
		logger.LogInfo(c, fmt.Sprintf("retry-override: hook skipped by guard (info=%t channel=%t apiErr=%t fallbackDone=%t)",
			info != nil, channel != nil, apiErr != nil, info != nil && info.RetryFallbackDone))
		return false
	}
	ops := channel.GetSetting().RetryOverride
	logger.LogInfo(c, fmt.Sprintf("retry-override: hook reached on channel #%d, configured operations=%d", channel.Id, len(ops)))
	if len(ops) == 0 {
		return false
	}
	ctx := retryrule.ResponseContext(apiErr, string(relayFormat))
	logger.LogInfo(c, fmt.Sprintf("retry-override: evaluating %d operation(s) on channel #%d, status_code=%d, error_message=%q",
		len(ops), channel.Id, apiErr.StatusCode, apiErr.Error()))
	rewrites, matched, action := retryrule.CollectRewrites(ops, ctx)
	if !matched {
		logger.LogInfo(c, fmt.Sprintf("retry-override: no operation matched on channel #%d, not retrying", channel.Id))
		return false
	}
	if data, err := common.Marshal(rewrites); err == nil {
		logger.LogInfo(c, fmt.Sprintf("retry-override: matched (action=%s), staging %d rewrite(s) on channel #%d: %s",
			action, len(rewrites), channel.Id, string(data)))
	} else {
		logger.LogInfo(c, fmt.Sprintf("retry-override: matched (action=%s), staging %d rewrite(s) on channel #%d",
			action, len(rewrites), channel.Id))
	}
	info.PendingRetryRewrite = rewrites
	info.RetryFallbackDone = true
	if action == retryrule.ActionFallbackNextChannel {
		// Do not pin and do not reset the retry budget: let the normal loop's
		// IncreaseRetry advance the priority tier so getChannel picks the next
		// channel. Returning true still bypasses the "400 not retryable" gate.
		logger.LogInfo(c, fmt.Sprintf("retry-override: handing off channel #%d to next-channel fallback", channel.Id))
		return true
	}
	// Same-channel retry: pin the channel and grant a one-shot retry that does
	// not consume retry budget.
	service.SetPinnedChannel(c, channel.Id)
	retryParam.ResetRetryNextTry()
	return true
}
