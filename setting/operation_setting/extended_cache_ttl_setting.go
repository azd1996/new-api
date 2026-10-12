package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// ExtendedCacheTTLSetting lists the groups whose Claude requests should have
// their prompt-cache breakpoints forced to the 1-hour TTL. Empty list = off.
type ExtendedCacheTTLSetting struct {
	Groups []string `json:"groups"`
}

var extendedCacheTTLSetting = ExtendedCacheTTLSetting{}

func init() {
	config.GlobalConfig.Register("extended_cache_ttl_setting", &extendedCacheTTLSetting)
}

func GetExtendedCacheTTLSetting() *ExtendedCacheTTLSetting {
	return &extendedCacheTTLSetting
}

// IsExtendedCacheTTLGroup reports whether the given group is configured to force
// the 1-hour prompt-cache TTL.
func IsExtendedCacheTTLGroup(group string) bool {
	if group == "" {
		return false
	}
	for _, g := range extendedCacheTTLSetting.Groups {
		if g == group {
			return true
		}
	}
	return false
}
