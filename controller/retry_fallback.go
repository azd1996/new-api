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
//   - stages the matching operations (with conditions stripped) as the one-shot
//     request rewrite applied by the relay handler on the next attempt;
//   - soft-pins the same channel so the rewritten request is retried against it;
//   - gives the attempt its own budget via ResetRetryNextTry.
//
// It fires at most once per request (RelayInfo.RetryFallbackDone) to avoid loops.
func maybeApplyRetryRuleFallback(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, apiErr *types.NewAPIError, relayFormat types.RelayFormat, retryParam *service.RetryParam) bool {
	if info == nil || channel == nil || apiErr == nil || info.RetryFallbackDone {
		return false
	}
	ops := channel.GetSetting().RetryOverride
	if len(ops) == 0 {
		return false
	}
	ctx := retryrule.ResponseContext(apiErr, string(relayFormat))
	logger.LogInfo(c, fmt.Sprintf("retry-override: evaluating %d operation(s) on channel #%d, status_code=%d, error_message=%q",
		len(ops), channel.Id, apiErr.StatusCode, apiErr.Error()))
	rewrites, matched := retryrule.CollectRewrites(ops, ctx)
	if !matched {
		logger.LogInfo(c, fmt.Sprintf("retry-override: no operation matched on channel #%d, not retrying", channel.Id))
		return false
	}
	if data, err := common.Marshal(rewrites); err == nil {
		logger.LogInfo(c, fmt.Sprintf("retry-override: matched, staging %d rewrite(s) on channel #%d and retrying the same channel: %s",
			len(rewrites), channel.Id, string(data)))
	} else {
		logger.LogInfo(c, fmt.Sprintf("retry-override: matched, staging %d rewrite(s) on channel #%d and retrying the same channel",
			len(rewrites), channel.Id))
	}
	info.PendingRetryRewrite = rewrites
	info.RetryFallbackDone = true
	service.SetPinnedChannel(c, channel.Id)
	retryParam.ResetRetryNextTry()
	return true
}
