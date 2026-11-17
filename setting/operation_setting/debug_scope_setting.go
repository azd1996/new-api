package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// DebugScopeRule scopes verbose debug logging to a single api-key (token name)
// combined with a client-requested model. Both fields are required by product
// rule: a rule with an empty token name or model never matches, so debug logging
// stays off for any traffic outside an explicitly configured scope. A field
// ending in "*" matches by prefix (for example "gpt-4o*"), which covers a whole
// model family without one rule per model name.
type DebugScopeRule struct {
	TokenName string `json:"token_name"`
	Model     string `json:"model"`
}

// DebugScopeSetting holds the api-key + model rules that selectively enable
// verbose debug logging while the global DebugEnabled switch is off. An empty
// rule list (the default) keeps scoped debug logging disabled for everyone.
type DebugScopeSetting struct {
	Rules []DebugScopeRule `json:"rules"`
}

var debugScopeSetting = DebugScopeSetting{
	Rules: []DebugScopeRule{},
}

func init() {
	config.GlobalConfig.Register("debug_scope_setting", &debugScopeSetting)
}

func GetDebugScopeSetting() *DebugScopeSetting {
	return &debugScopeSetting
}

// DebugScopeMatches reports whether the given token name and client-requested
// model fall within a configured debug scope rule. A rule matches only when both
// its token name and model match the request's values, so a request without a
// token name or model can never match. Returns false when no rules are set.
func DebugScopeMatches(tokenName, model string) bool {
	if tokenName == "" || model == "" {
		return false
	}
	for _, rule := range debugScopeSetting.Rules {
		if debugScopeFieldMatches(rule.TokenName, tokenName) && debugScopeFieldMatches(rule.Model, model) {
			return true
		}
	}
	return false
}

// debugScopeFieldMatches compares one rule field against the request value. A
// field ending in "*" matches every value with that prefix ("gpt-4o*" also
// matches "gpt-4o"), so a bare "*" matches all values. Any other field must
// equal the value exactly. An empty field never matches, keeping incomplete
// rules inert.
func debugScopeFieldMatches(field, value string) bool {
	if field == "" || value == "" {
		return false
	}
	if prefix, ok := strings.CutSuffix(field, "*"); ok {
		return strings.HasPrefix(value, prefix)
	}
	return field == value
}
