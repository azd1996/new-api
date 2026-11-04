package operation_setting

import (
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ChannelAffinityKeySource struct {
	Type string `json:"type"` // context_int, context_string, request_header, gjson
	Key  string `json:"key,omitempty"`
	Path string `json:"path,omitempty"`
}

type ChannelAffinityRule struct {
	Name             string                     `json:"name"`
	ModelRegex       []string                   `json:"model_regex"`
	PathRegex        []string                   `json:"path_regex"`
	UserAgentInclude []string                   `json:"user_agent_include,omitempty"`
	KeySources       []ChannelAffinityKeySource `json:"key_sources"`

	ValueRegex string `json:"value_regex"`
	TTLSeconds int    `json:"ttl_seconds"`

	ParamOverrideTemplate map[string]any `json:"param_override_template,omitempty"`

	SkipRetryOnFailure bool `json:"skip_retry_on_failure"`
	// "inherit" uses the global default; off/prefer/strict override it.
	// Empty preserves the legacy SkipRetryOnFailure behavior.
	SessionMode string `json:"session_mode,omitempty"`

	IncludeUsingGroup bool `json:"include_using_group"`
	IncludeModelName  bool `json:"include_model_name"`
	IncludeRuleName   bool `json:"include_rule_name"`
}

type ChannelAffinitySetting struct {
	Enabled bool `json:"enabled"`
	// Default for rules with SessionMode "inherit". Empty defaults to "prefer".
	SessionMode           string                `json:"session_mode"`
	SwitchOnSuccess       bool                  `json:"switch_on_success"`
	KeepOnChannelDisabled bool                  `json:"keep_on_channel_disabled"`
	MaxEntries            int                   `json:"max_entries"`
	DefaultTTLSeconds     int                   `json:"default_ttl_seconds"`
	Rules                 []ChannelAffinityRule `json:"rules"`
}

// Keep Codex CLI passthrough aligned with upstream. Codex uses lower-case
// header names, while HTTP matching here is case-insensitive.
// Request session/thread headers:
// https://github.com/openai/codex/commit/7c7b4861d88960f7e3bd5b7f30f8351be666dd84
// Responses metadata headers/client_metadata:
// https://github.com/openai/codex/commit/14df0e8833aad0d6d78287954b61ffac67af936c
// x-codex-turn-state response/request round trip:
// https://github.com/openai/codex/commit/ebdd8795e924a8149b616e46ca2ed7848c207a4b
var codexCliPassThroughHeaders = []string{
	"Originator",
	"Session_id",
	"Thread_id",
	"Session-Id",
	"Thread-Id",
	"X-Client-Request-Id",
	"User-Agent",
	"X-Codex-Beta-Features",
	"X-Codex-Turn-State",
	"X-Codex-Turn-Metadata",
	"X-Codex-Window-Id",
	"X-Codex-Parent-Thread-Id",
	//"X-Codex-Installation-Id",
	"X-OpenAI-Subagent",
	"X-OpenAI-Memgen-Request",
	//"X-OAI-Attestation",
	"X-ResponsesAPI-Include-Timing-Metrics",
	"X-OpenAI-Internal-Codex-Responses-Lite",
}

var claudeCliPassThroughHeaders = []string{
	"X-Stainless-Arch",
	"X-Stainless-Lang",
	"X-Stainless-Os",
	"X-Stainless-Package-Version",
	"X-Stainless-Retry-Count",
	"X-Stainless-Runtime",
	"X-Stainless-Runtime-Version",
	"X-Stainless-Timeout",
	"User-Agent",
	"X-App",
	"Anthropic-Beta",
	"Anthropic-Dangerous-Direct-Browser-Access",
	"Anthropic-Version",
}

func buildPassHeaderTemplate(headers []string) map[string]any {
	clonedHeaders := make([]string, 0, len(headers))
	clonedHeaders = append(clonedHeaders, headers...)
	return map[string]any{
		"operations": []map[string]any{
			{
				"mode":        "pass_headers",
				"value":       clonedHeaders,
				"keep_origin": true,
			},
		},
	}
}

func buildCodexPassHeaderTemplate() map[string]any {
	requestHeaders := make([]string, 0, len(codexCliPassThroughHeaders))
	requestHeaders = append(requestHeaders, codexCliPassThroughHeaders...)
	return map[string]any{
		"operations": []map[string]any{
			{
				"mode":        "pass_headers",
				"value":       requestHeaders,
				"keep_origin": true,
			},
		},
	}
}

var channelAffinitySetting = ChannelAffinitySetting{
	Enabled:               true,
	SwitchOnSuccess:       true,
	KeepOnChannelDisabled: false,
	MaxEntries:            100_000,
	DefaultTTLSeconds:     3600,
	Rules: []ChannelAffinityRule{
		{
			Name:       "codex cli trace",
			ModelRegex: []string{"^gpt-.*$"},
			PathRegex:  []string{"/v1/responses"},
			KeySources: []ChannelAffinityKeySource{
				{Type: "gjson", Path: "prompt_cache_key"},
			},
			ValueRegex:            "",
			TTLSeconds:            0,
			ParamOverrideTemplate: buildCodexPassHeaderTemplate(),
			SkipRetryOnFailure:    true,
			IncludeUsingGroup:     true,
			IncludeRuleName:       true,
			UserAgentInclude:      nil,
		},
		{
			Name:       "claude cli trace",
			ModelRegex: []string{"^claude-.*$"},
			PathRegex:  []string{"/v1/messages"},
			KeySources: []ChannelAffinityKeySource{
				{Type: "gjson", Path: "metadata.user_id"},
			},
			ValueRegex:            "",
			TTLSeconds:            0,
			ParamOverrideTemplate: buildPassHeaderTemplate(claudeCliPassThroughHeaders),
			SkipRetryOnFailure:    true,
			IncludeUsingGroup:     true,
			IncludeRuleName:       true,
			UserAgentInclude:      nil,
		},
	},
}

func init() {
	config.GlobalConfig.Register("channel_affinity_setting", &channelAffinitySetting)
}

func GetChannelAffinitySetting() *ChannelAffinitySetting {
	return &channelAffinitySetting
}

// claudeAffinityProbeModel is a representative Claude model name used only to
// test whether a configured channel-affinity rule's ModelRegex would match
// Claude traffic. It is not a real request.
const claudeAffinityProbeModel = "claude-sonnet-4-5"

// channelAffinityRegexMatchesAny reports whether any pattern matches s. Used
// here only for the startup/config-change self-check below; the hot request
// path has its own cached matcher in service/channel_affinity.go.
func channelAffinityRegexMatchesAny(patterns []string, s string) bool {
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil && re.MatchString(s) {
			return true
		}
	}
	return false
}

// channelAffinityCoversClaudeModels reports whether channel affinity is
// enabled and has at least one rule that would match Claude model traffic.
func channelAffinityCoversClaudeModels() bool {
	if !channelAffinitySetting.Enabled {
		return false
	}
	for _, rule := range channelAffinitySetting.Rules {
		if channelAffinityRegexMatchesAny(rule.ModelRegex, claudeAffinityProbeModel) {
			return true
		}
	}
	return false
}

// claudeCacheOptimizationGloballyConfigured reports whether any of the global
// Claude cache-optimization settings has at least one (group, model) rule.
// Customer-level overrides (model.CustomerClaudeCacheOptimization) are not
// visible from this package and are intentionally not checked here.
func claudeCacheOptimizationGloballyConfigured() bool {
	return len(extendedCacheTTLSetting.Rules) > 0 ||
		len(cacheBreakpointFixSetting.Rules) > 0 ||
		len(stableBaseBreakpointSetting.Rules) > 0 ||
		len(fallbackCacheBreakpointSetting.Rules) > 0 ||
		len(retainPreviousBreakpointSetting.Rules) > 0
}

// CheckClaudeCacheAffinityCoverage warns when a Claude prompt-cache
// optimization rule is configured globally but no enabled channel-affinity
// rule would route Claude traffic to a sticky upstream channel/key. Cache
// hits depend on consecutive turns of the same session landing on the same
// upstream; without affinity, the configured optimization's effective hit
// rate is likely near zero regardless of how aggressively breakpoints are
// placed. This is a best-effort heuristic against a representative model
// name, not a guarantee: a customer-level override or a model-specific
// affinity rule this probe does not match can still leave a real gap, or
// close one this check flags.
func CheckClaudeCacheAffinityCoverage() {
	if !claudeCacheOptimizationGloballyConfigured() {
		return
	}
	if channelAffinityCoversClaudeModels() {
		return
	}
	common.SysLog(common.LogText(
		"warning: a global Claude prompt-cache optimization rule (extended_cache_ttl_setting, " +
			"cache_breakpoint_fix_setting, stable_base_breakpoint_setting, fallback_cache_breakpoint_setting " +
			"or retain_previous_breakpoint_setting) is configured, but channel_affinity_setting has no enabled " +
			"rule matching Claude models; cache hits depend on repeat requests landing on the same upstream " +
			"channel/key, so the cache optimization's effective hit rate may be near zero until affinity is " +
			"configured to cover Claude traffic"))
}
