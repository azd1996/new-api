package controller

import (
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
	rewrites, matched := retryrule.CollectRewrites(ops, ctx)
	if !matched {
		return false
	}
	info.PendingRetryRewrite = rewrites
	info.RetryFallbackDone = true
	service.SetPinnedChannel(c, channel.Id)
	retryParam.ResetRetryNextTry()
	return true
}
