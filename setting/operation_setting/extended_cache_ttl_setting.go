package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// ExtendedCacheTTLRule forces the 1-hour prompt-cache TTL for requests in Group
// whose model matches one of Models. A model entry matches by exact name, by
// prefix when it ends with "*", or matches any model when it is "*".
type ExtendedCacheTTLRule struct {
	Group  string   `json:"group"`
	Models []string `json:"models"`
}

// ExtendedCacheTTLSetting lists per-(group, model) rules that force the 1-hour
// prompt-cache TTL. Empty rules = off. Restricting by model avoids sending the
// ttl field to models that only support 5m (which Bedrock rejects with a
// ValidationException).
type ExtendedCacheTTLSetting struct {
	Rules []ExtendedCacheTTLRule `json:"rules"`
}

var extendedCacheTTLSetting = ExtendedCacheTTLSetting{}

func init() {
	config.GlobalConfig.Register("extended_cache_ttl_setting", &extendedCacheTTLSetting)
}

func GetExtendedCacheTTLSetting() *ExtendedCacheTTLSetting {
	return &extendedCacheTTLSetting
}

// ShouldForceExtendedCacheTTL reports whether the given group+model is
// configured to force the 1-hour prompt-cache TTL.
func ShouldForceExtendedCacheTTL(group, model string) bool {
	if group == "" || model == "" {
		return false
	}
	for _, rule := range extendedCacheTTLSetting.Rules {
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
