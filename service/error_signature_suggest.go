package service

import (
	"regexp"
	"strings"
)

// SuggestedRule is a candidate normalization rule proposed to the admin. It is
// advisory: the admin copies it into the rule editor as-is or after tweaking.
// Example/MatchCount explain why it was suggested (what residual token it would
// collapse, and how many of the sampled signatures it matches).
type SuggestedRule struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Enabled     bool   `json:"enabled"`
	Example     string `json:"example"`
	MatchCount  int    `json:"match_count"`
}

// signatureSuggestionDetector is a candidate residual-token shape. Because the
// input signatures are already normalized by the active rules, anything a
// detector still matches is leftover variance worth collapsing.
type signatureSuggestionDetector struct {
	name        string
	re          *regexp.Regexp
	replacement string
}

const suggestionExampleMaxLen = 200

// signatureSuggestionDetectors are ordered, high-precision shapes that commonly
// remain variable in upstream error text after the default rules run (counts,
// vendor ids, emails, ARNs, retry delays, long quoted ids). They are RE2 and
// template-only, so a suggestion can be pasted straight into the rule editor.
var signatureSuggestionDetectors = []signatureSuggestionDetector{
	{"long-number", regexp.MustCompile(`\b\d{5,}\b`), "*"},
	{"org-id", regexp.MustCompile(`\borg-[A-Za-z0-9]{6,}\b`), "org-*"},
	{"project-id", regexp.MustCompile(`\bproj_[A-Za-z0-9]{6,}\b`), "proj_*"},
	{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), "*"},
	{"aws-arn", regexp.MustCompile(`arn:aws[0-9a-z-]*:[A-Za-z0-9:/_.*-]+`), "*"},
	{"retry-after", regexp.MustCompile(`(?i)(try again in )[0-9.]+ ?m?s`), "${1}*"},
	{"duration", regexp.MustCompile(`\b[0-9.]+ms\b`), "*"},
	{"long-quoted-id", regexp.MustCompile(`'[A-Za-z0-9._-]{10,}'`), "'*'"},
}

// SuggestErrorSignatureRules scans the given (already-normalized) signatures and
// returns advisory rules for the residual variable tokens it finds. A detector
// is suggested only if it matches at least one signature; results are ordered by
// how many signatures they match (most impactful first). Deterministic.
func SuggestErrorSignatureRules(signatures []string) []SuggestedRule {
	out := make([]SuggestedRule, 0, len(signatureSuggestionDetectors))
	for _, d := range signatureSuggestionDetectors {
		count := 0
		example := ""
		for _, sig := range signatures {
			loc := d.re.FindStringIndex(sig)
			if loc == nil {
				continue
			}
			count++
			if example == "" {
				example = truncateSuggestionExample(sig)
			}
		}
		if count == 0 {
			continue
		}
		out = append(out, SuggestedRule{
			Name:        d.name,
			Pattern:     d.re.String(),
			Replacement: d.replacement,
			Enabled:     true,
			Example:     example,
			MatchCount:  count,
		})
	}
	// Stable order: most-matched first, ties keep detector declaration order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].MatchCount > out[j-1].MatchCount; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func truncateSuggestionExample(s string) string {
	if len(s) <= suggestionExampleMaxLen {
		return s
	}
	return strings.ToValidUTF8(s[:suggestionExampleMaxLen], "") + "…"
}
