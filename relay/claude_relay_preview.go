package relay

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

// ClaudeRelayPreview is the result of running a full Claude relay attempt in
// dry-run mode. The request has gone through parsing, model mapping, channel
// selection, protocol conversion, parameter/header overrides and the cache
// optimizations, but was never sent upstream.
type ClaudeRelayPreview struct {
	ChannelID     int
	ChannelName   string
	ChannelType   int
	Group         string
	OriginModel   string
	UpstreamModel string
	ModelMapped   bool
	PassThrough   bool
	URL           string
	Method        string
	Headers       map[string]string
	Body          string
	EnabledRules  []string
	SkippedRules  []string
}

// PreviewClaudeRelay runs the production Claude relay pipeline against the
// request body in c and captures the outbound HTTP request instead of sending
// it. The caller must prepare the context with the using group and original
// model (the same keys the relay middleware sets for real requests).
func PreviewClaudeRelay(c *gin.Context) (*ClaudeRelayPreview, *types.NewAPIError) {
	request, err := helper.GetAndValidateClaudeRequest(c)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	info := relaycommon.GenRelayInfoClaude(c, request)
	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  info.UsingGroup,
		ModelName:   request.Model,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(errors.New("no available channel"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	return previewClaudeRelayWithChannel(c, info, channel, selectGroup)
}

// previewClaudeRelayWithChannel runs the relay pipeline for an already
// selected channel. It is split from PreviewClaudeRelay so tests can exercise
// the conversion/capture path without a channel database.
func previewClaudeRelayWithChannel(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, selectGroup string) (*ClaudeRelayPreview, *types.NewAPIError) {
	service.RequestPolicy(c).BeginAttempt(channel, selectGroup)
	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
	if setupErr := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName); setupErr != nil {
		return nil, setupErr
	}

	info.DryRunCapture = &relaycommon.UpstreamRequestCapture{}
	if apiErr := ClaudeHelper(c, info); apiErr != nil {
		return nil, apiErr
	}
	if info.DryRunCapture == nil || info.DryRunCapture.URL == "" {
		return nil, types.NewError(errors.New("upstream request was not captured"), types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}

	policy := resolveClaudeCacheOptimization(
		claudeCacheOptimizationForRequest(c, ""), claudeRequestGroup(c), info.UpstreamModelName)
	result := &ClaudeRelayPreview{
		ChannelID:     channel.Id,
		ChannelName:   channel.Name,
		ChannelType:   channel.Type,
		Group:         selectGroup,
		OriginModel:   info.OriginModelName,
		UpstreamModel: info.UpstreamModelName,
		ModelMapped:   info.IsModelMapped,
		PassThrough: model_setting.GetGlobalSettings().PassThroughRequestEnabled ||
			info.ChannelSetting.PassThroughBodyEnabled,
		URL:          relaycommon.SanitizeURLForLog(info.DryRunCapture.URL),
		Method:       info.DryRunCapture.Method,
		Headers:      info.DryRunCapture.Headers,
		Body:         info.DryRunCapture.Body,
		EnabledRules: claudeEnabledRuleNames(policy),
	}
	if policy.RetainPreviousBreakpoint.Enabled {
		// Retaining the previous breakpoint requires state persisted by an
		// earlier successful upstream call, which a dry run does not have.
		result.SkippedRules = []string{"retain_previous_breakpoint"}
	}
	return result, nil
}

func claudeEnabledRuleNames(policy model.CustomerClaudeCacheOptimization) []string {
	rules := []struct {
		name    string
		enabled bool
	}{
		{"force_extended_ttl", policy.ForceExtendedTTL.Enabled},
		{"fix_volatile_breakpoint", policy.FixVolatileBreakpoint.Enabled},
		{"stable_base_breakpoint", policy.StableBaseBreakpoint.Enabled},
		{"retain_previous_breakpoint", policy.RetainPreviousBreakpoint.Enabled},
		{"fallback_cache_breakpoint", policy.FallbackCacheBreakpoint.Enabled},
	}
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.enabled {
			names = append(names, rule.name)
		}
	}
	return names
}
