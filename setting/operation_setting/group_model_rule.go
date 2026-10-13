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
	extendedCacheTTLSetting         = GroupModelRuleSetting{} // opt-in: force 1h prompt-cache TTL
	metadataInjectionDisableSetting = GroupModelRuleSetting{} // opt-out: disable metadata.user_id injection
)

func init() {
	config.GlobalConfig.Register("extended_cache_ttl_setting", &extendedCacheTTLSetting)
	config.GlobalConfig.Register("metadata_injection_disable_setting", &metadataInjectionDisableSetting)
}

func GetExtendedCacheTTLSetting() *GroupModelRuleSetting { return &extendedCacheTTLSetting }
func GetMetadataInjectionDisableSetting() *GroupModelRuleSetting {
	return &metadataInjectionDisableSetting
}

// ShouldForceExtendedCacheTTL reports whether the given group+model is configured
// to force the 1-hour prompt-cache TTL.
func ShouldForceExtendedCacheTTL(group, model string) bool {
	return MatchGroupModel(extendedCacheTTLSetting.Rules, group, model)
}
