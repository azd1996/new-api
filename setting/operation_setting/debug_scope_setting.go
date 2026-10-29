package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// DebugScopeRule scopes verbose debug logging to a single api-key (token name)
// combined with a client-requested model. Both fields are required by product
// rule: a rule with an empty token name or model never matches, so debug logging
// stays off for any traffic outside an explicitly configured scope.
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
// its token name and model equal the request's values, so a request without a
// token name or model can never match. Returns false when no rules are set.
func DebugScopeMatches(tokenName, model string) bool {
	if tokenName == "" || model == "" {
		return false
	}
	for _, rule := range debugScopeSetting.Rules {
		if rule.TokenName == "" || rule.Model == "" {
			continue
		}
		if rule.TokenName == tokenName && rule.Model == model {
			return true
		}
	}
	return false
}
