package model

import "strings"

// Cache-minimum-token thresholds: the smallest prompt-prefix size at which each
// provider will cache at all. A request whose prompt is below its model's
// threshold can never produce a cache hit (no error is returned, the prefix is
// just processed uncached), so the log-analysis cache view treats those as
// structurally "too small" instead of counting them as ordinary cache misses.
//
// Values are taken from the official provider docs (Claude + GPT only, as
// requested). Model naming is messy across date-suffixed / mapped strings, so
// matching is done on normalized (lower-cased) substrings rather than exact
// ids; keep the rules ordered most-specific first. This table is the single
// place to edit when providers change their minimums, and it is surfaced in the
// analysis popup so the applied numbers are always visible.
//
// Sources (fetched 2026-10-29):
//   - Anthropic prompt caching docs: minimum cacheable prompt length per model.
//   - OpenAI prompt caching docs: 1,024-token minimum cacheable prefix.

// cacheMinTokensDefault is used for models outside the Claude/GPT series (e.g.
// Gemini) or otherwise unmatched. 0 means "no minimum applied", so such rows
// are never classified as too-small and instead fall through to the normal
// hit / miss classification.
const cacheMinTokensDefault = 0

type cacheMinTokenRule struct {
	// Label is the human-readable model/family shown in the UI threshold table.
	Label string
	// Series groups the rule for display ("claude" / "gpt").
	Series string
	// Match holds lower-cased substrings that must ALL be present in the
	// normalized model name for this rule to apply.
	Match []string
	// MinTokens is the minimum cacheable prompt length for the matched model.
	MinTokens int
}

// cacheMinTokenRules are evaluated in order; the first rule whose Match tokens
// are all present wins. More specific (version-bearing) rules come before the
// family fallbacks so e.g. "claude-opus-4-5" resolves to its 4.5 threshold
// before the generic Opus rule.
var cacheMinTokenRules = []cacheMinTokenRule{
	{Label: "Claude Haiku 5.x", Series: "claude", Match: []string{"haiku-5"}, MinTokens: 512},
	{Label: "Claude Haiku 4.5", Series: "claude", Match: []string{"haiku-4"}, MinTokens: 4096},
	{Label: "Claude Opus 5.x", Series: "claude", Match: []string{"opus-5"}, MinTokens: 512},
	{Label: "Claude Opus 4.8", Series: "claude", Match: []string{"opus-4-8"}, MinTokens: 1024},
	{Label: "Claude Opus 4.7", Series: "claude", Match: []string{"opus-4-7"}, MinTokens: 2048},
	{Label: "Claude Opus 4.6", Series: "claude", Match: []string{"opus-4-6"}, MinTokens: 4096},
	{Label: "Claude Opus 4.5", Series: "claude", Match: []string{"opus-4-5"}, MinTokens: 4096},
	{Label: "Claude Sonnet 5.5", Series: "claude", Match: []string{"sonnet-5-5"}, MinTokens: 512},
	{Label: "Claude Haiku 3.x", Series: "claude", Match: []string{"haiku"}, MinTokens: 2048},
	{Label: "Claude Sonnet (1024)", Series: "claude", Match: []string{"sonnet"}, MinTokens: 1024},
	{Label: "Claude Opus (1024)", Series: "claude", Match: []string{"opus"}, MinTokens: 1024},
	{Label: "GPT (all)", Series: "gpt", Match: []string{"gpt"}, MinTokens: 1024},
}

// CacheMinTokenRuleView is the API/UI projection of a threshold rule.
type CacheMinTokenRuleView struct {
	Label     string `json:"label"`
	Series    string `json:"series"`
	MinTokens int    `json:"min_tokens"`
}

// CacheMinTokenRuleViews returns the threshold table for display.
func CacheMinTokenRuleViews() []CacheMinTokenRuleView {
	views := make([]CacheMinTokenRuleView, 0, len(cacheMinTokenRules))
	for _, r := range cacheMinTokenRules {
		views = append(views, CacheMinTokenRuleView{Label: r.Label, Series: r.Series, MinTokens: r.MinTokens})
	}
	return views
}

// cacheMinTokensFor resolves the minimum cacheable prompt length for a model
// name, returning cacheMinTokensDefault when nothing matches.
func cacheMinTokensFor(modelName string) int {
	name := strings.ToLower(modelName)
	for _, r := range cacheMinTokenRules {
		matched := true
		for _, token := range r.Match {
			if !strings.Contains(name, token) {
				matched = false
				break
			}
		}
		if matched {
			return r.MinTokens
		}
	}
	return cacheMinTokensDefault
}
