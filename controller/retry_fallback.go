package controller

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// maybeApplyRetryRuleFallback inspects the just-failed upstream error against
// the failed channel's retry rules. On a match it arranges a single
// error-triggered fallback and returns true so the caller continues the retry
// loop:
//
//   - stages the rule's request-rewrite operations, which the relay handler
//     applies once to the outgoing body (see RelayInfo.PendingRetryRewrite);
//   - soft-pins the same channel so the rewritten request is retried against it
//     rather than demoted to a lower-priority channel;
//   - gives the attempt its own budget via ResetRetryNextTry, so it neither
//     consumes the normal retry count nor advances the priority tier.
//
// It fires at most once per request (RelayInfo.RetryFallbackDone) to avoid loops.
func maybeApplyRetryRuleFallback(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, apiErr *types.NewAPIError, relayFormat types.RelayFormat, retryParam *service.RetryParam) bool {
	if info == nil || channel == nil || apiErr == nil || info.RetryFallbackDone {
		return false
	}
	rules := retryrule.EffectiveRules(channel.GetSetting())
	rule, ok := retryrule.Match(rules, apiErr, string(relayFormat))
	if !ok {
		return false
	}
	info.PendingRetryRewrite = rule.Transform
	info.RetryFallbackDone = true
	service.SetPinnedChannel(c, channel.Id)
	retryParam.ResetRetryNextTry()
	return true
}
