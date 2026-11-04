package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// GroupModelRule enables a behavior for requests in Group whose model matches
// one of Models. A model entry matches by exact name, by prefix when it ends
// with "*", or matches any model when it is "*".
type GroupModelRule struct {
	Group  string   `json:"group"`
	Models []string `json:"models"`
}

// MatchGroupModel reports whether any rule matches the given group+model.
func MatchGroupModel(rules []GroupModelRule, group, model string) bool {
	if group == "" || model == "" {
		return false
	}
	for _, rule := range rules {
		if rule.Group != group {
			continue
		}
		for _, pattern := range rule.Models {
			if matchModelPattern(pattern, model) {
				return true
			}
		}
	}
	return false
}

func matchModelPattern(pattern, model string) bool {
	if pattern == "*" || pattern == model {
		return true
	}
	if strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
		return true
	}
	return false
}

// GroupModelRuleSetting holds a list of (group, model) rules. Multiple option
// keys reuse this shape; whether a match means "enable" or "disable" is decided
// at the call site.
type GroupModelRuleSetting struct {
	Rules []GroupModelRule `json:"rules"`
}

var (
	extendedCacheTTLSetting          = GroupModelRuleSetting{} // opt-in: force 1h prompt-cache TTL
	metadataInjectionDisableSetting  = GroupModelRuleSetting{} // opt-out: disable metadata.user_id injection
	cacheBreakpointFixSetting        = GroupModelRuleSetting{} // opt-in: move Claude breakpoints off volatile tails
	promptCacheKeyInjectionSetting   = GroupModelRuleSetting{} // opt-in: fill a stable OpenAI prompt_cache_key
	stableBaseBreakpointSetting      = GroupModelRuleSetting{} // opt-in: protect the tools/system prefix with its own breakpoint
	fallbackCacheBreakpointSetting   = GroupModelRuleSetting{} // opt-in: add a breakpoint to an otherwise unmarked Claude prompt
	retainPreviousBreakpointSetting  = GroupModelRuleSetting{} // opt-in: restore the previous turn's cache breakpoint
	claudeVolatileTailPatternSetting = ClaudeVolatileTailPatternSetting{}
)

// ClaudeVolatileTailPatternSetting lists substrings that mark a trailing
// Claude message block as rewritten every turn (so a cache breakpoint on it
// is never read back). An empty Patterns falls back to the built-in default
// recognizing Claude Code's own per-turn token-budget reminder.
type ClaudeVolatileTailPatternSetting struct {
	Patterns []string `json:"patterns"`
}

// claudeDefaultVolatileTailPatterns is used whenever no admin override is
// configured.
var claudeDefaultVolatileTailPatterns = []string{"<total_tokens>"}

func init() {
	config.GlobalConfig.Register("extended_cache_ttl_setting", &extendedCacheTTLSetting)
	config.GlobalConfig.Register("metadata_injection_disable_setting", &metadataInjectionDisableSetting)
	config.GlobalConfig.Register("cache_breakpoint_fix_setting", &cacheBreakpointFixSetting)
	config.GlobalConfig.Register("prompt_cache_key_injection_setting", &promptCacheKeyInjectionSetting)
	config.GlobalConfig.Register("stable_base_breakpoint_setting", &stableBaseBreakpointSetting)
	config.GlobalConfig.Register("fallback_cache_breakpoint_setting", &fallbackCacheBreakpointSetting)
	config.GlobalConfig.Register("retain_previous_breakpoint_setting", &retainPreviousBreakpointSetting)
	config.GlobalConfig.Register("claude_volatile_tail_pattern_setting", &claudeVolatileTailPatternSetting)
}

func GetExtendedCacheTTLSetting() *GroupModelRuleSetting { return &extendedCacheTTLSetting }
func GetMetadataInjectionDisableSetting() *GroupModelRuleSetting {
	return &metadataInjectionDisableSetting
}
func GetCacheBreakpointFixSetting() *GroupModelRuleSetting   { return &cacheBreakpointFixSetting }
func GetStableBaseBreakpointSetting() *GroupModelRuleSetting { return &stableBaseBreakpointSetting }
func GetFallbackCacheBreakpointSetting() *GroupModelRuleSetting {
	return &fallbackCacheBreakpointSetting
}
func GetRetainPreviousBreakpointSetting() *GroupModelRuleSetting {
	return &retainPreviousBreakpointSetting
}

// GetClaudeVolatileTailPatterns returns the configured substrings, or the
// built-in default when the admin has not configured any.
func GetClaudeVolatileTailPatterns() []string {
	if len(claudeVolatileTailPatternSetting.Patterns) > 0 {
		return claudeVolatileTailPatternSetting.Patterns
	}
	return claudeDefaultVolatileTailPatterns
}

// ShouldForceExtendedCacheTTL reports whether the given group+model is configured
// to force the 1-hour prompt-cache TTL.
func ShouldForceExtendedCacheTTL(group, model string) bool {
	return MatchGroupModel(extendedCacheTTLSetting.Rules, group, model)
}

// ShouldFixCacheBreakpoints reports whether Claude requests of the given
// group+model get their misplaced cache breakpoints adjusted.
func ShouldFixCacheBreakpoints(group, model string) bool {
	return MatchGroupModel(cacheBreakpointFixSetting.Rules, group, model)
}

// ShouldUseStableBaseBreakpoint reports whether Claude requests of the given
// group+model get an independent breakpoint protecting the tools/system
// prefix. Independent of ShouldFixCacheBreakpoints: an operator may want one
// without the other.
func ShouldUseStableBaseBreakpoint(group, model string) bool {
	return MatchGroupModel(stableBaseBreakpointSetting.Rules, group, model)
}

// ShouldUseFallbackCacheBreakpoint reports whether Claude requests of the
// given group+model get a breakpoint added when the prompt carries no cache
// marker at all.
func ShouldUseFallbackCacheBreakpoint(group, model string) bool {
	return MatchGroupModel(fallbackCacheBreakpointSetting.Rules, group, model)
}

// ShouldRetainPreviousBreakpoint reports whether Claude requests of the given
// group+model restore the previous turn's cache breakpoint. Independent of
// whether cache-skeleton diagnostics (model.CacheSkeletonEnabled) are on:
// that flag only gates persisting the diagnostic log, not this behavior.
func ShouldRetainPreviousBreakpoint(group, model string) bool {
	return MatchGroupModel(retainPreviousBreakpointSetting.Rules, group, model)
}

// ShouldInjectPromptCacheKey reports whether OpenAI requests of the given
// group+model without a prompt_cache_key get a gateway-derived one.
func ShouldInjectPromptCacheKey(group, model string) bool {
	return MatchGroupModel(promptCacheKeyInjectionSetting.Rules, group, model)
}
