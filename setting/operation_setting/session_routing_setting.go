package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// SessionRoutingSetting opts behind-cdnaigw groups out of automatic session
// routing. An absent entry permits routing when an enabled nonstandard channel
// serves the requested model.
type SessionRoutingSetting struct {
	GroupEnabled map[string]bool `json:"group_enabled"`
}

var sessionRoutingSetting = SessionRoutingSetting{GroupEnabled: map[string]bool{}}

func init() {
	config.GlobalConfig.Register("session_routing_setting", &sessionRoutingSetting)
}

func GetSessionRoutingSetting() *SessionRoutingSetting {
	return &sessionRoutingSetting
}
