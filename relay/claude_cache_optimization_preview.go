package relay

import (
	"github.com/QuantumNous/new-api/model"
)

// ClaudeCacheOptimizationPreview is the outcome of running the deterministic
// cache-optimization pipeline on one request body. It lets the admin console
// show exactly what the gateway would send upstream for that body.
type ClaudeCacheOptimizationPreview struct {
	Body         []byte
	EnabledRules []string
	ChangedSteps []string
	SkippedSteps []string
	Headers      map[string]string
}

// ResolveClaudeCacheOptimizationForPreview resolves the rules that apply to
// modelName using the same resolver as production traffic.
func ResolveClaudeCacheOptimizationForPreview(config *model.CustomerClaudeCacheOptimization, group, modelName string) model.CustomerClaudeCacheOptimization {
	return resolveClaudeCacheOptimization(config, group, modelName)
}

// PreviewClaudeCacheOptimization applies the production Claude cache
// optimization pipeline to body. The retain-previous-breakpoint rule is
// skipped: it depends on state persisted from a prior successful upstream
// cache write, which a dry run does not have.
func PreviewClaudeCacheOptimization(body []byte, policy model.CustomerClaudeCacheOptimization) (*ClaudeCacheOptimizationPreview, error) {
	result := &ClaudeCacheOptimizationPreview{Headers: map[string]string{}}
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
	for _, rule := range rules {
		if rule.enabled {
			result.EnabledRules = append(result.EnabledRules, rule.name)
		}
	}

	var err error
	if policy.FallbackCacheBreakpoint.Enabled {
		var added bool
		if body, added, err = addClaudeFallbackCacheBreakpoint(body); err != nil {
			return nil, err
		}
		if added {
			result.ChangedSteps = append(result.ChangedSteps, "fallback_cache_breakpoint")
		}
	}
	if policy.FixVolatileBreakpoint.Enabled {
		var fix string
		if body, fix, err = fixClaudeCacheBreakpoints(body); err != nil {
			return nil, err
		}
		if fix != "" {
			result.ChangedSteps = append(result.ChangedSteps, "fix_volatile_breakpoint")
		}
	}
	if policy.StableBaseBreakpoint.Enabled {
		var added bool
		if body, added, err = addClaudeStableBaseBreakpoint(body); err != nil {
			return nil, err
		}
		if added {
			result.ChangedSteps = append(result.ChangedSteps, "stable_base_breakpoint")
		}
	}
	if policy.RetainPreviousBreakpoint.Enabled {
		result.SkippedSteps = append(result.SkippedSteps, "retain_previous_breakpoint")
	}

	// clear_at messages need their beta header, and a turn-scoped message may
	// not carry cache_control; the API rejects the request otherwise.
	var hasClearAt bool
	if body, hasClearAt, err = prepareClaudeTurnScopedMessages(body); err != nil {
		return nil, err
	}
	if hasClearAt {
		result.Headers["anthropic-beta"] = mergeAnthropicBeta(result.Headers["anthropic-beta"], claudeClearAtBeta)
		result.ChangedSteps = append(result.ChangedSteps, "clear_at_cleanup")
	}
	if policy.ForceExtendedTTL.Enabled {
		if body, err = forceClaudeCacheControlTTL(body, claudeExtendedCacheTTL); err != nil {
			return nil, err
		}
		result.Headers["anthropic-beta"] = mergeAnthropicBeta(result.Headers["anthropic-beta"], claudeExtendedCacheBeta)
		result.ChangedSteps = append(result.ChangedSteps, "force_extended_ttl")
	}
	result.Body = body
	return result, nil
}
