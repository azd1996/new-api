// Package retryrule matches upstream errors against per-channel retry rules and
// resolves which rules apply for a channel. It is intentionally self-contained
// (depends only on dto and types) so it can live in its own file and keep edits
// to upstream files minimal.
//
// Matching design: rules match on the primitive fields captured in
// dto.RetryRuleMatch (status codes + error message regex + optional relay
// format), evaluated against an ErrorContext built from the upstream error.
// This deliberately does not reuse relay/common's condition evaluator, because
// that evaluator (checkConditions) is unexported and reusing it would require
// editing the upstream override.go; status code + message regex fully covers
// the current requirements. The transform operations are still applied later
// via the existing override executor (ApplyParamOverride).
package retryrule

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

// ErrorContext is the evaluable view of an upstream failure that retry rules
// match against. Building it decouples matching from the concrete error type.
type ErrorContext struct {
	StatusCode  int
	Message     string
	RelayFormat string
}

// BuildErrorContext extracts the matchable fields from an upstream error.
func BuildErrorContext(apiErr *types.NewAPIError, relayFormat string) ErrorContext {
	ec := ErrorContext{RelayFormat: relayFormat}
	if apiErr != nil {
		ec.StatusCode = apiErr.StatusCode
		ec.Message = apiErr.Error()
	}
	return ec
}

// Match returns the first rule that matches the upstream error, if any.
func Match(rules []dto.RetryRule, apiErr *types.NewAPIError, relayFormat string) (dto.RetryRule, bool) {
	if apiErr == nil || len(rules) == 0 {
		return dto.RetryRule{}, false
	}
	ec := BuildErrorContext(apiErr, relayFormat)
	for _, rule := range rules {
		if matchRule(rule, ec) {
			return rule, true
		}
	}
	return dto.RetryRule{}, false
}

func matchRule(rule dto.RetryRule, ec ErrorContext) bool {
	m := rule.Match
	if m.RelayFormat != "" && !strings.EqualFold(m.RelayFormat, ec.RelayFormat) {
		return false
	}
	if len(m.StatusCodes) > 0 && !slices.Contains(m.StatusCodes, ec.StatusCode) {
		return false
	}
	if strings.TrimSpace(m.ErrorRegex) != "" {
		re := compileRegex(m.ErrorRegex)
		if re == nil || !re.MatchString(ec.Message) {
			return false
		}
	}
	return true
}

// regexCache avoids recompiling rule patterns on every upstream error. Invalid
// patterns are cached as nil so they are not recompiled either. Rules are
// validated at save time (dto.RetryRule.Validate), so nil is not expected here.
var regexCache sync.Map // pattern string -> *regexp.Regexp (nil when invalid)

func compileRegex(pattern string) *regexp.Regexp {
	if cached, ok := regexCache.Load(pattern); ok {
		re, _ := cached.(*regexp.Regexp)
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	regexCache.Store(pattern, re)
	return re
}

const ruleNameThinkingFallback = "thinking-fallback"

// DefaultThinkingFallbackRules returns the built-in rule set that handles the
// "thinking family" of upstream 400s — signature verification failure and the
// Bedrock "final block in an assistant message cannot be `thinking`" variant —
// by stripping thinking / redacted_thinking blocks and retrying on the original
// channel. Removing the thinking blocks also removes their signatures.
func DefaultThinkingFallbackRules() []dto.RetryRule {
	return []dto.RetryRule{
		{
			Name: ruleNameThinkingFallback,
			Match: dto.RetryRuleMatch{
				StatusCodes: []int{400},
				ErrorRegex:  `(?i)(signature|final block in an assistant message cannot be .?thinking.?)`,
			},
			Transform: []map[string]any{
				{"mode": "prune_objects", "path": "messages.#.content", "value": map[string]any{"where": map[string]any{"type": "thinking"}}},
				{"mode": "prune_objects", "path": "messages.#.content", "value": map[string]any{"where": map[string]any{"type": "redacted_thinking"}}},
			},
			Retry: dto.RetryRuleRetry{Target: dto.RetryRuleTargetOriginalChannel, MaxAttempts: 1},
		},
	}
}

// EffectiveRules resolves which rules apply for a channel: explicit custom
// RetryRules take precedence (advanced override); otherwise, when the built-in
// thinking fallback is enabled, the default rule set is used. Returns nil when
// the feature is off for this channel.
func EffectiveRules(settings dto.ChannelSettings) []dto.RetryRule {
	if len(settings.RetryRules) > 0 {
		return settings.RetryRules
	}
	if settings.ThinkingFallbackEnabled {
		return DefaultThinkingFallbackRules()
	}
	return nil
}
